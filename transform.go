package main

import (
	"bytes"
	"log"
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
var widthAttrRegex = regexp.MustCompile(`(?i)(?:^|\s)width\s*=`)
var heightAttrRegex = regexp.MustCompile(`(?i)(?:^|\s)height\s*=`)

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

func rebuildImage(conf *transformConfig, relPath string, captures [][]byte) []byte {
	keyPath := findImagePath(conf.config, relPath, string(captures[2]))
	slug, ok := conf.pathToSlug[keyPath]
	if !ok {
		return captures[0]
	}
	built := conf.manifest[slug]
	sizesAttr := effectiveSizesAttr(conf, keyPath, captures)

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
		buf.WriteString(`>`)
	}

	buf.WriteString("<img ")
	buf.Write(captures[1])
	buf.WriteString(`src="`)
	buf.WriteString(nameToImagePath(conf.config, built.OriginalName))
	buf.WriteString(`"`)

	// if there's only one image no point in it being responsive
	if len(built.Files) > 1 {
		buf.WriteString(` srcset="`)
		writeSrcset(&buf, conf, built.Files)
		buf.WriteString(`"`)
		hasSourceSizes := sizesAttrRegex.Match(captures[1]) || sizesAttrRegex.Match(captures[3])
		if sizesAttr != "" && !hasSourceSizes {
			buf.WriteString(` sizes="`)
			buf.WriteString(sizesAttr)
			buf.WriteString(`"`)
		}
	}
	hasSourceWidth := widthAttrRegex.Match(captures[1]) || widthAttrRegex.Match(captures[3])
	hasSourceHeight := heightAttrRegex.Match(captures[1]) || heightAttrRegex.Match(captures[3])
	if len(built.WebPFiles) > 0 && !hasSourceWidth && !hasSourceHeight && built.Width > 0 && built.Height > 0 {
		buf.WriteString(` width="`)
		buf.WriteString(strconv.Itoa(built.Width))
		buf.WriteString(`" height="`)
		buf.WriteString(strconv.Itoa(built.Height))
		buf.WriteString(`"`)
	}

	buf.Write(captures[3])
	buf.WriteString(`>`)
	if len(built.WebPFiles) > 0 {
		buf.WriteString(`</picture>`)
	}

	return buf.Bytes()
}

func translateHtml(conf *transformConfig, inPath string, outPath string) (err error) {
	// log.Printf("Translating %s", inPath)
	bytes, err := readFileBytes(inPath)

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

	df, err := os.Create(outPath)
	if err != nil {
		return err
	}
	defer df.Close()
	df.Write(newBytes)

	return
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
