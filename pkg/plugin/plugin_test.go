package plugin

import (
	"strings"
	"testing"

	"github.com/ericevans16/sitemaker/pkg/model"
)

type fakePlugin struct {
	name string
	css  string
	seen map[string]any
}

func (f *fakePlugin) Name() string                      { return f.name }
func (f *fakePlugin) CSS() [][]byte                     { return [][]byte{[]byte(f.css)} }
func (f *fakePlugin) JS() [][]byte                      { return nil }
func (f *fakePlugin) Configure(opts map[string]any) error { f.seen = opts; return nil }
func (f *fakePlugin) RenderFence(lang, code string, _ FenceContext) (string, bool, error) {
	if lang != f.name {
		return "", false, nil
	}
	return "<" + f.name + ">" + code + "</" + f.name + ">", true, nil
}
func (f *fakePlugin) TransformPage(p *model.Page) error {
	p.Title += " [" + f.name + "]"
	return nil
}

func TestRegistryPriorityAndToggle(t *testing.T) {
	r := NewRegistry()
	late := &fakePlugin{name: "late", css: "b"}
	early := &fakePlugin{name: "early", css: "a"}
	r.Register(late, 200)
	r.Register(early, 100)

	if got := strings.Join(r.Names(), ","); got != "early,late" {
		t.Fatalf("Names() = %q, want early,late", got)
	}

	r.SetEnabled("early", false)
	page := &model.Page{Title: "Hello"}
	if err := Each(r, func(pt PageTransformer) error { return pt.TransformPage(page) }); err != nil {
		t.Fatal(err)
	}
	if page.Title != "Hello [late]" {
		t.Fatalf("TransformPage produced %q, want 'Hello [late]'", page.Title)
	}

	if out, ok, err := r.RenderFence("early", "x", FenceContext{}); err != nil || ok || out != "" {
		t.Fatalf("disabled plugin should not handle fence, got (%q, %v, %v)", out, ok, err)
	}
	if out, ok, err := r.RenderFence("late", "x", FenceContext{}); err != nil || !ok || out != "<late>x</late>" {
		t.Fatalf("enabled plugin failed: (%q, %v, %v)", out, ok, err)
	}
}
