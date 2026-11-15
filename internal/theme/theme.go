// Package theme loads a theme from themes/<name>/ (falling back to the
// built-in embedded theme when absent on disk), merges project-level layout
// overrides from layouts/, bundles CSS/JS assets and renders pages.
package theme

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"html/template"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/ericevans16/sitemaker/internal/icons"
	"github.com/ericevans16/sitemaker/internal/markdown"
	"github.com/ericevans16/sitemaker/pkg/plugin"

	"gopkg.in/yaml.v3"
)

// Manifest is the parsed theme.yaml.
type Manifest struct {
	Name        string   `yaml:"name"`
	Description string   `yaml:"description"`
	CSS         []string `yaml:"css"`
	JS          []string `yaml:"js"`
}

// Engine owns the parsed base templates and compiled asset URLs.
type Engine struct {
	Manifest  Manifest
	BundleCSS string
	BundleJS  string
	themeFS   fs.FS
	base      *template.Template
	layouts   map[string][]byte // layout name (e.g. "post") -> template source
}

// Load discovers templates and manifest in themes/<name>/ (or the embedded
// theme when that directory is absent), allowing any file in the project-root
// layouts/ directory to override the theme copy.
func Load(themeName string, plugins *plugin.Registry) (*Engine, error) {
	themeFS, err := resolveThemeFS(themeName)
	if err != nil {
		return nil, err
	}

	var manifest Manifest
	if data, err := fs.ReadFile(themeFS, "theme.yaml"); err == nil {
		if err := yaml.Unmarshal(data, &manifest); err != nil {
			return nil, fmt.Errorf("parse theme.yaml: %w", err)
		}
	}
	if manifest.Name == "" {
		manifest.Name = themeName
	}

	funcs := defaultFuncMap()
	if plugins != nil {
		_ = plugin.Each(plugins, func(p plugin.TemplateFuncs) error {
			for k, fn := range p.TemplateFuncs() {
				funcs[k] = fn
			}
			return nil
		})
	}

	base, err := parseSharedTemplates(themeFS, funcs)
	if err != nil {
		return nil, err
	}

	layouts, err := collectLayouts(themeFS)
	if err != nil {
		return nil, err
	}

	return &Engine{
		Manifest: manifest,
		themeFS:  themeFS,
		base:     base,
		layouts:  layouts,
	}, nil
}

func resolveThemeFS(themeName string) (fs.FS, error) {
	diskDir := filepath.Join("themes", themeName)
	if info, err := os.Stat(diskDir); err == nil && info.IsDir() {
		return os.DirFS(diskDir), nil
	}
	sub, err := fs.Sub(EmbeddedBlueprint, path.Join("themes", themeName))
	if err == nil {
		if _, statErr := fs.Stat(sub, "theme.yaml"); statErr == nil {
			return sub, nil
		}
	}
	return nil, fmt.Errorf("theme %q not found in themes/%s or embedded themes", themeName, themeName)
}

func parseSharedTemplates(themeFS fs.FS, funcs template.FuncMap) (*template.Template, error) {
	shared := map[string][]byte{}
	var order []string
	add := func(name string, data []byte) {
		if _, exists := shared[name]; !exists {
			order = append(order, name)
		}
		shared[name] = data
	}

	if data, err := fs.ReadFile(themeFS, "layouts/baseof.html"); err == nil {
		add("baseof.html", data)
	}
	if data, err := os.ReadFile(filepath.Join("layouts", "baseof.html")); err == nil {
		add("baseof.html", data)
	}
	if matches, err := fs.Glob(themeFS, "layouts/partials/*.html"); err == nil {
		for _, m := range matches {
			if data, err := fs.ReadFile(themeFS, m); err == nil {
				add("partials/"+path.Base(m), data)
			}
		}
	}
	if matches, err := filepath.Glob(filepath.Join("layouts", "partials", "*.html")); err == nil {
		for _, m := range matches {
			if data, err := os.ReadFile(m); err == nil {
				add("partials/"+filepath.Base(m), data)
			}
		}
	}

	base := template.New("base").Funcs(funcs)
	for _, name := range order {
		if _, err := base.New(name).Parse(string(shared[name])); err != nil {
			return nil, fmt.Errorf("parse %s: %w", name, err)
		}
	}
	return base, nil
}

func collectLayouts(themeFS fs.FS) (map[string][]byte, error) {
	out := map[string][]byte{}
	if matches, err := fs.Glob(themeFS, "layouts/*.html"); err == nil {
		for _, m := range matches {
			name := strings.TrimSuffix(path.Base(m), ".html")
			if name == "baseof" {
				continue
			}
			data, err := fs.ReadFile(themeFS, m)
			if err != nil {
				return nil, err
			}
			out[name] = data
		}
	}
	if matches, err := filepath.Glob(filepath.Join("layouts", "*.html")); err == nil {
		for _, m := range matches {
			name := strings.TrimSuffix(filepath.Base(m), ".html")
			if name == "baseof" {
				continue
			}
			data, err := os.ReadFile(m)
			if err != nil {
				return nil, err
			}
			out[name] = data
		}
	}
	return out, nil
}

// HasLayout reports whether layout.html exists in the theme or project.
func (e *Engine) HasLayout(name string) bool {
	_, ok := e.layouts[name]
	return ok
}

// Render executes the named layout (wrapping "content" inside "layout") and
// returns both the full HTML document and the inner <main> fragment.
func (e *Engine) Render(layout string, data map[string]any) (full []byte, fragment []byte, err error) {
	src, ok := e.layouts[layout]
	if !ok {
		if fallback, ok2 := e.layouts["page"]; ok2 {
			src = fallback
		} else {
			return nil, nil, fmt.Errorf("layout %q not found in theme %s", layout, e.Manifest.Name)
		}
	}
	tmpl, err := e.base.Clone()
	if err != nil {
		return nil, nil, err
	}
	if _, err := tmpl.New(layout + ".html").Parse(string(src)); err != nil {
		return nil, nil, fmt.Errorf("parse layout %s: %w", layout, err)
	}

	var fullBuf, fragBuf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&fullBuf, "layout", data); err != nil {
		return nil, nil, fmt.Errorf("render %s: %w", layout, err)
	}
	if err := tmpl.ExecuteTemplate(&fragBuf, "content", data); err != nil {
		return nil, nil, fmt.Errorf("render %s fragment: %w", layout, err)
	}
	return fullBuf.Bytes(), fragBuf.Bytes(), nil
}

// BundleAssets concatenates theme CSS/JS + plugin CSS/JS + site custom
// CSS/JS, writes content-hashed bundles under outDir/static/ and copies
// static assets (fonts, images, etc.) from theme and site static/.
func (e *Engine) BundleAssets(outDir string, minify bool, plugins *plugin.Registry) error {
	staticOut := filepath.Join(outDir, "static")
	if err := os.MkdirAll(staticOut, 0755); err != nil {
		return err
	}
	if sub, err := fs.Sub(e.themeFS, "static"); err == nil {
		if err := copyStaticFS(sub, staticOut); err != nil {
			return err
		}
	}
	if _, err := os.Stat("static"); err == nil {
		if err := copyStaticFS(os.DirFS("static"), staticOut); err != nil {
			return err
		}
	}

	var cssBuf bytes.Buffer
	for _, rel := range e.Manifest.CSS {
		data, err := fs.ReadFile(e.themeFS, path.Join("static", rel))
		if err != nil {
			return fmt.Errorf("read theme css %s: %w", rel, err)
		}
		cssBuf.Write(data)
		cssBuf.WriteByte('\n')
	}
	if plugins != nil {
		_ = plugin.Each(plugins, func(p plugin.AssetProvider) error {
			for _, chunk := range p.CSS() {
				cssBuf.Write(chunk)
				cssBuf.WriteByte('\n')
			}
			return nil
		})
	}
	if extra, err := readOptionalGlob("static/css/*.css"); err == nil && len(extra) > 0 {
		cssBuf.Write(extra)
	}

	cssBytes := cssBuf.Bytes()
	if minify {
		cssBytes = MinifyCSS(cssBytes)
	}
	cssHash := shortHash(cssBytes)
	e.BundleCSS = "/static/bundle." + cssHash + ".css"
	if err := os.WriteFile(filepath.Join(outDir, e.BundleCSS[1:]), cssBytes, 0644); err != nil {
		return err
	}

	var jsBuf bytes.Buffer
	for _, rel := range e.Manifest.JS {
		data, err := fs.ReadFile(e.themeFS, path.Join("static", rel))
		if err != nil {
			return fmt.Errorf("read theme js %s: %w", rel, err)
		}
		jsBuf.Write(data)
		jsBuf.WriteString(";\n")
	}
	if plugins != nil {
		_ = plugin.Each(plugins, func(p plugin.AssetProvider) error {
			for _, chunk := range p.JS() {
				jsBuf.Write(chunk)
				jsBuf.WriteString(";\n")
			}
			return nil
		})
	}
	if extra, err := readOptionalGlob("static/js/*.js"); err == nil && len(extra) > 0 {
		jsBuf.Write(extra)
	}

	jsBytes := jsBuf.Bytes()
	jsHash := shortHash(jsBytes)
	e.BundleJS = "/static/bundle." + jsHash + ".js"
	return os.WriteFile(filepath.Join(outDir, e.BundleJS[1:]), jsBytes, 0644)
}

func defaultFuncMap() template.FuncMap {
	return template.FuncMap{
		"dict":    dict,
		"slice":   func(items ...any) []any { return items },
		"icon":    icons.Func(),
		"slugify": markdown.Slugify,
		"lower":   strings.ToLower,
		"upper":   strings.ToUpper,
		"join":    strings.Join,
		"add":     func(a, b int) int { return a + b },
		"sub":     func(a, b int) int { return a - b },
		"dateFmt": func(t time.Time, layout string) string {
			if t.IsZero() {
				return ""
			}
			return t.Format(layout)
		},
		"safeHTML": func(s string) template.HTML { return template.HTML(s) },
	}
}

func dict(pairs ...any) (map[string]any, error) {
	if len(pairs)%2 != 0 {
		return nil, fmt.Errorf("dict: expected key/value pairs, got %d arguments", len(pairs))
	}
	m := make(map[string]any, len(pairs)/2)
	for i := 0; i < len(pairs); i += 2 {
		key, ok := pairs[i].(string)
		if !ok {
			return nil, fmt.Errorf("dict: key must be a string, got %T", pairs[i])
		}
		m[key] = pairs[i+1]
	}
	return m, nil
}

func copyStaticFS(src fs.FS, dst string) error {
	return fs.WalkDir(src, ".", func(rel string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if rel == "css" || rel == "js" {
				return fs.SkipDir
			}
			return os.MkdirAll(filepath.Join(dst, filepath.FromSlash(rel)), 0755)
		}
		data, err := fs.ReadFile(src, rel)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dst, filepath.FromSlash(rel)), data, 0644)
	})
}

func readOptionalGlob(pattern string) ([]byte, error) {
	matches, err := filepath.Glob(pattern)
	if err != nil || len(matches) == 0 {
		return nil, err
	}
	var buf bytes.Buffer
	for _, m := range matches {
		data, err := os.ReadFile(m)
		if err != nil {
			return nil, err
		}
		buf.Write(data)
		buf.WriteByte('\n')
	}
	return buf.Bytes(), nil
}

func shortHash(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])[:8]
}
