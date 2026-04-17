package resource

import (
	"bytes"
	"fmt"
	"html"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
)

func BuildLinks(filename, directURL string, resourceType Type) Links {
	escapedName := html.EscapeString(filename)
	switch resourceType {
	case TypeImage:
		return Links{
			Direct:   directURL,
			Markdown: fmt.Sprintf("![%s](%s)", filename, directURL),
			HTML:     fmt.Sprintf(`<img src="%s" alt="%s">`, directURL, escapedName),
			BBCode:   fmt.Sprintf("[img]%s[/img]", directURL),
		}
	default:
		return Links{
			Direct:   directURL,
			Markdown: fmt.Sprintf("[%s](%s)", filename, directURL),
			HTML:     fmt.Sprintf(`<a href="%s">%s</a>`, directURL, escapedName),
			BBCode:   fmt.Sprintf("[url=%s]%s[/url]", directURL, filename),
		}
	}
}

func DecodeImageConfig(content []byte) (width, height int, ok bool) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(content))
	if err != nil {
		return 0, 0, false
	}
	return cfg.Width, cfg.Height, true
}
