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
	OriginalName string
	Width        int
	Height       int
	Files        []builtImageFile
}

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

	df, err := os.Create(manifestPath)
	if err != nil {
		return err
	}
	defer df.Close()
	_, err = df.Write(out)

	return
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
	return true
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

	return
}

func optimizeImages(conf *config, newImages []string) (err error) {
	if conf.OptimCommand == nil || len(newImages) == 0 {
		return
	}

	args := append(conf.OptimCommand, newImages...)
	log.Printf("Optimizing with %v", args)
	cmd := exec.Command(args[0], args[1:len(args)]...)
	err = cmd.Run()
	return
}

// sometimes optimizing images leads to ones with larger dimensions actually
// having smaller file size, if we notice this, we want to cut the inefficient
// files out of our manifest.
func cullImages(conf *config, built builtImage) builtImage {
	if !conf.doCulling {
		return built
	}

	newFiles := []builtImageFile{}
	var bestSize int64 = 1000000000 // arbitrary large number, 1GB
	imageFolder := conf.ImageFolderPath()
	for _, builtFile := range built.Files {
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

	built.Files = newFiles
	return built
}

func buildNewManifest(conf *config, foundImages []foundImage, oldManifest map[string]builtImage) (newManifest map[string]builtImage, pathToSlug map[string]string, err error) {
	newManifest = map[string]builtImage{}
	pathToSlug = map[string]string{}
	newImages := []string{}
	inputPath := path.Join(conf.basePath, conf.InputFolder)
	for _, img := range foundImages {
		slug := getSlug(img.Path, img.Hash)
		if built, ok := oldManifest[slug]; ok && manifestHasConfiguredWidths(conf, img.Path, built) {
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

	log.Printf("New images: %v", newImages)

	err = optimizeImages(conf, newImages)

	return
}
