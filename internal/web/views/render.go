package views

import (
	"bytes"
	"html/template"
	"sync"

	chromahtml "github.com/alecthomas/chroma/v2/formatters/html"
	"github.com/yuin/goldmark"
	highlighting "github.com/yuin/goldmark-highlighting/v2"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer/html"
)

// markdown is the shared goldmark instance configured for ailog: GFM
// (tables, strikethrough, autolink), inline HTML allowed (callers pre-
// sanitize since the source is local user content), syntax highlighting
// via chroma. Constructed lazily so cold start cost is paid only when
// detail pages are actually viewed.
var (
	mdOnce sync.Once
	md     goldmark.Markdown
)

func mdInstance() goldmark.Markdown {
	mdOnce.Do(func() {
		md = goldmark.New(
			goldmark.WithExtensions(
				extension.GFM,
				highlighting.NewHighlighting(
					highlighting.WithStyle("github"),
					highlighting.WithFormatOptions(
						chromahtml.WithClasses(false), // inline styles, no separate CSS file needed
						chromahtml.TabWidth(4),
					),
				),
			),
			goldmark.WithParserOptions(parser.WithAutoHeadingID()),
			goldmark.WithRendererOptions(html.WithUnsafe()),
		)
	})
	return md
}

// RenderMarkdown converts a markdown source string to safe HTML for
// embedding in templ files. Returns template.HTML so the templ
// renderer trusts the markup (we trust our own markdown→HTML pipeline,
// not arbitrary user JS).
func RenderMarkdown(src string) template.HTML {
	if src == "" {
		return ""
	}
	var buf bytes.Buffer
	if err := mdInstance().Convert([]byte(src), &buf); err != nil {
		// On parse failure fall back to escaped plaintext — never
		// emit half-rendered output.
		return template.HTML(template.HTMLEscapeString(src))
	}
	return template.HTML(buf.String())
}
