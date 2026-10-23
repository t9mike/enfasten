package main

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPageImageWidthCap(t *testing.T) {
	for _, test := range []struct {
		name     string
		original int
		override *int
		media    string
		want     string
	}{
		{name: "oversized original", original: 6268, want: "1980px"},
		{name: "original at cap", original: 1980, want: "original"},
		{name: "original below cap", original: 1440, want: "original"},
		{name: "larger override", original: 2880, override: new(2880), want: "original"},
		{name: "disabled override", original: 6268, override: new(0), want: "original"},
		{name: "smaller override", original: 6268, override: new(990), want: "990px"},
		{name: "media gated", original: 6268, media: "(min-width: 40em)", want: "1980px"},
	} {
		t.Run(test.name, func(t *testing.T) {
			files := []builtImageFile{{FileName: "hero-original.png", Width: test.original}}
			for _, width := range []int{1980, 990} {
				if width < test.original {
					files = append(files, builtImageFile{FileName: fmt.Sprintf("hero-%dpx.png", width), Width: width})
				}
			}
			var webp []builtImageFile
			for _, file := range files {
				webp = append(webp, builtImageFile{FileName: webPFileName(file.FileName), Width: file.Width})
			}
			conf := &transformConfig{
				config:     &config{ImageFolder: "assets/eimages", MaxWidth: 1980, SizesRules: []sizesRule{{Pattern: "hero.png", MaxWidth: test.override, Media: test.media}}},
				manifest:   map[string]builtImage{"hero": {OriginalName: "hero-original.png", Width: test.original, Files: files, WebPFiles: webp}},
				pathToSlug: map[string]string{"hero.png": "hero"},
			}
			got := string(mustRebuildImage(t, conf, ".", imgRegex.FindSubmatch([]byte(`<img src="hero.png">`))))
			for _, extension := range []string{"png", "webp"} {
				if !strings.Contains(got, "hero-"+test.want+"."+extension) {
					t.Fatalf("missing capped %s candidate: %s", extension, got)
				}
			}
			if test.want != "original" && strings.Contains(got, "hero-original") {
				t.Fatalf("oversized original leaked into page: %s", got)
			}
			if test.media == "" && !strings.Contains(got, `src="/assets/eimages/hero-`+test.want+`.png"`) {
				t.Fatalf("fallback src is not capped: %s", got)
			}
			if len(conf.manifest["hero"].Files) != len(files) {
				t.Fatal("page filtering modified the download manifest")
			}
		})
	}
}

func TestMissingCappedCandidateFailsInsteadOfLoadingOriginal(t *testing.T) {
	conf := exampleTransformConfig()
	conf.MaxWidth = 100
	_, err := rebuildImage(conf, "en", imgRegex.FindSubmatch([]byte(`<img src="images/example.png">`)))
	if err == nil || !strings.Contains(err.Error(), "no page-image candidate") {
		t.Fatalf("error = %v, want missing capped candidate", err)
	}
}

func TestWidthCapUpgradesCachedImagesAndPreservesDownloads(t *testing.T) {
	base := t.TempDir()
	for _, locale := range []string{"en", "de"} {
		dir := filepath.Join(base, "site", locale)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		file, err := os.Create(filepath.Join(dir, "hero.png"))
		if err != nil {
			t.Fatal(err)
		}
		if err := png.Encode(file, image.NewNRGBA(image.Rect(0, 0, 2000, 20))); err != nil {
			t.Fatal(err)
		}
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
		page := `<a href="hero.png" download>Download original</a><img src="hero.png">`
		if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte(page), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	configPath := filepath.Join(base, "enfasten.yml")
	configText := "inputfolder: site\noutputfolder: fastsite\nimagefolder: assets/eimages\nwidths: [990]\n"
	if err := os.WriteFile(configPath, []byte(configText+"maxwidth: 0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := buildFastSite(base, false, ""); err != nil {
		t.Fatal(err)
	}
	// Inherit the 1980 default; the identical German file needs an 800px cap.
	configText += "sizesrules:\n  - pattern: de/hero.png\n    maxwidth: 800\n"
	if err := os.WriteFile(configPath, []byte(configText), 0o644); err != nil {
		t.Fatal(err)
	}
	for run := 0; run < 2; run++ {
		if err := buildFastSite(base, run == 1, ""); err != nil {
			t.Fatal(err)
		}
		for locale, cap := range map[string]int{"en": 1980, "de": 800} {
			page, err := os.ReadFile(filepath.Join(base, "fastsite", locale, "index.html"))
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(page), "-original.png") || !strings.Contains(string(page), fmt.Sprintf("-%dpx.png", cap)) {
				t.Fatalf("uncapped page for %s on run %d: %s", locale, run, page)
			}
			if !strings.Contains(string(page), `<a href="hero.png" download>`) {
				t.Fatal("download link was changed")
			}
			source, err := os.ReadFile(filepath.Join(base, "site", locale, "hero.png"))
			if err != nil {
				t.Fatal(err)
			}
			download, err := os.ReadFile(filepath.Join(base, "fastsite", locale, "hero.png"))
			if err != nil || !bytes.Equal(source, download) {
				t.Fatalf("download changed: %v", err)
			}
		}
		manifest, err := readManifest(filepath.Join(base, "enfasten_manifest.yml"))
		if err != nil {
			t.Fatal(err)
		}
		if len(manifest) != 1 {
			t.Fatalf("expected shared image, got %d entries", len(manifest))
		}
		for _, built := range manifest {
			if _, err := os.Stat(filepath.Join(base, "fastsite/assets/eimages", built.OriginalName)); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestCapBypassesPNGAndJPEGResizeThresholds(t *testing.T) {
	conf := &config{MaxWidth: 1980, ScaleThreshold: 0.9, JpgScaleThreshold: 0.7}
	for _, extension := range []string{".png", ".jpg"} {
		got := configuredScaledWidths(conf, "hero"+extension, 2000)
		if len(got) != 1 || got[0] != 1980 {
			t.Fatalf("%s widths = %v", extension, got)
		}
		if got := configuredScaledWidths(conf, "hero"+extension, 1980); len(got) != 0 {
			t.Fatalf("upscaled original: %v", got)
		}
	}
}

func TestInvalidWidthCapsFail(t *testing.T) {
	for _, settings := range []string{"maxwidth: -1\n", "sizesrules:\n  - pattern: '**/*.png'\n    maxwidth: -1\n"} {
		base := t.TempDir()
		if err := os.WriteFile(filepath.Join(base, "enfasten.yml"), []byte(settings), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := buildFastSite(base, false, ""); err == nil || !strings.Contains(err.Error(), "maxwidth") {
			t.Fatalf("expected invalid cap error, got %v", err)
		}
	}
}
