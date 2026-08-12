package main

import "testing"

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
	want := `<picture><source type="image/webp" srcset="/assets/eimages/example-a1b2c3d4-original.webp 1000w, /assets/eimages/example-a1b2c3d4-500px.webp 500w" sizes="(min-width: 60em) 30em, 90vw"><img alt="Example" loading="lazy" src="/assets/eimages/example-a1b2c3d4-original.png" srcset="/assets/eimages/example-a1b2c3d4-original.png 1000w, /assets/eimages/example-a1b2c3d4-500px.png 500w" sizes="(min-width: 60em) 30em, 90vw" width="1000" height="600"></picture>`

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
