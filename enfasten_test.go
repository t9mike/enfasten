package main

import (
	"os"
	"path/filepath"
	"testing"
)

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
