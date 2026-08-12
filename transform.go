package main

import (
	"bytes"
	"fmt"
	"log"
	"math"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/bmatcuk/doublestar"
)

// Yes I'm using a regex to parse HTML, yes everyone tells you not to do that.
// This is for personal sites so if this doesn't match your HTML, fix your HTML.
// This saves me having to do a bunch of tree traversal and serializing.
var imgRegex = regexp.MustCompile(`<img ([^>]*)src="([^"]+)"([^>]*)>`)
var sizesAttrRegex = regexp.MustCompile(`(?i)(?:^|\s)sizes\s*=`)
var sizesAttrValueRegex = regexp.MustCompile(`(?i)(?:^|\s)sizes\s*=\s*(?:"([^"]*)"|'([^']*)')`)
var maxDPRAttrRegex = regexp.MustCompile(`(?i)(?:^|\s)data-enfasten-max-dpr\s*=\s*(?:"([^"]*)"|'([^']*)')`)
var widthAttrRegex = regexp.MustCompile(`(?i)(?:^|\s)width\s*=`)
var heightAttrRegex = regexp.MustCompile(`(?i)(?:^|\s)height\s*=`)
var widthAttrValueRegex = regexp.MustCompile(`(?i)(?:^|\s)width\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s]+))`)
var heightAttrValueRegex = regexp.MustCompile(`(?i)(?:^|\s)height\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s]+))`)

const highestAdjustedDPR = 4.0
const adjustedDPRStep = 0.5

type transformConfig struct {
	*config
	manifest   map[string]builtImage
	pathToSlug map[string]string
}

func translatePath(conf *config, file string) string {
	inputPath := path.Join(conf.basePath, conf.InputFolder)
	relPath, err := filepath.Rel(inputPath, file)
	if err != nil {
		log.Fatalf("Can't make relative path %v", err)
	}
	return path.Join(conf.basePath, conf.OutputFolder, relPath)
}

func findImagePath(conf *config, fileDir string, relRef string) string {
	if relRef[0] == '/' {
		return relRef[1:len(relRef)]
	} else {
		return path.Join(fileDir, relRef)
	}
}

func nameToImagePath(conf *config, name string) string {
	return path.Join("/", conf.ImageFolder, name)
}

func (conf *config) responsiveRuleForImage(relPath string) *sizesRule {
	for _, rule := range conf.SizesRules {
		matched, err := doublestar.Match(rule.Pattern, relPath)
		if err != nil {
			log.Printf("Invalid responsive-image sizes pattern %q: %v", rule.Pattern, err)
			continue
		}
		if matched {
			return &rule
		}
	}
	return nil
}

func (conf *config) sizesAttrForImage(relPath string) string {
	if rule := conf.responsiveRuleForImage(relPath); rule != nil && rule.Sizes != "" {
		return rule.Sizes
	}
	return conf.SizesAttr
}

func effectiveSizesAttr(conf *transformConfig, keyPath string, captures [][]byte) string {
	attrs := append(append([]byte{}, captures[1]...), captures[3]...)
	if match := sizesAttrValueRegex.FindSubmatch(attrs); len(match) > 1 {
		if len(match[1]) > 0 {
			return string(match[1])
		}
		return string(match[2])
	}
	return conf.sizesAttrForImage(keyPath)
}

func sourceAttributes(captures [][]byte) []byte {
	return append(append([]byte{}, captures[1]...), captures[3]...)
}

func validateMaxDPR(maxDPR float64) error {
	if maxDPR == 0 || (maxDPR >= 1 && maxDPR <= highestAdjustedDPR) {
		return nil
	}
	return fmt.Errorf("maxdpr must be 0 (disabled) or between 1 and %.0f, got %g", highestAdjustedDPR, maxDPR)
}

func effectiveMaxDPR(conf *transformConfig, captures [][]byte) (float64, error) {
	matches := maxDPRAttrRegex.FindAllSubmatch(sourceAttributes(captures), -1)
	if len(matches) == 0 {
		return conf.MaxDPR, nil
	}
	if len(matches) > 1 {
		return 0, fmt.Errorf("an img may only specify data-enfasten-max-dpr once")
	}

	value := string(matches[0][1])
	if value == "" {
		value = string(matches[0][2])
	}
	value = strings.TrimSpace(value)
	if strings.EqualFold(value, "none") {
		return 0, nil
	}
	maxDPR, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid data-enfasten-max-dpr value %q", value)
	}
	if err = validateMaxDPR(maxDPR); err != nil {
		return 0, fmt.Errorf("invalid data-enfasten-max-dpr value %q: %w", value, err)
	}
	return maxDPR, nil
}

type sourceSize struct {
	media string
	size  string
}

func splitSizesEntries(sizes string) ([]string, error) {
	var entries []string
	start := 0
	depth := 0
	for index, char := range sizes {
		switch char {
		case '(':
			depth++
		case ')':
			depth--
			if depth < 0 {
				return nil, fmt.Errorf("invalid sizes value %q: unmatched closing parenthesis", sizes)
			}
		case ',':
			if depth == 0 {
				entries = append(entries, strings.TrimSpace(sizes[start:index]))
				start = index + 1
			}
		}
	}
	if depth != 0 {
		return nil, fmt.Errorf("invalid sizes value %q: unmatched opening parenthesis", sizes)
	}
	entries = append(entries, strings.TrimSpace(sizes[start:]))
	return entries, nil
}

func parseSourceSize(entry string) (sourceSize, error) {
	entry = strings.TrimSpace(entry)
	if entry == "" {
		return sourceSize{}, fmt.Errorf("sizes contains an empty entry")
	}

	depth := 0
	tokenStart := 0
	inToken := false
	for index, char := range entry {
		if depth == 0 && (char == ' ' || char == '\t' || char == '\n' || char == '\r') {
			inToken = false
			continue
		}
		if depth == 0 && !inToken {
			tokenStart = index
			inToken = true
		}
		switch char {
		case '(':
			depth++
		case ')':
			depth--
		}
	}

	size := strings.TrimSpace(entry[tokenStart:])
	if size == "" || strings.EqualFold(size, "auto") {
		return sourceSize{}, fmt.Errorf("maxdpr requires an explicit source size, got %q", size)
	}
	return sourceSize{
		media: strings.TrimSpace(entry[:tokenStart]),
		size:  size,
	}, nil
}

func formatDPR(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}

func formatDPRFactor(value float64) string {
	return strings.TrimRight(strings.TrimRight(strconv.FormatFloat(value, 'f', 4, 64), "0"), ".")
}

// maxDPRSizes adds higher-resolution branches ahead of the source sizes. Each
// branch reduces the declared slot by maxDPR/deviceDPR, causing width-based
// srcsets to target the configured density without degrading 1x/2x clients.
// Half-step buckets keep common fractional Android densities close to the cap.
func maxDPRSizes(sizes string, maxDPR float64) (string, error) {
	if sizes == "" || maxDPR == 0 || maxDPR >= highestAdjustedDPR {
		return sizes, nil
	}
	if err := validateMaxDPR(maxDPR); err != nil {
		return "", err
	}

	rawEntries, err := splitSizesEntries(sizes)
	if err != nil {
		return "", err
	}
	entries := make([]sourceSize, 0, len(rawEntries))
	for _, rawEntry := range rawEntries {
		entry, parseErr := parseSourceSize(rawEntry)
		if parseErr != nil {
			return "", parseErr
		}
		entries = append(entries, entry)
	}

	var adjusted []string
	firstDensity := math.Ceil(maxDPR/adjustedDPRStep) * adjustedDPRStep
	if firstDensity <= maxDPR {
		firstDensity += adjustedDPRStep
	}
	for deviceDPR := highestAdjustedDPR; deviceDPR >= firstDensity; deviceDPR -= adjustedDPRStep {
		threshold := deviceDPR - adjustedDPRStep/2
		factor := maxDPR / deviceDPR
		resolution := `(min-resolution: ` + formatDPR(threshold) + `dppx)`
		for _, entry := range entries {
			media := resolution
			if entry.media != "" {
				media = entry.media + " and " + resolution
			}
			adjusted = append(adjusted, media+` calc((`+entry.size+`) * `+formatDPRFactor(factor)+`)`)
		}
	}
	adjusted = append(adjusted, rawEntries...)
	return strings.Join(adjusted, ", "), nil
}

func rewriteBuildAttributes(attrs []byte, sourceHadSizes bool, sizes string) []byte {
	rewritten := maxDPRAttrRegex.ReplaceAll(attrs, nil)
	if sourceHadSizes {
		rewritten = sizesAttrValueRegex.ReplaceAll(rewritten, []byte(` sizes="`+sizes+`"`))
	}
	return rewritten
}

func writeSrcset(buf *bytes.Buffer, conf *transformConfig, files []builtImageFile) {
	for i, builtFile := range files {
		if i != 0 {
			buf.WriteString(`, `)
		}
		buf.WriteString(nameToImagePath(conf.config, builtFile.FileName))
		buf.WriteString(` `)
		buf.WriteString(strconv.Itoa(builtFile.Width))
		buf.WriteString(`w`)
	}
}

func attributeValue(attrs []byte, valueRegex *regexp.Regexp) (string, bool) {
	match := valueRegex.FindSubmatch(attrs)
	if len(match) == 0 {
		return "", false
	}
	for _, value := range match[1:] {
		if value != nil {
			return string(value), true
		}
	}
	return "", false
}

func writeDimensionAttribute(buf *bytes.Buffer, name string, value string) {
	buf.WriteString(` `)
	buf.WriteString(name)
	buf.WriteString(`="`)
	buf.WriteString(value)
	buf.WriteString(`"`)
}

func rebuildImage(conf *transformConfig, relPath string, captures [][]byte) []byte {
	keyPath := findImagePath(conf.config, relPath, string(captures[2]))
	slug, ok := conf.pathToSlug[keyPath]
	if !ok {
		return captures[0]
	}
	built := conf.manifest[slug]
	sizesAttr := effectiveSizesAttr(conf, keyPath, captures)
	maxDPR, err := effectiveMaxDPR(conf, captures)
	if err != nil {
		log.Printf("Invalid max DPR for %s: %v", keyPath, err)
		return captures[0]
	}
	sizesAttr, err = maxDPRSizes(sizesAttr, maxDPR)
	if err != nil {
		log.Printf("Cannot apply max DPR to %s: %v", keyPath, err)
		return captures[0]
	}
	sourceHadSizes := sizesAttrRegex.Match(captures[1]) || sizesAttrRegex.Match(captures[3])
	beforeSrc := rewriteBuildAttributes(captures[1], sourceHadSizes, sizesAttr)
	afterSrc := rewriteBuildAttributes(captures[3], sourceHadSizes, sizesAttr)
	attrs := append(append([]byte{}, beforeSrc...), afterSrc...)
	hasSourceWidth := widthAttrRegex.Match(attrs)
	hasSourceHeight := heightAttrRegex.Match(attrs)
	sourceWidth, hasSourceWidthValue := attributeValue(attrs, widthAttrValueRegex)
	sourceHeight, hasSourceHeightValue := attributeValue(attrs, heightAttrValueRegex)
	if !hasSourceWidth && !hasSourceHeight && built.Width > 0 && built.Height > 0 {
		sourceWidth = strconv.Itoa(built.Width)
		sourceHeight = strconv.Itoa(built.Height)
		hasSourceWidthValue = true
		hasSourceHeightValue = true
	}

	var buf bytes.Buffer
	if len(built.WebPFiles) > 0 {
		buf.WriteString(`<picture><source type="image/webp" srcset="`)
		writeSrcset(&buf, conf, built.WebPFiles)
		buf.WriteString(`"`)
		if sizesAttr != "" {
			buf.WriteString(` sizes="`)
			buf.WriteString(sizesAttr)
			buf.WriteString(`"`)
		}
		if hasSourceWidthValue {
			writeDimensionAttribute(&buf, "width", sourceWidth)
		}
		if hasSourceHeightValue {
			writeDimensionAttribute(&buf, "height", sourceHeight)
		}
		buf.WriteString(`>`)
	}

	buf.WriteString("<img ")
	buf.Write(beforeSrc)
	buf.WriteString(`src="`)
	buf.WriteString(nameToImagePath(conf.config, built.OriginalName))
	buf.WriteString(`"`)

	// if there's only one image no point in it being responsive
	if len(built.Files) > 1 {
		buf.WriteString(` srcset="`)
		writeSrcset(&buf, conf, built.Files)
		buf.WriteString(`"`)
		if sizesAttr != "" && !sourceHadSizes {
			buf.WriteString(` sizes="`)
			buf.WriteString(sizesAttr)
			buf.WriteString(`"`)
		}
	}
	if len(built.WebPFiles) > 0 && !hasSourceWidth && !hasSourceHeight && built.Width > 0 && built.Height > 0 {
		buf.WriteString(` width="`)
		buf.WriteString(strconv.Itoa(built.Width))
		buf.WriteString(`" height="`)
		buf.WriteString(strconv.Itoa(built.Height))
		buf.WriteString(`"`)
	}

	buf.Write(afterSrc)
	buf.WriteString(`>`)
	if len(built.WebPFiles) > 0 {
		buf.WriteString(`</picture>`)
	}

	return buf.Bytes()
}

func translateHtml(conf *transformConfig, inPath string, outPath string) (err error) {
	// log.Printf("Translating %s", inPath)
	bytes, err := readFileBytes(inPath)
	if err != nil {
		return err
	}

	// set up for HTML relative paths
	inputPath := path.Join(conf.basePath, conf.InputFolder)
	dirPath := path.Dir(inPath)
	relPath, err := filepath.Rel(inputPath, dirPath)
	if err != nil {
		log.Fatalf("Can't make relative path %v", err)
	}
	// log.Printf("Relative path: %s", relPath)

	newBytes := imgRegex.ReplaceAllFunc(bytes, func(match []byte) []byte {
		captures := imgRegex.FindSubmatch(match)
		// log.Printf("Old Image: %s", match)
		rebuilt := rebuildImage(conf, relPath, captures)
		// log.Printf("New Image: %s", rebuilt)
		return rebuilt
	})

	return writeFileIfChanged(outPath, newBytes)
}

// isBlacklisted reports whether file (an absolute-or-relative path under the
// input folder) matches any of the config's Blacklist glob patterns. Patterns
// are doublestar globs evaluated against the path relative to the input folder,
// e.g. "**/*.mp4" matches videos at any depth.
func (conf *config) isBlacklisted(file string) bool {
	relPath, err := filepath.Rel(conf.InputFolderPath(), file)
	if err != nil {
		return false
	}
	for _, pattern := range conf.Blacklist {
		if matched, err := doublestar.Match(pattern, relPath); err == nil && matched {
			return true
		}
	}
	return false
}

func transferAndTransform(conf *transformConfig, whitelist *[]string, file string) (err error) {
	outPath := translatePath(conf.config, file)
	// log.Printf("Walked %s,      translate to %s", file, outPath)
	err = os.MkdirAll(path.Dir(outPath), os.ModePerm)
	extension := path.Ext(file)

	// Always whitelist the output path so deleteNonWhitelist keeps it, then bail
	// out for blacklisted files. This leaves whatever is already at outPath (for
	// example a symlink placed there by the build) untouched: enfasten neither
	// overwrites nor deletes it. Used to keep large assets like videos as
	// symlinks in the output instead of full copies.
	*whitelist = append(*whitelist, outPath)
	if conf.isBlacklisted(file) {
		return
	}

	switch extension {
	case ".html":
		err = translateHtml(conf, file, outPath)
	default:
		err = copyFile(file, outPath)
	}
	return
}

func transferAndTransformAll(conf *transformConfig) (whitelist []string, err error) {
	whitelist = []string{}
	walkFunk := func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		return transferAndTransform(conf, &whitelist, path)
	}
	inputPath := path.Join(conf.basePath, conf.InputFolder)
	if conf.languageFilter != "" {
		inputPath = path.Join(inputPath, conf.languageFilter)
	}
	err = filepath.Walk(inputPath, filepath.WalkFunc(walkFunk))
	return
}

func deleteNonWhitelist(conf *config, whitelist []string) (err error) {
	outputPath := path.Join(conf.basePath, conf.OutputFolder)
	return deleteNonWhitelistUnder(conf, whitelist, outputPath)
}

func deleteNonWhitelistUnder(conf *config, whitelist []string, outputPath string) (err error) {

	whiteMap := map[string]bool{}
	for _, item := range whitelist {
		relPath, relErr := filepath.Rel(outputPath, item)
		if relErr != nil || relPath == ".." || strings.HasPrefix(relPath, ".."+string(filepath.Separator)) {
			continue
		}
		whiteMap[item] = true

		// TODO: this is wasteful as heck
		trimmedPath := item
		for {
			trimmedPath = path.Dir(trimmedPath)
			whiteMap[trimmedPath] = true
			if trimmedPath == outputPath {
				break
			}
		}
	}

	toRemove := []string{}

	walkFunk := func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if _, ok := whiteMap[path]; !ok {
			toRemove = append(toRemove, path)
			if info.IsDir() {
				return filepath.SkipDir
			}
		}
		return nil
	}
	err = filepath.Walk(outputPath, filepath.WalkFunc(walkFunk))

	if len(toRemove) > 0 {
		log.Printf("Delete: %v\n", toRemove)
	}

	for _, item := range toRemove {
		err = os.RemoveAll(item)
		if err != nil {
			return
		}
	}

	return
}
