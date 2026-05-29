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
