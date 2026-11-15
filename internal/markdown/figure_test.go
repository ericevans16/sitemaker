package markdown

import (
	"strings"
	"testing"
)

func renderFigure(t *testing.T, source string) string {
	t.Helper()
	html, err := New(Options{}).Convert("test.md", source)
	if err != nil {
		t.Fatalf("render markdown: %v", err)
	}
	return html
}

func TestLoneImageIsWrappedInRainFigure(t *testing.T) {
	html := renderFigure(t, "前文\n\n![架构图](https://example.com/arch.png)\n\n后文\n")
	if !strings.Contains(html, `<rain-figure><img src="https://example.com/arch.png"`) {
		t.Fatalf("expected lone image wrapped in <rain-figure>, got %q", html)
	}
	for _, want := range []string{`alt="架构图"`, `loading="lazy"`, `decoding="async"`} {
		if !strings.Contains(html, want) {
			t.Fatalf("expected %s preserved, got %q", want, html)
		}
	}
}

func TestBadgeImagesStayPlainParagraphs(t *testing.T) {
	html := renderFigure(t, "![Static Badge](https://img.shields.io/badge/x-green)\n")
	if strings.Contains(html, "<rain-figure>") {
		t.Fatalf("badge image must not be wrapped, got %q", html)
	}
}

func TestHeadingAnchorPreservesCJK(t *testing.T) {
	html := renderFigure(t, "## 慢下来的力量\n\n## 慢下来的力量\n")
	if !strings.Contains(html, `id="慢下来的力量"`) || !strings.Contains(html, `id="慢下来的力量-1"`) {
		t.Fatalf("unexpected heading IDs: %s", html)
	}
}

func TestMermaidBlocksAreEscaped(t *testing.T) {
	html := renderFigure(t, "```mermaid\ngraph TD;\nA[<script>alert(1)</script>]-->B\n```")
	if strings.Contains(html, "<script>") || !strings.Contains(html, "&lt;script&gt;") {
		t.Fatalf("mermaid block was not escaped: %s", html)
	}
}
