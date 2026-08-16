package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExpandIncludesRecursively(t *testing.T) {
	basePath := t.TempDir()
	includePath := filepath.Join(basePath, "site", "includes")
	if err := os.MkdirAll(includePath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(includePath, "outer.html"), []byte("before <!-- include inner.html --> after"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(includePath, "inner.html"), []byte("inside"), 0o644); err != nil {
		t.Fatal(err)
	}

	conf := &config{basePath: basePath, InputFolder: "site", IncludeFolder: "includes"}
	got, err := expandIncludes(conf, filepath.Join(basePath, "site", "en", "index.html"), []byte(`start <!-- include "outer.html" --> end`), nil)
	if err != nil {
		t.Fatal(err)
	}
	if want := "start before inside after end"; string(got) != want {
		t.Fatalf("expandIncludes() = %q, want %q", got, want)
	}
}

func TestExpandIncludesIndentsStandaloneFragments(t *testing.T) {
	basePath := t.TempDir()
	includePath := filepath.Join(basePath, "site", "includes")
	if err := os.MkdirAll(includePath, 0o755); err != nil {
		t.Fatal(err)
	}
	fragment := "<script>\n\n    example();\n</script>\n"
	if err := os.WriteFile(filepath.Join(includePath, "script.html"), []byte(fragment), 0o644); err != nil {
		t.Fatal(err)
	}

	conf := &config{basePath: basePath, InputFolder: "site", IncludeFolder: "includes"}
	contents := []byte("<head>\n    <!-- include script.html -->\n    <title>Example</title>\n</head>\n")
	got, err := expandIncludes(conf, filepath.Join(basePath, "site", "en", "index.html"), contents, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := "<head>\n    <script>\n\n        example();\n    </script>\n    <title>Example</title>\n</head>\n"
	if string(got) != want {
		t.Fatalf("expandIncludes() = %q, want %q", got, want)
	}
}

func TestExpandIncludesRejectsMissingTraversalAndCycles(t *testing.T) {
	basePath := t.TempDir()
	includePath := filepath.Join(basePath, "site", "includes")
	if err := os.MkdirAll(includePath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(includePath, "cycle.html"), []byte("<!-- include cycle.html -->"), 0o644); err != nil {
		t.Fatal(err)
	}

	conf := &config{basePath: basePath, InputFolder: "site", IncludeFolder: "includes"}
	tests := []struct {
		contents string
		want     string
	}{
		{contents: "<!-- include missing.html -->", want: "missing.html"},
		{contents: "<!-- include ../secret.html -->", want: "invalid include path"},
		{contents: "<!-- include cycle.html -->", want: "include cycle"},
	}
	for _, test := range tests {
		_, err := expandIncludes(conf, filepath.Join(basePath, "site", "en", "index.html"), []byte(test.contents), nil)
		if err == nil || !strings.Contains(err.Error(), test.want) {
			t.Fatalf("expandIncludes(%q) error = %v, want containing %q", test.contents, err, test.want)
		}
	}
}

func TestTransferAndTransformAllDoesNotPublishIncludes(t *testing.T) {
	basePath := t.TempDir()
	includePath := filepath.Join(basePath, "site", "includes")
	pagePath := filepath.Join(basePath, "site", "en", "index.html")
	for _, dirPath := range []string{includePath, filepath.Dir(pagePath)} {
		if err := os.MkdirAll(dirPath, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(includePath, "head.html"), []byte("<meta name=example>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pagePath, []byte("<head><!-- include head.html --></head>"), 0o644); err != nil {
		t.Fatal(err)
	}

	conf := &transformConfig{
		config:     &config{basePath: basePath, InputFolder: "site", OutputFolder: "fastsite", IncludeFolder: "includes"},
		manifest:   map[string]builtImage{},
		pathToSlug: map[string]string{},
	}
	if _, err := transferAndTransformAll(conf); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(filepath.Join(basePath, "fastsite", "en", "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != "<head><meta name=example></head>" {
		t.Fatalf("transformed page = %q", contents)
	}
	if _, err := os.Stat(filepath.Join(basePath, "fastsite", "includes")); !os.IsNotExist(err) {
		t.Fatalf("include folder was published: %v", err)
	}
}

func TestTranslateHtmlTransformsImagesFromIncludes(t *testing.T) {
	basePath := t.TempDir()
	includePath := filepath.Join(basePath, "site", "includes")
	pagePath := filepath.Join(basePath, "site", "en", "index.html")
	outputPath := filepath.Join(basePath, "fastsite", "en", "index.html")
	for _, dirPath := range []string{includePath, filepath.Dir(pagePath), filepath.Dir(outputPath)} {
		if err := os.MkdirAll(dirPath, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(includePath, "image.html"), []byte(`<img alt="Example" src="images/example.png">`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pagePath, []byte("<!-- include image.html -->"), 0o644); err != nil {
		t.Fatal(err)
	}

	conf := exampleTransformConfig()
	conf.basePath = basePath
	conf.InputFolder = "site"
	conf.IncludeFolder = "includes"
	if err := translateHtml(conf, pagePath, outputPath); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(contents), `/assets/eimages/example-a1b2c3d4-original.png`) {
		t.Fatalf("included image was not transformed: %s", contents)
	}
}

func TestRebuildImageClosesSrcsetQuote(t *testing.T) {
	conf := &transformConfig{
		config: &config{
			ImageFolder: "assets/eimages",
		},
		manifest: map[string]builtImage{
			"example": {
				OriginalName: "example-a1b2c3d4-original.png",
				Files: []builtImageFile{
					{FileName: "example-a1b2c3d4-original.png", Width: 1000},
					{FileName: "example-a1b2c3d4-500px.png", Width: 500},
				},
			},
		},
		pathToSlug: map[string]string{
			"images/example.png": "example",
		},
	}

	match := []byte(`<img alt="Example" src="images/example.png">`)
	captures := imgRegex.FindSubmatch(match)
	got := string(rebuildImage(conf, ".", captures))
	want := `<img alt="Example" src="/assets/eimages/example-a1b2c3d4-original.png" srcset="/assets/eimages/example-a1b2c3d4-original.png 1000w, /assets/eimages/example-a1b2c3d4-500px.png 500w">`

	if got != want {
		t.Fatalf("rebuildImage() = %q, want %q", got, want)
	}
}

func TestRebuildImageUsesMatchingSizesRule(t *testing.T) {
	conf := exampleTransformConfig()
	conf.SizesRules = []sizesRule{
		{Pattern: "**/images/example.png", Sizes: "(min-width: 60em) 30em, 90vw"},
	}

	match := []byte(`<img alt="Example" src="images/example.png">`)
	got := string(rebuildImage(conf, "en", imgRegex.FindSubmatch(match)))
	want := `<img alt="Example" src="/assets/eimages/example-a1b2c3d4-original.png" srcset="/assets/eimages/example-a1b2c3d4-original.png 1000w, /assets/eimages/example-a1b2c3d4-500px.png 500w" sizes="(min-width: 60em) 30em, 90vw">`

	if got != want {
		t.Fatalf("rebuildImage() = %q, want %q", got, want)
	}
}

func TestRebuildImageUsesWebPPictureAndIntrinsicDimensions(t *testing.T) {
	conf := exampleTransformConfig()
	conf.SizesRules = []sizesRule{
		{Pattern: "**/images/example.png", Sizes: "(min-width: 60em) 30em, 90vw", WebP: true},
	}
	built := conf.manifest["example"]
	built.Width = 1000
	built.Height = 600
	built.WebPFiles = []builtImageFile{
		{FileName: "example-a1b2c3d4-original.webp", Width: 1000, Height: 600},
		{FileName: "example-a1b2c3d4-500px.webp", Width: 500, Height: 300},
	}
	conf.manifest["example"] = built

	match := []byte(`<img alt="Example" loading="lazy" src="images/example.png">`)
	got := string(rebuildImage(conf, "en", imgRegex.FindSubmatch(match)))
	want := `<picture><source type="image/webp" srcset="/assets/eimages/example-a1b2c3d4-original.webp 1000w, /assets/eimages/example-a1b2c3d4-500px.webp 500w" sizes="(min-width: 60em) 30em, 90vw" width="1000" height="600"><img alt="Example" loading="lazy" src="/assets/eimages/example-a1b2c3d4-original.png" srcset="/assets/eimages/example-a1b2c3d4-original.png 1000w, /assets/eimages/example-a1b2c3d4-500px.png 500w" sizes="(min-width: 60em) 30em, 90vw" width="1000" height="600"></picture>`

	if got != want {
		t.Fatalf("rebuildImage() = %q, want %q", got, want)
	}
}

func TestRebuildImageMediaGatesModernAndFallbackSources(t *testing.T) {
	conf := exampleTransformConfig()
	conf.SizesRules = []sizesRule{
		{
			Pattern: "**/images/example.png",
			Sizes:   "90vw",
			Media:   "(max-width: 40em)",
			WebP:    true,
		},
	}
	built := conf.manifest["example"]
	built.Width = 1000
	built.Height = 600
	built.WebPFiles = []builtImageFile{
		{FileName: "example-a1b2c3d4-original.webp", Width: 1000, Height: 600},
		{FileName: "example-a1b2c3d4-500px.webp", Width: 500, Height: 300},
	}
	conf.manifest["example"] = built

	match := []byte(`<img alt="Example" loading="lazy" src="images/example.png">`)
	got := string(rebuildImage(conf, "en", imgRegex.FindSubmatch(match)))
	want := `<picture><source type="image/webp" media="(max-width: 40em)" srcset="/assets/eimages/example-a1b2c3d4-original.webp 1000w, /assets/eimages/example-a1b2c3d4-500px.webp 500w" sizes="90vw" width="1000" height="600"><source media="(max-width: 40em)" srcset="/assets/eimages/example-a1b2c3d4-original.png 1000w, /assets/eimages/example-a1b2c3d4-500px.png 500w" sizes="90vw" width="1000" height="600"><img alt="Example" loading="lazy" src="` + transparentImage + `" width="1000" height="600"></picture>`

	if got != want {
		t.Fatalf("rebuildImage() = %q, want %q", got, want)
	}
}

func TestRebuildImageUsesAndStripsPerImageMediaGate(t *testing.T) {
	conf := exampleTransformConfig()
	built := conf.manifest["example"]
	built.Width = 1000
	built.Height = 600
	conf.manifest["example"] = built

	match := []byte(`<img alt="Example" data-enfasten-media="(min-width: 40em)" sizes="50vw" src="images/example.png">`)
	got := string(rebuildImage(conf, "en", imgRegex.FindSubmatch(match)))
	want := `<picture><source media="(min-width: 40em)" srcset="/assets/eimages/example-a1b2c3d4-original.png 1000w, /assets/eimages/example-a1b2c3d4-500px.png 500w" sizes="50vw" width="1000" height="600"><img alt="Example" src="` + transparentImage + `" width="1000" height="600"></picture>`

	if got != want {
		t.Fatalf("rebuildImage() = %q, want %q", got, want)
	}
	if strings.Contains(got, "data-enfasten-media") {
		t.Fatalf("build-only media attribute leaked into output: %q", got)
	}
}

func TestRebuildImageCanDisableRuleMediaGate(t *testing.T) {
	conf := exampleTransformConfig()
	conf.SizesRules = []sizesRule{
		{Pattern: "**/images/example.png", Media: "(max-width: 40em)"},
	}

	match := []byte(`<img alt="Example" data-enfasten-media="none" src="images/example.png">`)
	got := string(rebuildImage(conf, "en", imgRegex.FindSubmatch(match)))
	want := `<img alt="Example" src="/assets/eimages/example-a1b2c3d4-original.png" srcset="/assets/eimages/example-a1b2c3d4-original.png 1000w, /assets/eimages/example-a1b2c3d4-500px.png 500w">`

	if got != want {
		t.Fatalf("rebuildImage() = %q, want %q", got, want)
	}
}

func TestRebuildImageCopiesAuthoredDimensionsToWebPSource(t *testing.T) {
	conf := exampleTransformConfig()
	conf.SizesRules = []sizesRule{
		{Pattern: "**/images/example.png", WebP: true},
	}
	built := conf.manifest["example"]
	built.Width = 1000
	built.Height = 600
	built.WebPFiles = []builtImageFile{
		{FileName: "example-a1b2c3d4-original.webp", Width: 1000, Height: 600},
		{FileName: "example-a1b2c3d4-500px.webp", Width: 500, Height: 300},
	}
	conf.manifest["example"] = built

	match := []byte(`<img alt="Example" width='250' src="images/example.png" height=150>`)
	got := string(rebuildImage(conf, "en", imgRegex.FindSubmatch(match)))
	want := `<picture><source type="image/webp" srcset="/assets/eimages/example-a1b2c3d4-original.webp 1000w, /assets/eimages/example-a1b2c3d4-500px.webp 500w" width="250" height="150"><img alt="Example" width='250' src="/assets/eimages/example-a1b2c3d4-original.png" srcset="/assets/eimages/example-a1b2c3d4-original.png 1000w, /assets/eimages/example-a1b2c3d4-500px.png 500w" height=150></picture>`

	if got != want {
		t.Fatalf("rebuildImage() = %q, want %q", got, want)
	}
}

func TestRebuildImageKeepsSourceDimensions(t *testing.T) {
	conf := exampleTransformConfig()
	built := conf.manifest["example"]
	built.Width = 1000
	built.Height = 600
	conf.manifest["example"] = built

	match := []byte(`<img alt="Example" src="images/example.png" width="250" height="150">`)
	got := string(rebuildImage(conf, "en", imgRegex.FindSubmatch(match)))
	want := `<img alt="Example" src="/assets/eimages/example-a1b2c3d4-original.png" srcset="/assets/eimages/example-a1b2c3d4-original.png 1000w, /assets/eimages/example-a1b2c3d4-500px.png 500w" width="250" height="150">`

	if got != want {
		t.Fatalf("rebuildImage() = %q, want %q", got, want)
	}
}

func TestRebuildImageDoesNotAddDimensionsWithoutModernSource(t *testing.T) {
	conf := exampleTransformConfig()
	built := conf.manifest["example"]
	built.Width = 1000
	built.Height = 600
	conf.manifest["example"] = built

	match := []byte(`<img alt="Example" src="images/example.png">`)
	got := string(rebuildImage(conf, "en", imgRegex.FindSubmatch(match)))
	want := `<img alt="Example" src="/assets/eimages/example-a1b2c3d4-original.png" srcset="/assets/eimages/example-a1b2c3d4-original.png 1000w, /assets/eimages/example-a1b2c3d4-500px.png 500w">`

	if got != want {
		t.Fatalf("rebuildImage() = %q, want %q", got, want)
	}
}

func TestRebuildImageDoesNotMixSourceAndGeneratedDimensions(t *testing.T) {
	conf := exampleTransformConfig()
	built := conf.manifest["example"]
	built.Width = 1000
	built.Height = 600
	conf.manifest["example"] = built

	match := []byte(`<img alt="Example" src="images/example.png" width="250">`)
	got := string(rebuildImage(conf, "en", imgRegex.FindSubmatch(match)))
	want := `<img alt="Example" src="/assets/eimages/example-a1b2c3d4-original.png" srcset="/assets/eimages/example-a1b2c3d4-original.png 1000w, /assets/eimages/example-a1b2c3d4-500px.png 500w" width="250">`

	if got != want {
		t.Fatalf("rebuildImage() = %q, want %q", got, want)
	}
}

func TestRebuildImageKeepsSourceSizesInsteadOfAddingRule(t *testing.T) {
	conf := exampleTransformConfig()
	conf.SizesRules = []sizesRule{
		{Pattern: "**/images/example.png", Sizes: "100vw"},
	}

	match := []byte(`<img alt="Example" src="images/example.png" sizes="50vw">`)
	got := string(rebuildImage(conf, "en", imgRegex.FindSubmatch(match)))
	want := `<img alt="Example" src="/assets/eimages/example-a1b2c3d4-original.png" srcset="/assets/eimages/example-a1b2c3d4-original.png 1000w, /assets/eimages/example-a1b2c3d4-500px.png 500w" sizes="50vw">`

	if got != want {
		t.Fatalf("rebuildImage() = %q, want %q", got, want)
	}
}

func TestMaxDPRSizesPreservesLowerDensityAndCapsHigherDensity(t *testing.T) {
	got, err := maxDPRSizes("(max-width: 40em) 90vw, 600px", 2)
	if err != nil {
		t.Fatal(err)
	}
	want := "(max-width: 40em) and (min-resolution: 3.75dppx) calc((90vw) * 0.5), " +
		"(min-resolution: 3.75dppx) calc((600px) * 0.5), " +
		"(max-width: 40em) and (min-resolution: 3.25dppx) calc((90vw) * 0.5714), " +
		"(min-resolution: 3.25dppx) calc((600px) * 0.5714), " +
		"(max-width: 40em) and (min-resolution: 2.75dppx) calc((90vw) * 0.6667), " +
		"(min-resolution: 2.75dppx) calc((600px) * 0.6667), " +
		"(max-width: 40em) and (min-resolution: 2.25dppx) calc((90vw) * 0.8), " +
		"(min-resolution: 2.25dppx) calc((600px) * 0.8), " +
		"(max-width: 40em) 90vw, 600px"
	if got != want {
		t.Fatalf("maxDPRSizes() = %q, want %q", got, want)
	}
}

func TestRebuildImageUsesGlobalMaxDPR(t *testing.T) {
	conf := exampleTransformConfig()
	conf.MaxDPR = 2
	conf.SizesRules = []sizesRule{
		{Pattern: "**/images/example.png", Sizes: "90vw"},
	}

	match := []byte(`<img alt="Example" src="images/example.png">`)
	got := string(rebuildImage(conf, "en", imgRegex.FindSubmatch(match)))
	if !strings.Contains(got, `(min-resolution: 2.75dppx) calc((90vw) * 0.6667)`) {
		t.Fatalf("global maxdpr was not applied: %q", got)
	}
	if !strings.Contains(got, `, 90vw"`) {
		t.Fatalf("original sizes fallback was not preserved: %q", got)
	}
}

func TestRebuildImageUsesAndStripsPerImageMaxDPROverride(t *testing.T) {
	conf := exampleTransformConfig()
	conf.MaxDPR = 3

	match := []byte(`<img alt="Example" data-enfasten-max-dpr="2" sizes="90vw" src="images/example.png">`)
	got := string(rebuildImage(conf, "en", imgRegex.FindSubmatch(match)))
	if strings.Contains(got, "data-enfasten-max-dpr") {
		t.Fatalf("build-only max DPR attribute leaked into output: %q", got)
	}
	if !strings.Contains(got, `(min-resolution: 2.75dppx) calc((90vw) * 0.6667)`) {
		t.Fatalf("per-image max DPR override was not applied: %q", got)
	}
	if strings.Count(got, `sizes="`) != 1 {
		t.Fatalf("source-authored sizes should be rewritten exactly once: %q", got)
	}
}

func TestRebuildImageCanRaiseOrDisableGlobalMaxDPR(t *testing.T) {
	conf := exampleTransformConfig()
	conf.MaxDPR = 2

	match := []byte(`<img alt="Example" data-enfasten-max-dpr="3" sizes="90vw" src="images/example.png">`)
	got := string(rebuildImage(conf, "en", imgRegex.FindSubmatch(match)))
	if strings.Contains(got, "2.75dppx") || !strings.Contains(got, "3.25dppx") {
		t.Fatalf("per-image max DPR 3 did not replace global max DPR 2: %q", got)
	}

	match = []byte(`<img alt="Example" data-enfasten-max-dpr="none" sizes="90vw" src="images/example.png">`)
	got = string(rebuildImage(conf, "en", imgRegex.FindSubmatch(match)))
	if strings.Contains(got, "min-resolution") || strings.Contains(got, "data-enfasten-max-dpr") {
		t.Fatalf("per-image max DPR none did not disable and strip the directive: %q", got)
	}
}

func TestValidateMaxDPR(t *testing.T) {
	for _, value := range []float64{0, 1, 2, 2.5, 3, 4} {
		if err := validateMaxDPR(value); err != nil {
			t.Fatalf("validateMaxDPR(%g) = %v", value, err)
		}
	}
	for _, value := range []float64{-1, 0.5, 4.5} {
		if err := validateMaxDPR(value); err == nil {
			t.Fatalf("validateMaxDPR(%g) should fail", value)
		}
	}
}

func TestWidthsForImageAddsOnlyMatchingRuleWidths(t *testing.T) {
	conf := &config{
		InputFolder: "site",
		Widths:      []int{330, 660},
		SizesRules: []sizesRule{
			{Pattern: "**/images/hero.png", Widths: []int{2200}},
		},
		basePath: "/example",
	}

	got := conf.widthsForImage("/example/site/en/images/hero.png")
	want := []int{330, 660, 2200}
	if len(got) != len(want) {
		t.Fatalf("widthsForImage() = %v, want %v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("widthsForImage() = %v, want %v", got, want)
		}
	}

	unmatched := conf.widthsForImage("/example/site/en/images/other.png")
	if len(unmatched) != 2 || unmatched[0] != 330 || unmatched[1] != 660 {
		t.Fatalf("unmatched widthsForImage() = %v, want [330 660]", unmatched)
	}
}

func exampleTransformConfig() *transformConfig {
	return &transformConfig{
		config: &config{
			ImageFolder: "assets/eimages",
		},
		manifest: map[string]builtImage{
			"example": {
				OriginalName: "example-a1b2c3d4-original.png",
				Files: []builtImageFile{
					{FileName: "example-a1b2c3d4-original.png", Width: 1000},
					{FileName: "example-a1b2c3d4-500px.png", Width: 500},
				},
			},
		},
		pathToSlug: map[string]string{
			"en/images/example.png": "example",
		},
	}
}
