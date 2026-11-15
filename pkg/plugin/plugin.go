// Package plugin is the public extension API of sitemaker.
//
// A plugin is any value implementing [Plugin]. It opts into build stages by
// additionally implementing one or more hook interfaces:
//
//	Configurable      receive its options block from _config.yaml
//	FenceRenderer     take over ```lang fenced code blocks
//	MarkdownExtender  add goldmark extensions to the Markdown pipeline
//	AssetProvider     contribute CSS / JS to the site bundle
//	TemplateFuncs     expose functions to theme templates
//	PageTransformer   mutate each page after it has been rendered
//	SiteHook          inspect / mutate the site graph before rendering
//	Generator         add virtual pages or raw output files
//	PostBuildHook     run after every file has been written
//
// Plugins register themselves from an init function:
//
//	func init() { plugin.Register(&myPlugin{}, 100) }
//
// and are toggled per site in _config.yaml:
//
//	plugins:
//	  my-plugin: true          # enabled with defaults
//	  other: false             # disabled
//	  image-layout:            # enabled with options
//	    verbose: true
package plugin

import (
	"fmt"
	"html/template"
	"sort"
	"strings"
	"sync"

	"github.com/ericevans16/sitemaker/pkg/model"

	"github.com/yuin/goldmark"
)

// Plugin is the minimal contract: a unique, stable name.
type Plugin interface {
	Name() string
}

// Configurable plugins receive their options map (possibly empty) before
// the build starts. Returning an error aborts the build.
type Configurable interface {
	Configure(options map[string]any) error
}

// FenceContext describes the fenced block being rendered.
type FenceContext struct {
	Document string // source file, e.g. content/posts/foo.md
	Info     string // the full info string after the backticks
}

// FenceRenderer renders fenced code blocks. Return handled=false to pass
// the block on to the next renderer (and ultimately the code highlighter).
type FenceRenderer interface {
	RenderFence(lang, code string, ctx FenceContext) (html string, handled bool, err error)
}

// MarkdownExtender adds goldmark extensions (new syntax, renderers...).
type MarkdownExtender interface {
	MarkdownExtensions() []goldmark.Extender
}

// AssetProvider contributes stylesheets and scripts to the bundles.
type AssetProvider interface {
	CSS() [][]byte
	JS() [][]byte
}

// TemplateFuncs exposes extra functions to theme templates.
type TemplateFuncs interface {
	TemplateFuncs() template.FuncMap
}

// PageTransformer runs once per page after Markdown rendering and before
// the site graph (taxonomies, related posts) is assembled.
type PageTransformer interface {
	TransformPage(page *model.Page) error
}

// SiteHook runs after the site graph is complete, right before rendering.
type SiteHook interface {
	BeforeRender(site *model.Site) error
}

// Generator produces extra pages rendered through the theme and/or raw
// files copied verbatim into the output directory.
type Generator interface {
	Generate(site *model.Site) ([]*model.VirtualPage, []*model.OutputFile, error)
}

// PostBuildHook runs after the whole site has been written to outDir.
type PostBuildHook interface {
	AfterBuild(site *model.Site, outDir string) error
}

type entry struct {
	plugin   Plugin
	priority int
	enabled  bool
}

// Registry is an ordered, toggleable set of plugins. Lower priority
// values run first; built-ins use 100, third-party plugins default to 200.
type Registry struct {
	mu      sync.RWMutex
	entries []*entry
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry { return &Registry{} }

// Default is the process-wide registry plugins register into from init().
var Default = NewRegistry()

// Register adds p to the default registry.
func Register(p Plugin, priority int) { Default.Register(p, priority) }

// Register adds a plugin. Duplicate names panic: that is a programming
// error that must surface at startup, not silently shadow a plugin.
func (r *Registry) Register(p Plugin, priority int) {
	if p == nil || strings.TrimSpace(p.Name()) == "" {
		panic("sitemaker: plugin must have a non-empty name")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, e := range r.entries {
		if e.plugin.Name() == p.Name() {
			panic(fmt.Sprintf("sitemaker: plugin %q registered twice", p.Name()))
		}
	}
	r.entries = append(r.entries, &entry{plugin: p, priority: priority, enabled: true})
	sort.SliceStable(r.entries, func(i, j int) bool { return r.entries[i].priority < r.entries[j].priority })
}

// SetEnabled toggles a plugin by name; unknown names are ignored.
func (r *Registry) SetEnabled(name string, enabled bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, e := range r.entries {
		if e.plugin.Name() == name {
			e.enabled = enabled
		}
	}
}

// Has reports whether a plugin with the given name is registered.
func (r *Registry) Has(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, e := range r.entries {
		if e.plugin.Name() == name {
			return true
		}
	}
	return false
}

// Names lists every registered plugin, enabled or not, in run order.
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.entries))
	for _, e := range r.entries {
		out = append(out, e.plugin.Name())
	}
	return out
}

// Enabled returns the enabled plugins in run order.
func (r *Registry) Enabled() []Plugin {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Plugin, 0, len(r.entries))
	for _, e := range r.entries {
		if e.enabled {
			out = append(out, e.plugin)
		}
	}
	return out
}

// Each calls fn for every enabled plugin implementing T, stopping at the
// first error.
func Each[T any](r *Registry, fn func(T) error) error {
	for _, p := range r.Enabled() {
		if hook, ok := p.(T); ok {
			if err := fn(hook); err != nil {
				return fmt.Errorf("plugin %s: %w", p.Name(), err)
			}
		}
	}
	return nil
}

// RenderFence offers a fenced block to every enabled FenceRenderer.
func (r *Registry) RenderFence(lang, code string, ctx FenceContext) (string, bool, error) {
	for _, p := range r.Enabled() {
		fr, ok := p.(FenceRenderer)
		if !ok {
			continue
		}
		out, handled, err := fr.RenderFence(lang, code, ctx)
		if err != nil {
			return "", false, fmt.Errorf("plugin %s: %w", p.Name(), err)
		}
		if handled {
			return out, true, nil
		}
	}
	return "", false, nil
}
