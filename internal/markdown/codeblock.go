package markdown

import (
	"bytes"
	stdhtml "html"
	"strings"

	"github.com/ericevans16/sitemaker/internal/icons"
	"github.com/ericevans16/sitemaker/pkg/plugin"

	"github.com/alecthomas/chroma/v2"
	chromahtml "github.com/alecthomas/chroma/v2/formatters/html"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/util"
)

type codeBlockRenderer struct {
	state *state
	style string
}

func (r *codeBlockRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(ast.KindFencedCodeBlock, r.renderFencedCodeBlock)
}

func (r *codeBlockRenderer) renderFencedCodeBlock(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}
	n := node.(*ast.FencedCodeBlock)
	lang := string(n.Language(source))
	info := ""
	if n.Info != nil {
		info = string(n.Info.Segment.Value(source))
	}

	var raw bytes.Buffer
	lines := n.Lines()
	for i := 0; i < lines.Len(); i++ {
		line := lines.At(i)
		raw.Write(line.Value(source))
	}
	code := raw.String()

	if lang == "mermaid" {
		w.WriteString(`<pre class="mermaid">`)
		w.WriteString(stdhtml.EscapeString(code))
		w.WriteString(`</pre>`)
		return ast.WalkSkipChildren, nil
	}

	trimmed := strings.TrimLeft(code, "\n\r")
	out, handled, err := r.state.plugins.RenderFence(lang, trimmed, plugin.FenceContext{Document: r.state.document, Info: info})
	if err != nil {
		return ast.WalkStop, err
	}
	if handled {
		w.WriteString(out)
		return ast.WalkSkipChildren, nil
	}

	label := lang
	if label == "" {
		label = "code"
	}
	w.WriteString(`<div class="code-block"><div class="code-block-header"><div class="code-block-header-left"><span class="code-block-lang">`)
	w.WriteString(stdhtml.EscapeString(label))
	w.WriteString(`</span></div><div class="code-block-header-right">`)
	// <rain-code-copy> owns the copy behaviour (theme JS).
	w.WriteString(`<rain-code-copy><button class="copy-btn" type="button" aria-label="Copy code" title="Copy code">`)
	if copyIcon, err := icons.Func()("copy", "copy-icon"); err == nil {
		w.WriteString(string(copyIcon))
	}
	w.WriteString(`<span class="copy-text">Copy</span></button></rain-code-copy></div></div><pre><code`)
	if lang != "" {
		w.WriteString(` class="language-` + stdhtml.EscapeString(lang) + `"`)
	}
	w.WriteString(`>`)
	w.WriteString(r.highlight(lang, strings.TrimRight(trimmed, "\n")))
	w.WriteString(`</code></pre></div>`)
	return ast.WalkSkipChildren, nil
}

// highlight returns Chroma class-based markup, or escaped text when the
// language is unknown or empty.
func (r *codeBlockRenderer) highlight(lang, code string) string {
	if lang == "" {
		return stdhtml.EscapeString(code)
	}
	lexer := lexers.Get(lang)
	if lexer == nil {
		return stdhtml.EscapeString(code)
	}
	lexer = chroma.Coalesce(lexer)
	style := styles.Get(r.style)
	if style == nil {
		style = styles.Fallback
	}
	formatter := chromahtml.New(chromahtml.WithClasses(true), chromahtml.TabWidth(4), chromahtml.PreventSurroundingPre(true))
	it, err := lexer.Tokenise(nil, code)
	if err != nil {
		return stdhtml.EscapeString(code)
	}
	var buf bytes.Buffer
	if err := formatter.Format(&buf, style, it); err != nil {
		return stdhtml.EscapeString(code)
	}
	return strings.TrimSpace(buf.String())
}
