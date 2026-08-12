package main

import (
	"crypto/sha256"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"log"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/bamiaux/rez"
	"github.com/bmatcuk/doublestar"
	"gopkg.in/yaml.v2"
)

type foundImage struct {
	Path string
	Hash []byte
}

type builtImageFile struct {
	FileName string
	Width    int
	Height   int
}

type builtImage struct {
	OriginalName  string
	Width         int
	Height        int
	Files         []builtImageFile
	WebPFiles     []builtImageFile `yaml:"webpfiles,omitempty"`
	WebPSignature string           `yaml:"webpsignature,omitempty"`
}

const webPManifestVersion = "2"

func readManifest(manifestPath string) (manifest map[string]builtImage, err error) {
	if manifestPath == "" {
		return // use empty manifest
	}
	if _, statError := os.Stat(manifestPath); os.IsNotExist(statError) {
		log.Print("Can't find manifest, starting with an empty one")
		return // no manifest, starting with an empty one
	}
	bytes, err := readFileBytes(manifestPath)
	if err != nil {
		return
	}
	err = yaml.Unmarshal(bytes, &manifest)
	return
}

func saveManifest(manifestPath string, manifest map[string]builtImage) (err error) {
	if manifestPath == "" {
		return // don't persist manifest
	}

	// Go deliberately randomizes map iteration order. Serialize the top-level
	// image slugs in a canonical order so an unchanged build does not rewrite
	// the manifest with ordering-only differences.
	keys := make([]string, 0, len(manifest))
	for key := range manifest {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	orderedManifest := make(yaml.MapSlice, 0, len(keys))
	for _, key := range keys {
		orderedManifest = append(orderedManifest, yaml.MapItem{
			Key:   key,
			Value: manifest[key],
		})
	}

	out, err := yaml.Marshal(orderedManifest)
	if err != nil {
		return
	}

	return writeFileIfChanged(manifestPath, out)
}

func hashFile(path string) (hash []byte, err error) {
	f, err := os.OpenFile(path, os.O_RDONLY, 0)
	if err != nil {
		return
	}
	defer f.Close()

	h := sha256.New()
	if _, err = io.Copy(h, f); err != nil {
		return
	}

	hash = h.Sum(nil)
	return
}

func isBlacklisted(conf *config, filePath string) bool {
	if conf.Blacklist == nil {
		return false
	}
	relPath, err := filepath.Rel(conf.InputFolderPath(), filePath)
	if err != nil {
		log.Fatalf("%v", err)
	}
	for _, blackPath := range conf.Blacklist {
		matched, err := path.Match(blackPath, relPath)
		if err != nil {
			log.Fatalf("Invalid blacklist pattern '%s': %v", blackPath, err)
		}
		if matched {
			return true
		}
	}
	return false
}

func discoverImages(conf *config, inFolder string) (results []foundImage, err error) {
	matches, err := doublestar.Glob(path.Join(inFolder, "**/*.{png,jpg}"))
	if err != nil {
		return
	}

	for _, path := range matches {
		if isBlacklisted(conf, path) {
			continue
		}

		var hash []byte
		hash, err = hashFile(path)
		if err != nil {
			return
		}
		results = append(results, foundImage{path, hash})
	}
	return
}

func getSlug(imagePath string, hash []byte) string {
	_, baseName := path.Split(imagePath)
	extension := path.Ext(baseName)
	fileName := baseName[0 : len(baseName)-len(extension)]
	hashFragment := hash[0:4] // 2900 images of same name gives 0.1% chance of collision
	return fmt.Sprintf("%s-%x", fileName, hashFragment)
}

func downscaleImage(width int, height int, inputImage image.Image) (downscaled image.Image, err error) {
	// allocate correct buffer type
	r := image.Rect(0, 0, width, height)
	switch t := inputImage.(type) {
	case *image.YCbCr:
		downscaled = image.NewYCbCr(r, t.SubsampleRatio)
	case *image.RGBA:
		downscaled = image.NewRGBA(r)
	case *image.NRGBA:
		downscaled = image.NewNRGBA(r)
	case *image.Gray:
		downscaled = image.NewGray(r)
	default:
		err = fmt.Errorf("Unsupported image colour format %T.", inputImage)
	}

	err = rez.Convert(downscaled, inputImage, rez.NewLanczosFilter(3))
	return
}

func saveImage(conf *config, outPath string, extension string, img image.Image) (err error) {
	log.Printf("Saving %s with ext %s", outPath, extension)
	// encode the output
	df, err := os.Create(outPath)
	if err != nil {
		return err
	}
	defer df.Close()

	switch extension {
	case ".png":
		err = png.Encode(df, img)
	case ".jpg":
		options := jpeg.Options{Quality: conf.JpgQuality}
		err = jpeg.Encode(df, img, &options)
	default:
		err = fmt.Errorf("Unrecognized extension %s", extension)
	}

	return
}

func (conf *config) widthsForImage(imagePath string) []int {
	widthSet := make(map[int]bool, len(conf.Widths))
	for _, width := range conf.Widths {
		widthSet[width] = true
	}

	relPath, err := filepath.Rel(conf.InputFolderPath(), imagePath)
	if err == nil {
		if rule := conf.responsiveRuleForImage(filepath.ToSlash(relPath)); rule != nil {
			for _, width := range rule.Widths {
				widthSet[width] = true
			}
		}
	}

	widths := make([]int, 0, len(widthSet))
	for width := range widthSet {
		widths = append(widths, width)
	}
	sort.Ints(widths)
	return widths
}

func configuredScaledWidths(conf *config, imagePath string, sourceWidth int) []int {
	extension := path.Ext(imagePath)
	var widths []int
	for _, width := range conf.widthsForImage(imagePath) {
		if width >= sourceWidth {
			continue
		}
		downscaleRatio := float64(width) / float64(sourceWidth)
		if downscaleRatio > conf.ScaleThreshold {
			continue
		}
		if downscaleRatio > conf.JpgScaleThreshold && extension == ".jpg" {
			continue
		}
		widths = append(widths, width)
	}
	return widths
}

func manifestHasConfiguredWidths(conf *config, imagePath string, built builtImage) bool {
	builtWidths := make(map[int]bool, len(built.Files))
	for _, file := range built.Files {
		builtWidths[file.Width] = true
	}
	for _, width := range configuredScaledWidths(conf, imagePath, built.Width) {
		if !builtWidths[width] {
			return false
		}
	}
	if conf.shouldGenerateWebP(imagePath) {
		if built.WebPSignature != conf.webPCommandSignature() {
			return false
		}
		webPWidths := make(map[int]bool, len(built.WebPFiles))
		for _, file := range built.WebPFiles {
			webPWidths[file.Width] = true
		}
		for _, file := range webPCandidateFiles(built.Files) {
			if !webPWidths[file.Width] {
				return false
			}
		}
	} else if !conf.webPConfiguredForImage(imagePath) && (len(built.WebPFiles) > 0 || built.WebPSignature != "") {
		// Rebuild the manifest entry so removing a WebP rule also removes the
		// picture source and allows the obsolete generated files to be cleaned.
		return false
	}
	return true
}

func (conf *config) webPCommandSignature() string {
	parts := append([]string{webPManifestVersion}, conf.WebPCommand...)
	hash := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return fmt.Sprintf("%x", hash[:8])
}

func (conf *config) webPConfiguredForImage(imagePath string) bool {
	if len(conf.WebPCommand) == 0 {
		return false
	}
	relPath, err := filepath.Rel(conf.InputFolderPath(), imagePath)
	if err != nil {
		return false
	}
	rule := conf.responsiveRuleForImage(filepath.ToSlash(relPath))
	return rule != nil && rule.WebP
}

func (conf *config) shouldGenerateWebP(imagePath string) bool {
	if !conf.webPConfiguredForImage(imagePath) {
		return false
	}
	if conf.languageFilter == "" {
		return true
	}

	relPath, err := filepath.Rel(conf.InputFolderPath(), imagePath)
	if err != nil {
		return false
	}
	relPath = filepath.ToSlash(relPath)
	language := strings.Trim(conf.languageFilter, "/")
	return relPath == language || strings.HasPrefix(relPath, language+"/")
}

func webPFileName(fileName string) string {
	return strings.TrimSuffix(fileName, path.Ext(fileName)) + ".webp"
}

func webPCandidateFiles(files []builtImageFile) []builtImageFile {
	if len(files) > 1 {
		// Responsive rules describe bounded layout slots, so the largest scaled
		// candidate is sufficient. Keep the oversized original only as the
		// original-format fallback rather than offering it to WebP browsers.
		return files[1:]
	}
	return files
}

func encodeWebP(conf *config, inputPath string, outputPath string) error {
	temporaryPath := outputPath + ".tmp"
	_ = os.Remove(temporaryPath)
	args := append([]string{}, conf.WebPCommand[1:]...)
	args = append(args, inputPath, "-o", temporaryPath)
	log.Printf("Encoding WebP %s", outputPath)
	cmd := exec.Command(conf.WebPCommand[0], args...)
	if output, err := cmd.CombinedOutput(); err != nil {
		_ = os.Remove(temporaryPath)
		return fmt.Errorf("WebP encoder failed for %s: %w: %s", inputPath, err, output)
	}
	if err := os.Rename(temporaryPath, outputPath); err != nil {
		_ = os.Remove(temporaryPath)
		return err
	}
	return nil
}

func buildWebPImages(conf *config, imagePath string, built *builtImage) error {
	if !conf.shouldGenerateWebP(imagePath) {
		return nil
	}

	imageFolder := conf.ImageFolderPath()
	built.WebPSignature = conf.webPCommandSignature()
	var waitGroup sync.WaitGroup
	semaphore := make(chan struct{}, 3)
	var firstError error
	var errorMutex sync.Mutex
	for _, sourceFile := range webPCandidateFiles(built.Files) {
		outputFile := builtImageFile{
			FileName: webPFileName(sourceFile.FileName),
			Width:    sourceFile.Width,
			Height:   sourceFile.Height,
		}
		built.WebPFiles = append(built.WebPFiles, outputFile)

		outputPath := path.Join(imageFolder, outputFile.FileName)
		inputPath := path.Join(imageFolder, sourceFile.FileName)
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()
			if err := encodeWebP(conf, inputPath, outputPath); err != nil {
				errorMutex.Lock()
				if firstError == nil {
					firstError = err
				}
				errorMutex.Unlock()
			}
		}()
	}
	waitGroup.Wait()
	return firstError
}

func buildImage(conf *config, imagePath string, slug string, newImages *[]string) (built builtImage, err error) {
	log.Printf("Building image %s from %s", slug, imagePath)
	extension := path.Ext(imagePath)

	// load image
	f, err := os.OpenFile(imagePath, os.O_RDONLY, 0)
	if err != nil {
		f.Close()
		return
	}

	var inputImage image.Image
	switch extension {
	case ".png":
		inputImage, err = png.Decode(f)
	case ".jpg":
		inputImage, err = jpeg.Decode(f)
	default:
		err = fmt.Errorf("Unrecognized extension %s", extension)
		return
	}

	f.Close()
	if err != nil {
		return
	}
	built.Width = inputImage.Bounds().Dx()
	built.Height = inputImage.Bounds().Dy()

	// copy-paste original file
	imageFolder := conf.ImageFolderPath()
	originalName := fmt.Sprintf("%s-original%s", slug, extension)
	built.OriginalName = originalName
	originalPath := path.Join(imageFolder, originalName)

	if _, err = os.Stat(originalPath); os.IsNotExist(err) {
		*newImages = append(*newImages, originalPath)
		err = copyFile(imagePath, originalPath)
		if err != nil {
			return
		}
	} else {
		log.Printf("Original already copied, skipping: %s", originalPath)
	}

	builtOriginal := builtImageFile{FileName: originalName, Width: built.Width, Height: built.Height}
	built.Files = append(built.Files, builtOriginal)

	// resize to relevant sizes
	for _, w := range configuredScaledWidths(conf, imagePath, built.Width) {
		downscaleRatio := float64(w) / float64(built.Width)
		destHeight := int(float64(built.Height) * downscaleRatio)

		log.Printf("Downscaling %s from %v to (%d,%d)", slug, inputImage.Bounds(), w, destHeight)

		outName := fmt.Sprintf("%s-%dpx%s", slug, w, extension)
		outPath := path.Join(imageFolder, outName)

		builtScaled := builtImageFile{FileName: outName, Width: w, Height: destHeight}
		built.Files = append(built.Files, builtScaled)

		if _, err := os.Stat(outPath); !os.IsNotExist(err) {
			log.Printf("Image already exists, skipping: %s", outPath)
			continue // already exists
		}

		// do the scaling
		var downscaledImage image.Image
		downscaledImage, err = downscaleImage(w, destHeight, inputImage)
		if err != nil {
			return
		}

		*newImages = append(*newImages, outPath)
		err = saveImage(conf, outPath, extension, downscaledImage)
		if err != nil {
			return
		}
	}

	// the files list should always be sorted by decreasing width
	sort.Slice(built.Files, func(i, j int) bool {
		return built.Files[i].Width > built.Files[j].Width
	})
	if err = buildWebPImages(conf, imagePath, &built); err != nil {
		return
	}

	return
}

func optimizeImages(conf *config, newImages []string) (err error) {
	if conf.OptimCommand == nil || len(newImages) == 0 {
		return
	}

	batches := optimizationBatches(newImages, conf.OptimBatchSize)
	for index, batch := range batches {
		args := append(append([]string{}, conf.OptimCommand...), batch...)
		log.Printf("Optimizing batch %d/%d (%d images)", index+1, len(batches), len(batch))
		cmd := exec.Command(args[0], args[1:len(args)]...)
		if err = cmd.Run(); err != nil {
			return fmt.Errorf("optimization batch %d/%d failed: %w", index+1, len(batches), err)
		}
	}
	return
}

func optimizationBatches(images []string, batchSize int) [][]string {
	if len(images) == 0 {
		return nil
	}
	if batchSize <= 0 || batchSize >= len(images) {
		return [][]string{images}
	}

	batches := make([][]string, 0, (len(images)+batchSize-1)/batchSize)
	for start := 0; start < len(images); start += batchSize {
		end := start + batchSize
		if end > len(images) {
			end = len(images)
		}
		batches = append(batches, images[start:end])
	}
	return batches
}

// sometimes optimizing images leads to ones with larger dimensions actually
// having smaller file size, if we notice this, we want to cut the inefficient
// files out of our manifest.
func cullImages(conf *config, built builtImage) builtImage {
	if !conf.doCulling {
		return built
	}

	built.Files = cullImageFiles(conf, built.Files)
	built.WebPFiles = cullImageFiles(conf, built.WebPFiles)
	return built
}

func cullImageFiles(conf *config, files []builtImageFile) []builtImageFile {
	newFiles := []builtImageFile{}
	var bestSize int64 = 1000000000 // arbitrary large number, 1GB
	imageFolder := conf.ImageFolderPath()
	for _, builtFile := range files {
		info, err := os.Stat(path.Join(imageFolder, builtFile.FileName))
		if err != nil {
			log.Printf("Couldn't stat %s, removing from manifest", builtFile.FileName)
			continue // file doesn't exist, shouldn't be in manifest
		}
		if info.Size() < bestSize {
			bestSize = info.Size()
			newFiles = append(newFiles, builtFile)
		} else {
			log.Printf("Culling %s, it is %d bytes when a larger image was only %d bytes",
				builtFile.FileName, info.Size(), bestSize)
		}
	}

	return newFiles
}

func shouldRebuildDuplicate(conf *config, imagePath string, built builtImage) bool {
	return conf.shouldGenerateWebP(imagePath) && !manifestHasConfiguredWidths(conf, imagePath, built)
}

func buildNewManifest(conf *config, foundImages []foundImage, oldManifest map[string]builtImage) (newManifest map[string]builtImage, pathToSlug map[string]string, err error) {
	newManifest = map[string]builtImage{}
	pathToSlug = map[string]string{}
	newImages := []string{}
	inputPath := path.Join(conf.basePath, conf.InputFolder)
	for _, img := range foundImages {
		slug := getSlug(img.Path, img.Hash)
		if built, ok := newManifest[slug]; ok {
			// Identical files in multiple locales share a slug. Never let a
			// deferred locale overwrite WebP work already completed for the
			// selected locale, but allow the selected locale to upgrade an entry
			// first encountered outside the filter.
			if shouldRebuildDuplicate(conf, img.Path, built) {
				built, err = buildImage(conf, img.Path, slug, &newImages)
				if err != nil {
					return
				}
				newManifest[slug] = built
			}
		} else if built, ok := oldManifest[slug]; ok && manifestHasConfiguredWidths(conf, img.Path, built) {
			newManifest[slug] = cullImages(conf, built)
		} else {
			var built builtImage
			built, err = buildImage(conf, img.Path, slug, &newImages)
			if err != nil {
				return
			}
			newManifest[slug] = built
		}
		var relPath string
		relPath, err = filepath.Rel(inputPath, img.Path)
		if err != nil {
			return
		}
		pathToSlug[relPath] = slug
	}

	log.Printf("New images: %d", len(newImages))

	err = optimizeImages(conf, newImages)

	return
}
