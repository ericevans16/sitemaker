package build

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"html/template"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ericevans16/sitemaker/internal/config"
	"github.com/ericevans16/sitemaker/internal/theme"
	"github.com/ericevans16/sitemaker/pkg/model"
)

// MenuEntry is passed to templates in .Menu so the header can render the
// configured navigation bar without hard-coding page names.
type MenuEntry struct {
	Name   string
	URL    string
	Active bool
}

func (r *runner) pageData(currentURL string, extra map[string]any) map[string]any {
	menu, navMap := r.navFor(currentURL)
	data := map[string]any{
		"Site":        r.cfg.Site,
		"SiteGraph":   r.site,
		"Home":        r.cfg.Home,
		"Params":      r.cfg.Params,
		"SiteName":    r.site.Title,
		"BundleCSS":   r.eng.BundleCSS,
		"BundleJS":    r.eng.BundleJS,
		"Year":        time.Now().Year(),
		"Menu":        menu,
		"Nav":         navMap,
		"Features":    r.cfg.Features,
		"Permalinks":  r.cfg.Permalinks,
		"CurrentURL":  currentURL,
		"CanonicalURL": r.canonical(currentURL),
	}
	for k, v := range extra {
		data[k] = v
	}
	return data
}

func (r *runner) canonical(pageURL string) string {
	if r.site.URL == "" {
		return pageURL
	}
	if pageURL == "" || pageURL[0] != '/' {
		pageURL = "/" + pageURL
	}
	return r.site.URL + pageURL
}

func (r *runner) navFor(currentURL string) ([]MenuEntry, map[string]string) {
	menu := make([]MenuEntry, len(r.cfg.Menu))
	navMap := map[string]string{"Home": "", "Articles": "", "Friends": "", "About": "", "Archives": "", "Tags": "", "Search": ""}
	for i, item := range r.cfg.Menu {
		active := isMenuActive(item.URL, currentURL)
		menu[i] = MenuEntry{Name: item.Name, URL: item.URL, Active: active}
		if active {
			navMap[item.Name] = "active"
		} else {
			navMap[item.Name] = ""
		}
	}
	// Keep legacy .Nav["Articles"] lit on post pages even when the menu
	// uses a custom label.
	if strings.HasPrefix(currentURL, "/articles/") {
		navMap["Articles"] = "active"
	}
	return menu, navMap
}

func isMenuActive(itemURL, currentURL string) bool {
	if itemURL == "/" || itemURL == "/index.html" {
		return currentURL == "/" || currentURL == "/index.html"
	}
	prefix := strings.TrimSuffix(itemURL, ".html")
	prefix = strings.TrimSuffix(prefix, "/")
	if prefix == "" {
		return false
	}
	return currentURL == itemURL || strings.HasPrefix(currentURL, prefix+"/") || strings.HasPrefix(currentURL, prefix+".")
}

// writePage renders layout, writes the HTML file (and optional SPA fragment)
// and records the URL for sitemap.xml.
func (r *runner) writePage(layout, pageURL string, data map[string]any, lastmod, priority string) error {
	full, frag, err := r.eng.Render(layout, data)
	if err != nil {
		return err
	}
	if r.cfg.Features.Minify {
		full = theme.MinifyHTML(full)
		frag = theme.MinifyHTML(frag)
	}
	if r.opts.InjectHead != "" {
		full = bytes.Replace(full, []byte("</head>"), []byte(r.opts.InjectHead+"</head>"), 1)
	}

	outPath := urlToFilePath(r.opts.OutDir, pageURL)
	if err := os.MkdirAll(filepath.Dir(outPath), 0755); err != nil {
		return err
	}
	if err := os.WriteFile(outPath, full, 0644); err != nil {
		return err
	}
	r.filesWritten++

	if r.cfg.Features.Fragments {
		title := fmt.Sprint(data["Title"])
		fragPath := strings.TrimSuffix(outPath, ".html") + ".frag.html"
		fragDoc := "<title>" + template.HTMLEscapeString(title) + " | " + template.HTMLEscapeString(r.site.Title) +
			"</title><meta name=\"robots\" content=\"noindex\"><main>" + string(frag) + "</main>"
		if err := os.WriteFile(fragPath, []byte(fragDoc), 0644); err != nil {
			return err
		}
		r.filesWritten++
	}

	if priority != "" {
		r.record[pageURL] = sitemapEntry{loc: pageURL, lastmod: lastmod, priority: priority}
	}
	return nil
}

func (r *runner) writeRaw(relPath string, data []byte) error {
	dst := filepath.Join(r.opts.OutDir, filepath.FromSlash(relPath))
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	r.filesWritten++
	return os.WriteFile(dst, data, 0644)
}

func urlToFilePath(outDir, pageURL string) string {
	clean := strings.TrimPrefix(pageURL, "/")
	if clean == "" || strings.HasSuffix(clean, "/") {
		return filepath.Join(outDir, filepath.FromSlash(clean), "index.html")
	}
	if !strings.HasSuffix(clean, ".html") {
		return filepath.Join(outDir, filepath.FromSlash(clean), "index.html")
	}
	return filepath.Join(outDir, filepath.FromSlash(clean))
}

func prepareOutputDir(root string) error {
	gitDir := filepath.Join(root, ".git")
	stashDir := filepath.Join(filepath.Dir(root), "."+filepath.Base(root)+".git.keep")
	_ = os.RemoveAll(stashDir)

	hasGit := false
	if info, err := os.Stat(gitDir); err == nil && info.IsDir() {
		hasGit = true
		if err := os.Rename(gitDir, stashDir); err != nil {
			return err
		}
	}
	restore := func() error {
		if !hasGit {
			return nil
		}
		if err := os.MkdirAll(root, 0755); err != nil {
			return err
		}
		hasGit = false
		return os.Rename(stashDir, gitDir)
	}
	if err := os.RemoveAll(root); err != nil {
		_ = restore()
		return err
	}
	if err := os.MkdirAll(root, 0755); err != nil {
		_ = restore()
		return err
	}
	return restore()
}

// Heatmap cells for the 53-week activity chart on the home page.
type heatmapCell struct {
	Level int
	Date  string
	Count int
}

type heatmapMonth struct {
	Label string
	Col   int
}

func computeHeatmap(posts []*model.Page) (cells []heatmapCell, dayLabels []string, months []heatmapMonth, total int) {
	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	start := today.AddDate(0, 0, -int(today.Weekday())-364)

	counts := map[string]int{}
	for _, p := range posts {
		counts[p.DateString()]++
		total++
	}

	cells = make([]heatmapCell, 53*7)
	for w := 0; w < 53; w++ {
		for d := 0; d < 7; d++ {
			date := start.AddDate(0, 0, w*7+d).Format("2006-01-02")
			c := counts[date]
			lvl := min(c, 4)
			cells[w*7+d] = heatmapCell{Level: lvl, Date: date, Count: c}
		}
	}

	dayLabels = []string{"", "Mon", "", "Wed", "", "Fri", ""}
	lastLabel, lastCol := "", -100
	for w := 0; w < 53; w++ {
		m := start.AddDate(0, 0, w*7).Format("Jan")
		if m == lastLabel {
			continue
		}
		lastLabel = m
		if w == 0 || w-lastCol < 3 {
			continue
		}
		months = append(months, heatmapMonth{Label: m, Col: w})
		lastCol = w
	}
	return
}

func formatWords(n int) string {
	if n >= 1_000_000 {
		return strconv.Itoa((n+500_000)/1_000_000) + "M"
	}
	return strconv.Itoa((n+500)/1000) + "K"
}

// --- Sitemap, RSS, Search index, robots.txt, llm.txt ---

func renderSitemap(baseURL string, entries map[string]sitemapEntry) []byte {
	keys := make([]string, 0, len(entries))
	for k := range entries {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var buf bytes.Buffer
	buf.WriteString(`<?xml version="1.0" encoding="UTF-8"?>`)
	buf.WriteString(`<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">`)
	for _, k := range keys {
		e := entries[k]
		buf.WriteString("<url><loc>" + escapeXML(baseURL+e.loc) + "</loc>")
		if e.lastmod != "" {
			buf.WriteString("<lastmod>" + escapeXML(e.lastmod) + "</lastmod>")
		}
		if e.priority != "" {
			buf.WriteString("<priority>" + escapeXML(e.priority) + "</priority>")
		}
		buf.WriteString("</url>")
	}
	buf.WriteString("</urlset>")
	return buf.Bytes()
}

func renderRSS(site *model.Site, cfg *config.Config) []byte {
	base := site.URL
	var buf bytes.Buffer
	buf.WriteString(`<?xml version="1.0" encoding="UTF-8"?>`)
	buf.WriteString(`<rss version="2.0" xmlns:atom="http://www.w3.org/2005/Atom"><channel>`)
	buf.WriteString("<title>" + escapeXML(site.Title) + "</title>")
	buf.WriteString("<link>" + escapeXML(base) + "</link>")
	buf.WriteString("<description>" + escapeXML(site.Description) + "</description>")
	buf.WriteString("<lastBuildDate>" + escapeXML(site.BuildTime.Format(time.RFC1123Z)) + "</lastBuildDate>")
	buf.WriteString(`<atom:link href="` + escapeXML(base+"/feed.xml") + `" rel="self" type="application/rss+xml"/>`)

	posts := site.Posts
	if cfg.Feed.Limit > 0 && len(posts) > cfg.Feed.Limit {
		posts = posts[:cfg.Feed.Limit]
	}
	for _, p := range posts {
		link := site.Abs(p.URL)
		buf.WriteString("<item>")
		buf.WriteString("<title>" + escapeXML(p.Title) + "</title>")
		buf.WriteString("<link>" + escapeXML(link) + "</link>")
		buf.WriteString(`<guid isPermaLink="true">` + escapeXML(link) + `</guid>`)
		buf.WriteString("<pubDate>" + escapeXML(p.Date.Format(time.RFC1123Z)) + "</pubDate>")
		if p.Category != "" {
			buf.WriteString("<category>" + escapeXML(p.Category) + "</category>")
		}
		body := p.Excerpt
		if cfg.Feed.FullContent {
			body = string(p.Content)
		}
		if body != "" {
			buf.WriteString("<description><![CDATA[" + strings.ReplaceAll(body, "]]>", "]]]]><![CDATA[>") + "]]></description>")
		}
		buf.WriteString("</item>")
	}
	buf.WriteString("</channel></rss>")
	return buf.Bytes()
}

type searchEntry struct {
	Title    string   `json:"title"`
	URL      string   `json:"url"`
	Date     string   `json:"date,omitempty"`
	Category string   `json:"category,omitempty"`
	Tags     []string `json:"tags,omitempty"`
	Excerpt  string   `json:"excerpt,omitempty"`
	Content  string   `json:"content,omitempty"`
}

func buildSearchIndex(site *model.Site) []byte {
	items := make([]searchEntry, 0, len(site.Posts))
	for _, p := range site.Posts {
		text := p.PlainText
		if runes := []rune(text); len(runes) > 2000 {
			text = string(runes[:2000])
		}
		items = append(items, searchEntry{
			Title:    p.Title,
			URL:      p.URL,
			Date:     p.DateString(),
			Category: p.Category,
			Tags:     p.Tags,
			Excerpt:  p.Excerpt,
			Content:  text,
		})
	}
	out, _ := json.Marshal(items)
	return out
}

func renderRobots(baseURL string) []byte {
	return []byte("User-agent: *\nAllow: /\nSitemap: " + baseURL + "/sitemap.xml\n")
}

func renderLLMTXT(site *model.Site, cfg *config.Config) []byte {
	var b strings.Builder
	b.WriteString("# " + site.Title + "\n\n")
	if site.Description != "" {
		b.WriteString("> " + site.Description + "\n\n")
	}
	b.WriteString("## Pages\n\n")
	for _, m := range cfg.Menu {
		b.WriteString("- [" + m.Name + "](" + site.Abs(m.URL) + ")\n")
	}
	if len(site.Posts) > 0 {
		b.WriteString("\n## Posts\n\n")
		for _, p := range site.Posts {
			b.WriteString("- [" + p.Title + "](" + site.Abs(p.URL) + ") - " + p.DateString() + "\n")
		}
	}
	return []byte(b.String())
}

func escapeXML(v string) string {
	var buf bytes.Buffer
	if err := xml.EscapeText(&buf, []byte(v)); err != nil {
		return v
	}
	return buf.String()
}
