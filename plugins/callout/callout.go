// Package callout renders admonition fences:
//
//	```note Optional title
//	Markdown body
//	```
//
// Supported fence names: note, tip, info, warning, caution, danger.
package callout

import (
	"fmt"
	"html"
	"strings"

	"github.com/ericevans16/sitemaker/pkg/plugin"
)

var kinds = map[string]string{
	"note":    "Note",
	"tip":     "Tip",
	"info":    "Info",
	"warning": "Warning",
	"caution": "Caution",
	"danger":  "Danger",
}

type calloutPlugin struct{}

func init() { plugin.Register(&calloutPlugin{}, 150) }

func (*calloutPlugin) Name() string  { return "callout" }
func (*calloutPlugin) JS() [][]byte  { return nil }
func (*calloutPlugin) CSS() [][]byte { return [][]byte{[]byte(css)} }

func (*calloutPlugin) RenderFence(lang, code string, ctx plugin.FenceContext) (string, bool, error) {
	key := strings.ToLower(strings.TrimSpace(lang))
	defaultTitle, ok := kinds[key]
	if !ok {
		return "", false, nil
	}
	title := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(ctx.Info), lang))
	if title == "" {
		title = defaultTitle
	}
	body := strings.TrimSpace(code)
	// Paragraphs are separated by blank lines; inline text is escaped so
	// raw HTML cannot slip through an admonition block.
	var paras []string
	for _, p := range strings.Split(body, "\n\n") {
		if p = strings.TrimSpace(p); p != "" {
			paras = append(paras, "<p>"+strings.ReplaceAll(html.EscapeString(p), "\n", "<br>")+"</p>")
		}
	}
	return fmt.Sprintf(
		`<aside class="callout callout--%s"><div class="callout-head mono">%s</div><div class="callout-body">%s</div></aside>`,
		key, html.EscapeString(title), strings.Join(paras, ""),
	), true, nil
}

const css = `
.callout {
  margin: var(--gap-2) 0;
  border: var(--wire-w) solid var(--border-light);
  background-color: var(--card-bg);
}
.callout-head {
  padding: 6px var(--cell-x);
  border-bottom: var(--wire-w) solid var(--border-soft);
  background-image: var(--hatch-pattern);
  font-size: 0.72rem;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  color: var(--text-muted);
}
.callout--tip .callout-head,
.callout--note .callout-head,
.callout--info .callout-head {
  color: var(--primary-color);
}
.callout-body {
  padding: var(--cell-y) var(--cell-x);
}
.callout-body p {
  margin: 0;
}
.callout-body p + p {
  margin-top: var(--gap-1);
}
`
