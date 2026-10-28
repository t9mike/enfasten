package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestClickableImagesPreserveLinksAndMarkup(t *testing.T) {
	for _, test := range []struct{ name, input, want string }{
		{"relative source", `<img src="images/full.png" alt="Screenshot" data-enfasten-clickable>`, `<a href="images/full.png"><img src="images/full.png" alt="Screenshot"></a>`},
		{"existing link", `<a href="/download"><span><img data-enfasten-clickable src="full.png"></span></a>`, `<a href="/download"><span><img src="full.png"></span></a>`},
		{"existing button", `<button><img src="full.png" data-enfasten-clickable></button>`, `<button><img src="full.png"></button>`},
		{"opt out", `<img src="full.png" data-enfasten-clickable="false">`, `<img src="full.png">`},
		{"unmarked", `<img src="full.png">`, `<img src="full.png">`},
		{"passthrough svg and empty alt", `<img data-enfasten-clickable='true' src='/icons/star.svg' alt='' />`, `<a href="/icons/star.svg" aria-label="star.svg"><img src='/icons/star.svg' alt='' /></a>`},
		{"escaped URL", `<IMG SRC='/full.png?a=1&amp;b=2' ALT='A > B' DATA-ENFASTEN-CLICKABLE>`, `<a href="/full.png?a=1&amp;b=2"><IMG SRC='/full.png?a=1&amp;b=2' ALT='A > B'></a>`},
		{"attribute text", `<img src="full.png" alt="example data-enfasten-clickable">`, `<img src="full.png" alt="example data-enfasten-clickable">`},
		{"authored picture", `<picture><source srcset="small.webp"><img src="full.png" alt="Full" data-enfasten-clickable></picture>`, `<a href="full.png"><picture><source srcset="small.webp"><img src="full.png" alt="Full"></picture></a>`},
		{"linked picture", `<a href="/keep"><picture><img src="full.png" data-enfasten-clickable></picture></a>`, `<a href="/keep"><picture><img src="full.png"></picture></a>`},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := linkClickableImages([]byte(test.input), "")
			if err != nil || string(got) != test.want {
				t.Fatalf("got %s, %v; want %s", got, err, test.want)
			}
			again, err := linkClickableImages(got, "")
			if err != nil || string(again) != string(got) {
				t.Fatalf("second transformation changed the output: %s, %v", again, err)
			}
		})
	}
}

func TestClickableImagesIgnoreCommentsAndRawText(t *testing.T) {
	prefix := `<!-- <a><img src="comment.png" data-enfasten-clickable> -->` +
		`<script>const template = '<a><img src="script.png" data-enfasten-clickable>';</script>` +
		`<style>/* <a> */</style><textarea><a><img data-enfasten-clickable></textarea>`
	got, err := linkClickableImages([]byte(prefix+`<img src=full.png alt=Full data-enfasten-clickable>`), "")
	want := prefix + `<a href="full.png"><img src=full.png alt=Full></a>`
	if err != nil || string(got) != want {
		t.Fatalf("got %s, %v; want %s", got, err, want)
	}
}

func TestClickableImagesRejectInvalidMarkers(t *testing.T) {
	for _, input := range []string{
		`<img src="full.png" data-enfasten-clickable="yes">`,
		`<img src="full.png" data-enfasten-clickable data-enfasten-clickable>`,
		`<img data-enfasten-clickable>`,
		`<picture><img src="full.png" data-enfasten-clickable>`,
	} {
		if _, err := linkClickableImages([]byte(input), ""); err == nil {
			t.Fatalf("expected error for %s", input)
		}
	}
}

func TestClickableImageWindowTarget(t *testing.T) {
	for _, test := range []struct{ target, attributes string }{
		{"", ""},
		{"_blank", ` target="_blank" rel="noopener"`},
		{"original-images", ` target="original-images"`},
		{"_self", ` target="_self"`},
		{`image"window`, ` target="image&#34;window"`},
	} {
		got, err := linkClickableImages([]byte(`<img src="full.png" alt="Full" data-enfasten-clickable>`), test.target)
		want := `<a href="full.png"` + test.attributes + `><img src="full.png" alt="Full"></a>`
		if err != nil || string(got) != want {
			t.Fatalf("target %q: got %s, %v; want %s", test.target, got, err, want)
		}
	}
}

func TestClickableImageLinksOriginalThroughIncludesAndResponsiveRewrite(t *testing.T) {
	conf := exampleTransformConfig()
	conf.basePath = t.TempDir()
	conf.InputFolder = "site"
	conf.IncludeFolder = "includes"
	conf.MaxWidth = 990
	built := conf.manifest["example"]
	built.Files = append(built.Files, builtImageFile{FileName: "example-a1b2c3d4-250px.png", Width: 250})
	built.WebPFiles = []builtImageFile{{FileName: "example-a1b2c3d4-500px.webp", Width: 500}}
	conf.manifest["example"] = built
	page := filepath.Join(conf.basePath, "site", "en", "index.html")
	includeDir := filepath.Join(conf.basePath, "site", "includes")
	for _, dir := range []string{filepath.Dir(page), includeDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(page, []byte(`<!-- include image.html -->`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(includeDir, "image.html"), []byte(`<img src="images/example.png" alt="Example" data-enfasten-clickable>`), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(conf.basePath, "output.html")
	if err := translateHtml(conf, page, out); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	if !strings.HasPrefix(got, `<a href="images/example.png"><picture>`) || !strings.HasSuffix(got, `</picture></a>`) {
		t.Fatalf("original link must wrap the generated picture: %s", got)
	}
	if !strings.Contains(got, `srcset=`) || strings.Contains(got, "data-enfasten-clickable") || strings.Contains(got, "-original") {
		t.Fatalf("responsive candidates must stay capped and build marker must be consumed: %s", got)
	}
}
