package main

import (
	"bytes"
	"fmt"
	"html"
	"path"
	"regexp"
	"sort"
	"strings"
)

// Preserve source formatting while recognizing tags, quoted attribute values,
// comments and raw-text elements. In particular, an <a> inside a comment or
// script must not make subsequent images appear to be already linked.
var clickableTokenRegex = regexp.MustCompile(`(?is)<!--.*?-->|<script\b[^>]*>.*?</script\s*>|<style\b[^>]*>.*?</style\s*>|<textarea\b[^>]*>.*?</textarea\s*>|<title\b[^>]*>.*?</title\s*>|</?[a-z][a-z0-9:-]*(?:[^<>"']|"[^"]*"|'[^']*')*>`)
var clickableTagRegex = regexp.MustCompile(`(?i)^<(/?)([a-z][a-z0-9:-]*)`)
var clickableAttributeRegex = regexp.MustCompile(`\s+([^\s=/>]+)(?:\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s>]+)))?`)

type htmlEdit struct {
	start, end int
	text       string
}

// Link before responsive rewriting so href retains the author's source URL,
// while rebuildImage remains free to replace src and generate a picture.
func linkClickableImages(contents []byte, target string) ([]byte, error) {
	target = strings.TrimSpace(target)
	var edits []htmlEdit
	anchorDepth, buttonDepth := 0, 0
	pictureStart := -1
	pictureLink := ""
	for _, span := range clickableTokenRegex.FindAllIndex(contents, -1) {
		token := contents[span[0]:span[1]]
		tag := clickableTagRegex.FindSubmatchIndex(token)
		if tag == nil {
			continue
		}
		name := strings.ToLower(string(token[tag[4]:tag[5]]))
		closing := tag[3] > tag[2]
		switch name {
		case "a":
			if closing {
				anchorDepth = max(0, anchorDepth-1)
			} else {
				anchorDepth++
			}
		case "button":
			if closing {
				buttonDepth = max(0, buttonDepth-1)
			} else {
				buttonDepth++
			}
		case "picture":
			if closing {
				if pictureLink != "" {
					edits = append(edits, htmlEdit{pictureStart, pictureStart, pictureLink}, htmlEdit{span[1], span[1], "</a>"})
				}
				pictureStart, pictureLink = -1, ""
			} else {
				pictureStart = span[0]
			}
		case "img":
			attrs := token[tag[1]:]
			values := map[string]string{}
			marked, enabled := false, false
			var clean bytes.Buffer
			last := 0
			for _, attr := range clickableAttributeRegex.FindAllSubmatchIndex(attrs, -1) {
				key := strings.ToLower(string(attrs[attr[2]:attr[3]]))
				value := ""
				for i := 4; i < len(attr); i += 2 {
					if attr[i] >= 0 {
						value = html.UnescapeString(string(attrs[attr[i]:attr[i+1]]))
						break
					}
				}
				values[key] = value
				if key != "data-enfasten-clickable" {
					continue
				}
				if marked {
					return nil, fmt.Errorf("an img may only specify data-enfasten-clickable once")
				}
				marked = true
				switch strings.ToLower(strings.TrimSpace(value)) {
				case "", "true":
					enabled = true
				case "false":
				default:
					return nil, fmt.Errorf("invalid data-enfasten-clickable value %q: use an empty value, true or false", value)
				}
				clean.Write(attrs[last:attr[0]])
				last = attr[1]
			}
			if !marked {
				continue
			}
			clean.Write(attrs[last:])
			replacement := string(token[:tag[1]]) + clean.String()
			if enabled && anchorDepth == 0 && buttonDepth == 0 {
				src := values["src"]
				if strings.TrimSpace(src) == "" {
					return nil, fmt.Errorf("data-enfasten-clickable requires an img src")
				}
				link := `<a href="` + html.EscapeString(src) + `"`
				if target != "" {
					link += ` target="` + html.EscapeString(target) + `"`
					// noopener makes named targets behave like _blank, preventing
					// reuse of the requested window. Only apply it to new tabs.
					if strings.EqualFold(target, "_blank") {
						link += ` rel="noopener"`
					}
				}
				// An empty alt previously marked a decorative image. Once linked,
				// it needs an accessible name without inventing localized prose.
				if strings.TrimSpace(values["alt"]) == "" {
					link += ` aria-label="` + html.EscapeString(path.Base(strings.SplitN(src, "?", 2)[0])) + `"`
				}
				link += ">"
				if pictureStart >= 0 {
					pictureLink = link
				} else {
					replacement = link + replacement + "</a>"
				}
			}
			edits = append(edits, htmlEdit{span[0], span[1], replacement})
		}
	}
	if pictureLink != "" {
		return nil, fmt.Errorf("clickable image is inside an unclosed picture")
	}
	sort.SliceStable(edits, func(i, j int) bool { return edits[i].start < edits[j].start })
	var out bytes.Buffer
	last := 0
	for _, edit := range edits {
		out.Write(contents[last:edit.start])
		out.WriteString(edit.text)
		last = edit.end
	}
	out.Write(contents[last:])
	return out.Bytes(), nil
}
