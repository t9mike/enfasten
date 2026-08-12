package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCopyFilePreservesSourceModificationTime(t *testing.T) {
	basePath := t.TempDir()
	sourcePath := filepath.Join(basePath, "source.txt")
	destPath := filepath.Join(basePath, "dest.txt")
	if err := os.WriteFile(sourcePath, []byte("first"), 0o644); err != nil {
		t.Fatal(err)
	}
	firstTime := time.Unix(1_700_000_000, 0)
	if err := os.Chtimes(sourcePath, firstTime, firstTime); err != nil {
		t.Fatal(err)
	}
	if err := copyFile(sourcePath, destPath); err != nil {
		t.Fatal(err)
	}

	destInfo, err := os.Stat(destPath)
	if err != nil {
		t.Fatal(err)
	}
	if !destInfo.ModTime().Equal(firstTime) {
		t.Fatalf("destination mtime = %v, want %v", destInfo.ModTime(), firstTime)
	}

	if err := os.WriteFile(sourcePath, []byte("second"), 0o644); err != nil {
		t.Fatal(err)
	}
	secondTime := firstTime.Add(time.Hour)
	if err := os.Chtimes(sourcePath, secondTime, secondTime); err != nil {
		t.Fatal(err)
	}
	if err := copyFile(sourcePath, destPath); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(destPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != "second" {
		t.Fatalf("destination contents = %q, want %q", contents, "second")
	}
	destInfo, err = os.Stat(destPath)
	if err != nil {
		t.Fatal(err)
	}
	if !destInfo.ModTime().Equal(secondTime) {
		t.Fatalf("updated destination mtime = %v, want %v", destInfo.ModTime(), secondTime)
	}
}

func TestWriteFileIfChangedPreservesUnchangedModificationTime(t *testing.T) {
	filePath := filepath.Join(t.TempDir(), "output.html")
	if err := os.WriteFile(filePath, []byte("unchanged"), 0o644); err != nil {
		t.Fatal(err)
	}
	fixedTime := time.Unix(1_700_000_000, 0)
	if err := os.Chtimes(filePath, fixedTime, fixedTime); err != nil {
		t.Fatal(err)
	}

	if err := writeFileIfChanged(filePath, []byte("unchanged")); err != nil {
		t.Fatal(err)
	}
	fileInfo, err := os.Stat(filePath)
	if err != nil {
		t.Fatal(err)
	}
	if !fileInfo.ModTime().Equal(fixedTime) {
		t.Fatalf("unchanged mtime = %v, want %v", fileInfo.ModTime(), fixedTime)
	}

	if err := writeFileIfChanged(filePath, []byte("changed")); err != nil {
		t.Fatal(err)
	}
	fileInfo, err = os.Stat(filePath)
	if err != nil {
		t.Fatal(err)
	}
	if fileInfo.ModTime().Equal(fixedTime) {
		t.Fatal("changed output kept the old modification time")
	}
}

func TestNormalizeLanguageFilter(t *testing.T) {
	for _, language := range []string{"en", "pt-BR", "zh-Hans"} {
		got, err := normalizeLanguageFilter(language)
		if err != nil || got != language {
			t.Fatalf("normalizeLanguageFilter(%q) = %q, %v", language, got, err)
		}
	}

	for _, language := range []string{".", "..", "en/../de", "/en", "en/"} {
		if _, err := normalizeLanguageFilter(language); err == nil {
			t.Fatalf("normalizeLanguageFilter(%q) should fail", language)
		}
	}
}

func TestDeleteNonWhitelistUnderPreservesOtherLocales(t *testing.T) {
	basePath := t.TempDir()
	conf := &config{basePath: basePath, OutputFolder: "fastsite"}
	englishPath := filepath.Join(basePath, "fastsite", "en")
	keepPath := filepath.Join(englishPath, "keep.html")
	removePath := filepath.Join(englishPath, "remove.html")
	otherLocalePath := filepath.Join(basePath, "fastsite", "de", "stay.html")

	for _, filePath := range []string{keepPath, removePath, otherLocalePath} {
		if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filePath, []byte("test"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	if err := deleteNonWhitelistUnder(conf, []string{keepPath}, englishPath); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(keepPath); err != nil {
		t.Fatalf("whitelisted English file was removed: %v", err)
	}
	if _, err := os.Stat(removePath); !os.IsNotExist(err) {
		t.Fatalf("non-whitelisted English file still exists: %v", err)
	}
	if _, err := os.Stat(otherLocalePath); err != nil {
		t.Fatalf("other locale file was removed: %v", err)
	}
}
