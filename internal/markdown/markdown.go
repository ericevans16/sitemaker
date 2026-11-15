// Package markdown is sitemaker's goldmark pipeline: GFM, footnotes,
// Unicode-aware heading anchors, Chroma highlighting, Mermaid, foldable
// figures, image size probing and plugin-provided fence renderers.
package markdown

import (
	"bytes"
	"strconv"
	"strings"
	"unicode"

	"github.com/ericevans16/sitemaker/pkg/plugin"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	goldmarkhtml "github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/util"
)

// Renderer converts Markdown to HTML. It is not safe for concurrent use:
// the current document name is tracked for diagnostics.
type Renderer struct {
	md    goldmark.Markdown
	state *state
}

type state struct {
	document string
	plugins  *plugin.Registry
}

// Options tunes the pipeline.
type Options struct {
	Plugins *plugin.Registry // fence renderers + markdown extenders; may be nil
	// CodeStyle is the Chroma style name used for class generation.
	CodeStyle string
}

// New builds a renderer.
func New(opts Options) *Renderer {
	if opts.Plugins == nil {
		opts.Plugins = plugin.NewRegistry()
	}
	st := &state{plugins: opts.Plugins}

	exts := []goldmark.Extender{
		extension.GFM,
		extension.Footnote,
		&core{state: st, style: opts.CodeStyle},
	}
	_ = plugin.Each(opts.Plugins, func(p plugin.MarkdownExtender) error {
		exts = append(exts, p.MarkdownExtensions()...)
		return nil
	})

	md := goldmark.New(
		goldmark.WithExtensions(exts...),
		goldmark.WithParserOptions(parser.WithAutoHeadingID()),
		goldmark.WithRendererOptions(goldmarkhtml.WithUnsafe()),
	)
	return &Renderer{md: md, state: st}
}

// Convert renders source; document names the file for plugin diagnostics.
func (r *Renderer) Convert(document, source string) (string, error) {
	r.state.document = document
	defer func() { r.state.document = "" }()

	var buf bytes.Buffer
	ctx := parser.NewContext(parser.WithIDs(newIDs()))
	if err := r.md.Convert([]byte(source), &buf, parser.WithContext(ctx)); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// core wires sitemaker's node renderers into goldmark.
type core struct {
	state *state
	style string
}

func (c *core) Extend(m goldmark.Markdown) {
	m.Renderer().AddOptions(renderer.WithNodeRenderers(
		util.Prioritized(&codeBlockRenderer{state: c.state, style: c.style}, 0),
		util.Prioritized(&imageRenderer{}, 0),
		util.Prioritized(&paragraphRenderer{}, 0),
	))
}

// ids generates heading anchors that keep CJK and other letters, so a
// heading "## 慢下来的力量" gets id="慢下来的力量" instead of "heading".
type ids struct{ seen map[string]int }

func newIDs() *ids { return &ids{seen: map[string]int{}} }

func (s *ids) Generate(value []byte, _ ast.NodeKind) []byte {
	base := Slugify(string(value))
	if base == "" {
		base = "section"
	}
	id := base
	for n := 1; s.seen[id] > 0; n++ {
		id = base + "-" + strconv.Itoa(n)
	}
	s.seen[id] = 1
	return []byte(id)
}

func (s *ids) Put(value []byte) { s.seen[string(value)] = 1 }

// Slugify lowercases text and joins runs of letters/digits with '-'.
// Non-Latin letters are kept, so Chinese tags stay readable in URLs.
func Slugify(text string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(strings.TrimSpace(text)) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
			dash = false
		case r == '-' || r == '_' || r == '.' || unicode.IsSpace(r) || unicode.IsPunct(r) || unicode.IsSymbol(r):
			if !dash && b.Len() > 0 {
				b.WriteByte('-')
				dash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}
