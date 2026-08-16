package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"path"
	"strings"

	"gopkg.in/yaml.v2"
)

type config struct {
	InputFolder  string
	OutputFolder string
	// Source fragments referenced by HTML include directives. The folder is
	// relative to InputFolder and is never copied into OutputFolder.
	IncludeFolder string
	ImageFolder   string
	ManifestFile  string
	SizesAttr     string
	MaxDPR        float64
	SizesRules    []sizesRule
	OptimCommand  []string
	// Maximum number of image paths appended to each OptimCommand invocation.
	// Zero keeps the historical single-command behavior.
	OptimBatchSize int
	WebPCommand    []string
	// A number between 0-1 where if the downscaling is greater
	// than this fraction of the width it doesn't bother.
	ScaleThreshold    float64
	JpgScaleThreshold float64
	JpgQuality        int
	DoCopy            bool
	Widths            []int
	Blacklist         []string
	// PNG/JPEG paths copied normally but excluded from responsive processing.
	PassthroughImages []string
	// PNG/JPEG globs that remain eligible for WebP during a language-filtered
	// build because they are shared by every locale.
	SharedImages   []string
	basePath       string
	doCulling      bool
	languageFilter string
}

type sizesRule struct {
	Pattern string
	Sizes   string
	// Optional media query that gates whether the real image candidates are
	// eligible at all. This is distinct from Sizes, which only influences which
	// candidate the browser chooses after it has decided to load an image.
	Media  string
	Widths []int
	WebP   bool
}

func (conf *config) ImageFolderPath() string {
	return path.Join(conf.basePath, conf.OutputFolder, conf.ImageFolder)
}

func (conf *config) InputFolderPath() string {
	return path.Join(conf.basePath, conf.InputFolder)
}

func (conf *config) IncludeFolderPath() string {
	if conf.IncludeFolder == "" {
		return ""
	}
	return path.Join(conf.InputFolderPath(), conf.IncludeFolder)
}

func copyFile(source string, dest string) error {
	sourceInfo, err := os.Stat(source)
	if err != nil {
		return err
	}
	destInfo, err := os.Stat(dest)
	if err == nil && destInfo.Size() == sourceInfo.Size() && destInfo.ModTime().Equal(sourceInfo.ModTime()) {
		return nil
	}
	if err != nil && !os.IsNotExist(err) {
		return err
	}

	sf, err := os.Open(source)
	if err != nil {
		return err
	}
	defer sf.Close()
	df, err := os.Create(dest)
	if err != nil {
		return err
	}
	if _, err = io.Copy(df, sf); err != nil {
		df.Close()
		return err
	}
	if err = df.Close(); err != nil {
		return err
	}
	return os.Chtimes(dest, sourceInfo.ModTime(), sourceInfo.ModTime())
}

func writeFileIfChanged(filePath string, contents []byte) error {
	existing, err := os.ReadFile(filePath)
	if err == nil && bytes.Equal(existing, contents) {
		return nil
	}
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return os.WriteFile(filePath, contents, 0o666)
}

func readFileBytes(path string) (bytes []byte, err error) {
	f, err := os.OpenFile(path, os.O_RDONLY, 0)
	if err != nil {
		return
	}
	defer f.Close()

	bytes, err = ioutil.ReadAll(f)
	return
}

func readConfig(basePath string) (conf config, err error) {
	conf = config{
		InputFolder:    "_site",
		OutputFolder:   "_fastsite",
		ImageFolder:    "assets/images",
		ManifestFile:   "enfasten_manifest.yml",
		SizesAttr:      "",
		MaxDPR:         0,
		OptimCommand:   nil,
		OptimBatchSize: 32,
		DoCopy:         true,
		// ManifestFile:   "",
		ScaleThreshold:    0.9,
		JpgScaleThreshold: 0.7,
		JpgQuality:        90,
		Widths:            []int{},
	}

	bytes, err := readFileBytes(path.Join(basePath, "enfasten.yml"))
	if err != nil {
		return
	}
	err = yaml.Unmarshal(bytes, &conf)
	return
}

func normalizeLanguageFilter(languageFilter string) (string, error) {
	languageFilter = strings.TrimSpace(languageFilter)
	if languageFilter == "" {
		return "", nil
	}
	if path.Clean(languageFilter) != languageFilter || path.Base(languageFilter) != languageFilter || languageFilter == "." || languageFilter == ".." {
		return "", fmt.Errorf("invalid language filter %q", languageFilter)
	}
	return languageFilter, nil
}

func normalizeIncludeFolder(includeFolder string) (string, error) {
	includeFolder = strings.TrimSpace(includeFolder)
	if includeFolder == "" {
		return "", nil
	}
	if path.Clean(includeFolder) != includeFolder || path.IsAbs(includeFolder) || includeFolder == "." || includeFolder == ".." || strings.HasPrefix(includeFolder, "../") {
		return "", fmt.Errorf("invalid include folder %q", includeFolder)
	}
	return includeFolder, nil
}

func buildFastSite(basePath string, doCulling bool, languageFilter string) (err error) {
	conf, err := readConfig(basePath)
	if err != nil {
		return
	}

	languageFilter, err = normalizeLanguageFilter(languageFilter)
	if err != nil {
		return
	}

	conf.basePath = basePath
	conf.doCulling = doCulling
	conf.languageFilter = languageFilter
	conf.IncludeFolder, err = normalizeIncludeFolder(conf.IncludeFolder)
	if err != nil {
		return
	}
	if conf.IncludeFolder != "" {
		includeInfo, statErr := os.Stat(conf.IncludeFolderPath())
		if statErr != nil {
			return fmt.Errorf("include folder: %w", statErr)
		}
		if !includeInfo.IsDir() {
			return fmt.Errorf("include folder is not a directory: %s", conf.IncludeFolderPath())
		}
	}
	if err = validateMaxDPR(conf.MaxDPR); err != nil {
		return
	}
	if conf.OptimBatchSize < 0 {
		return fmt.Errorf("optimbatchsize must be zero or greater, got %d", conf.OptimBatchSize)
	}

	foundImages, err := discoverImages(&conf, path.Join(conf.basePath, conf.InputFolder))
	if err != nil {
		return
	}

	manifestPath := conf.ManifestFile
	if manifestPath != "" {
		manifestPath = path.Join(conf.basePath, manifestPath)
	}

	oldManifest, err := readManifest(manifestPath)
	if err != nil {
		return
	}

	err = os.MkdirAll(conf.ImageFolderPath(), os.ModePerm)
	if err != nil {
		return
	}

	newManifest, pathToSlug, err := buildNewManifest(&conf, foundImages, oldManifest)
	if err != nil {
		return
	}

	err = saveManifest(manifestPath, newManifest)
	if err != nil {
		return
	}

	// TODO also clean up files when not copying
	if !conf.DoCopy {
		return
	}

	// fmt.Printf("%v\n", pathToSlug)

	transformConf := transformConfig{
		config:     &conf,
		manifest:   newManifest,
		pathToSlug: pathToSlug,
	}
	whitelist, err := transferAndTransformAll(&transformConf)
	if err != nil {
		return
	}

	// whitelist all our files
	imageFolder := conf.ImageFolderPath()
	for _, bImg := range newManifest {
		for _, bImgFile := range bImg.Files {
			whitelist = append(whitelist, path.Join(imageFolder, bImgFile.FileName))
		}
		for _, bImgFile := range bImg.WebPFiles {
			whitelist = append(whitelist, path.Join(imageFolder, bImgFile.FileName))
		}
	}

	// fmt.Printf("Keep: %v\n", whitelist)

	if conf.languageFilter == "" {
		err = deleteNonWhitelist(&conf, whitelist)
	} else {
		err = deleteNonWhitelistUnder(&conf, whitelist, path.Join(conf.basePath, conf.OutputFolder, conf.languageFilter))
	}

	return
}

func main() {
	basePath := flag.String("basepath", ".", "The folder in which to search for enfasten.yml")
	cull := flag.Bool("cull", false, "Whether to cull inefficient images this run")
	language := flag.String("lang", "", "Only transform and generate new WebP images beneath this input language directory")
	flag.Parse()
	err := buildFastSite(*basePath, *cull, *language)
	if err != nil {
		log.Fatal("FATAL ERROR: ", err)
	}
}
