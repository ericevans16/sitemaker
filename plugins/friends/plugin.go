// Package friends renders friend-link fenced blocks (```friends or
// ```better-friends) as accessible card grids.
package friends

import (
	_ "embed"
	"html"
	"net/url"
	"strings"

	"github.com/ericevans16/sitemaker/pkg/plugin"
)

//go:embed assets/better-friends.css
var cssAsset []byte

type friendsPlugin struct{}

type friend struct {
	name string
	url  string
}

var _ plugin.FenceRenderer = (*friendsPlugin)(nil)
var _ plugin.AssetProvider = (*friendsPlugin)(nil)

func init() { plugin.Register(&friendsPlugin{}, 200) }

func (*friendsPlugin) Name() string  { return "better-friends" }
func (*friendsPlugin) CSS() [][]byte { return [][]byte{cssAsset} }
func (*friendsPlugin) JS() [][]byte  { return nil }

// RenderFence renders lines in the form "- Name: [https://example.com/](https://example.com/)".
func (*friendsPlugin) RenderFence(lang, content string, _ plugin.FenceContext) (string, bool, error) {
	if lang != "better-friends" && lang != "friends" {
		return "", false, nil
	}

	list := parseFriends(content)
	if len(list) == 0 {
		return `<p class="better-friends-empty">No valid friend links were found.</p>`, true, nil
	}

	var out strings.Builder
	out.WriteString(`<nav class="better-friends-grid" aria-label="Friend links">`)
	for _, item := range list {
		out.WriteString(`<a class="better-friends-card" href="`)
		out.WriteString(html.EscapeString(item.url))
		out.WriteString(`" rel="noopener noreferrer">`)
		out.WriteString(html.EscapeString(item.name))
		out.WriteString(`</a>`)
	}
	out.WriteString(`</nav>`)
	return out.String(), true, nil
}

func parseFriends(content string) []friend {
	var list []friend
	for _, line := range strings.Split(content, "\n") {
		if name, target, ok := parseFriendLine(strings.TrimSpace(line)); ok {
			list = append(list, friend{name: name, url: target})
		}
	}
	return list
}

func parseFriendLine(line string) (name, target string, ok bool) {
	line = strings.TrimSpace(strings.TrimPrefix(line, "-"))
	name, value, found := strings.Cut(line, ":")
	if !found || strings.TrimSpace(name) == "" {
		return "", "", false
	}

	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, "[") {
		return "", "", false
	}
	closeLabel := strings.Index(value, "](")
	if closeLabel < 1 || !strings.HasSuffix(value, ")") {
		return "", "", false
	}
	label := strings.TrimSpace(value[1:closeLabel])
	target = strings.TrimSpace(value[closeLabel+2 : len(value)-1])
	if label == "" || !safeURL(target) {
		return "", "", false
	}
	return strings.TrimSpace(name), target, true
}

func safeURL(raw string) bool {
	parsed, err := url.Parse(raw)
	return err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host != ""
}
