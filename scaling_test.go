package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
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

	roundTripped, err := readManifest(manifestPath)
	if err != nil {
		t.Fatalf("readManifest() error = %v", err)
	}
	if !reflect.DeepEqual(roundTripped, manifest) {
		t.Fatalf("readManifest() = %#v, want %#v", roundTripped, manifest)
	}
}
