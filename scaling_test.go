package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestSaveManifestSortsTopLevelKeys(t *testing.T) {
	manifestPath := filepath.Join(t.TempDir(), "manifest.yml")
	manifest := map[string]builtImage{
		"zeta":   {OriginalName: "zeta.png", Files: []builtImageFile{}},
		"alpha":  {OriginalName: "alpha.png", Files: []builtImageFile{}},
		"middle": {OriginalName: "middle.png", Files: []builtImageFile{}},
	}

	if err := saveManifest(manifestPath, manifest); err != nil {
		t.Fatalf("saveManifest() error = %v", err)
	}

	contents, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	var topLevelKeys []string
	for _, line := range strings.Split(string(contents), "\n") {
		if line != "" && !strings.HasPrefix(line, " ") {
			topLevelKeys = append(topLevelKeys, strings.TrimSuffix(line, ":"))
		}
	}

	want := []string{"alpha", "middle", "zeta"}
	if !reflect.DeepEqual(topLevelKeys, want) {
		t.Fatalf("manifest keys = %v, want %v", topLevelKeys, want)
	}
	fixedTime := time.Unix(1_700_000_000, 0)
	if err := os.Chtimes(manifestPath, fixedTime, fixedTime); err != nil {
		t.Fatal(err)
	}
	if err := saveManifest(manifestPath, manifest); err != nil {
		t.Fatalf("second saveManifest() error = %v", err)
	}
	secondContents, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("second ReadFile() error = %v", err)
	}
	if string(secondContents) != string(contents) {
		t.Fatal("saveManifest() changed an unchanged manifest")
	}
	manifestInfo, err := os.Stat(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if !manifestInfo.ModTime().Equal(fixedTime) {
		t.Fatalf("unchanged manifest mtime = %v, want %v", manifestInfo.ModTime(), fixedTime)
	}

	roundTripped, err := readManifest(manifestPath)
	if err != nil {
		t.Fatalf("readManifest() error = %v", err)
	}
	if !reflect.DeepEqual(roundTripped, manifest) {
		t.Fatalf("readManifest() = %#v, want %#v", roundTripped, manifest)
	}
}

func TestOptimizationBatches(t *testing.T) {
	images := []string{"a", "b", "c", "d", "e"}
	want := [][]string{{"a", "b"}, {"c", "d"}, {"e"}}
	if got := optimizationBatches(images, 2); !reflect.DeepEqual(got, want) {
		t.Fatalf("optimizationBatches() = %#v, want %#v", got, want)
	}

	want = [][]string{images}
	for _, batchSize := range []int{0, 5, 10} {
		if got := optimizationBatches(images, batchSize); !reflect.DeepEqual(got, want) {
			t.Fatalf("optimizationBatches(batchSize: %d) = %#v, want %#v", batchSize, got, want)
		}
	}
}

func TestManifestWebPConfigurationInvalidatesStaleEntries(t *testing.T) {
	basePath := t.TempDir()
	imagePath := filepath.Join(basePath, "site", "en", "images", "example.png")
	conf := &config{
		InputFolder: "site",
		WebPCommand: []string{"cwebp", "-q", "92"},
		SizesRules: []sizesRule{
			{Pattern: "**/images/example.png", WebP: true},
		},
		basePath: basePath,
	}
	built := builtImage{
		Width:         1000,
		Files:         []builtImageFile{{FileName: "example.png", Width: 1000}},
		WebPFiles:     []builtImageFile{{FileName: "example.webp", Width: 1000}},
		WebPSignature: conf.webPCommandSignature(),
	}

	if !manifestHasConfiguredWidths(conf, imagePath, built) {
		t.Fatal("matching WebP manifest entry should remain valid")
	}

	missingWebP := built
	missingWebP.WebPFiles = nil
	if manifestHasConfiguredWidths(conf, imagePath, missingWebP) {
		t.Fatal("manifest entry without WebP candidates should be invalid")
	}

	conf.WebPCommand = []string{"cwebp", "-q", "95"}
	if manifestHasConfiguredWidths(conf, imagePath, built) {
		t.Fatal("encoder setting change should invalidate the WebP manifest entry")
	}

	conf.SizesRules[0].WebP = false
	if manifestHasConfiguredWidths(conf, imagePath, built) {
		t.Fatal("removing the WebP rule should invalidate an entry with stale WebP candidates")
	}
}

func TestLanguageFilterDefersWebPGenerationOutsideSelectedLocale(t *testing.T) {
	basePath := t.TempDir()
	imagePath := filepath.Join(basePath, "site", "en", "images", "example.png")
	conf := &config{
		InputFolder:    "site",
		WebPCommand:    []string{"cwebp"},
		languageFilter: "de",
		SizesRules: []sizesRule{
			{Pattern: "**/images/example.png", WebP: true},
		},
		basePath: basePath,
	}
	built := builtImage{
		Width: 1000,
		Files: []builtImageFile{{FileName: "example.png", Width: 1000}},
	}

	if conf.shouldGenerateWebP(imagePath) {
		t.Fatal("English image should not generate WebP during a German-only build")
	}
	if !manifestHasConfiguredWidths(conf, imagePath, built) {
		t.Fatal("deferred WebP work should not invalidate an unselected locale entry")
	}

	conf.languageFilter = "en"
	if !conf.shouldGenerateWebP(imagePath) {
		t.Fatal("English image should generate WebP during an English-only build")
	}
	if manifestHasConfiguredWidths(conf, imagePath, built) {
		t.Fatal("selected locale entry without WebP candidates should be invalid")
	}
}

func TestWebPCandidateFilesOmitsOversizedOriginal(t *testing.T) {
	files := []builtImageFile{
		{FileName: "example-original.png", Width: 3000},
		{FileName: "example-1980px.png", Width: 1980},
		{FileName: "example-990px.png", Width: 990},
	}
	got := webPCandidateFiles(files)
	if len(got) != 2 || got[0].Width != 1980 || got[1].Width != 990 {
		t.Fatalf("webPCandidateFiles() = %v, want the two scaled candidates", got)
	}

	oneFile := files[:1]
	if got := webPCandidateFiles(oneFile); len(got) != 1 || got[0].Width != 3000 {
		t.Fatalf("webPCandidateFiles() = %v, want the only available original", got)
	}
}

func TestSelectedLocaleUpgradesDuplicateSlug(t *testing.T) {
	basePath := t.TempDir()
	conf := &config{
		InputFolder:    "site",
		WebPCommand:    []string{"cwebp"},
		languageFilter: "en",
		SizesRules: []sizesRule{
			{Pattern: "**/images/example.png", WebP: true},
		},
		basePath: basePath,
	}
	built := builtImage{
		Width: 1000,
		Files: []builtImageFile{{FileName: "example.png", Width: 1000}},
	}

	germanPath := filepath.Join(basePath, "site", "de", "images", "example.png")
	if shouldRebuildDuplicate(conf, germanPath, built) {
		t.Fatal("deferred duplicate should not overwrite the selected locale entry")
	}

	englishPath := filepath.Join(basePath, "site", "en", "images", "example.png")
	if !shouldRebuildDuplicate(conf, englishPath, built) {
		t.Fatal("selected duplicate should upgrade an entry without WebP candidates")
	}

	built.WebPFiles = []builtImageFile{{FileName: "example.webp", Width: 1000}}
	built.WebPSignature = conf.webPCommandSignature()
	if shouldRebuildDuplicate(conf, englishPath, built) {
		t.Fatal("complete selected duplicate should remain incremental")
	}
}
