package friends

import (
	"strings"
	"testing"

	"github.com/ericevans16/sitemaker/pkg/plugin"
)

func TestRenderFenceBuildsFriendCards(t *testing.T) {
	out, handled, err := (&friendsPlugin{}).RenderFence("better-friends", "- Alice: [https://alice.example/](https://alice.example/)\n- Bob: [http://bob.example/](http://bob.example/)", plugin.FenceContext{})
	if err != nil || !handled {
		t.Fatalf("RenderFence = (%q, %v, %v)", out, handled, err)
	}
	for _, want := range []string{`class="better-friends-grid"`, `href="https://alice.example/"`, `>Alice</a>`, `href="http://bob.example/"`} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected output to contain %q, got %q", want, out)
		}
	}
}

func TestRenderFenceRejectsInvalidLinks(t *testing.T) {
	out, handled, err := (&friendsPlugin{}).RenderFence("friends", "- Unsafe: [javascript:alert(1)](javascript:alert(1))\n- Missing: [https://valid.example/]", plugin.FenceContext{})
	if err != nil || !handled || !strings.Contains(out, "better-friends-empty") || strings.Contains(out, "javascript:") {
		t.Fatalf("expected safe empty state, got (%q, %v, %v)", out, handled, err)
	}
}

func TestRenderFenceIgnoresOtherLanguages(t *testing.T) {
	if out, handled, err := (&friendsPlugin{}).RenderFence("go", "", plugin.FenceContext{}); err != nil || handled || out != "" {
		t.Fatalf("RenderFence = (%q, %v, %v), want (empty, false, nil)", out, handled, err)
	}
}
