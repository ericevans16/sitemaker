// Package content loads Markdown documents from content/ and builds the
// in-memory Site graph (taxonomies, prev/next links, related posts).
package content

import (
	"fmt"
	stdhtml "html"
	"html/template"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/ericevans16/sitemaker/internal/config"
	"github.com/ericevans16/sitemaker/internal/markdown"
	"github.com/ericevans16/sitemaker/pkg/model"

	"gopkg.in/yaml.v3"
)

type frontmatter struct {
	Title       string   `yaml:"title"`
	Description string   `yaml:"description"`
	Summary     string   `yaml:"summary"`
	Author      string   `yaml:"author"`
	Date        string   `yaml:"date"`
	UpdatedAt   string   `yaml:"updated_at"`
	Location    string   `yaml:"location"`
	Avatar      string   `yaml:"avatar"`
	Cover       string   `yaml:"cover"`
	Category    string   `yaml:"category"`
	Tags        []string `yaml:"tags"`
	Slug        string   `yaml:"slug"`
	URL         string   `yaml:"url"`
	Layout      string   `yaml:"layout"`
	Lang        string   `yaml:"lang"`
	Draft       bool     `yaml:"draft"`
	TOC         *bool    `yaml:"toc"`
	Weight      int      `yaml:"weight"`
}

// Options configures content loading.
type Options struct {
	ContentDir    string // defaults to "content"
	IncludeDrafts bool
}

// Load walks ContentDir, parses every .md file and returns the assembled Site.
func Load(cfg *config.Config, md *markdown.Renderer, opts Options) (*model.Site, error) {
	root := opts.ContentDir
	if root == "" {
		root = "content"
	}

	site := &model.Site{
		Title:       cfg.Site.Title,
		URL:         cfg.Site.URL,
		Description: cfg.Site.Description,
		Language:    cfg.Site.Language,
		Author:      cfg.Site.Author,
		Tags:        model.NewTaxonomy("tags", "Tags", cfg.Permalinks.Tags),
		Categories:  model.NewTaxonomy("categories", "Categories", cfg.Permalinks.Categories),
		Params:      cfg.Params,
		Data:        map[string]any{},
		BuildTime:   time.Now(),
		Drafts:      opts.IncludeDrafts,
	}
	// Pre-seed configured categories so their display order and descriptions
	// are preserved even when a category currently has zero posts.
	for _, cat := range cfg.Categories {
		slug := cat.Slug
		if slug == "" {
			slug = markdown.Slugify(cat.Name)
		}
		term := site.Categories.Ensure(cat.Name, slug)
		term.Description = cat.Description
	}

	if _, err := os.Stat(root); os.IsNotExist(err) {
		return site, nil
	}

	seenSlugs := map[string]string{}
	seenURLs := map[string]string{}

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if strings.HasPrefix(info.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(info.Name(), ".md") {
			return nil
		}

		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		relSlash := filepath.ToSlash(rel)
		section, _ := strings.CutPrefix(relSlash, "/")
		if i := strings.IndexByte(section, '/'); i >= 0 {
			section = section[:i]
		} else {
			section = ""
		}

		page, err := parseFile(filepath.ToSlash(path), section, cfg, md)
		if err != nil {
			return err
		}
		if page.Draft && !opts.IncludeDrafts {
			return nil
		}

		if page.Kind == model.KindPost {
			if prev, dup := seenSlugs[page.Slug]; dup {
				return fmt.Errorf("duplicate post slug %q in %s and %s", page.Slug, prev, page.SourcePath)
			}
			seenSlugs[page.Slug] = page.SourcePath
			site.Posts = append(site.Posts, page)
		} else {
			site.Pages = append(site.Pages, page)
		}
		if prev, dup := seenURLs[page.URL]; dup {
			return fmt.Errorf("duplicate page URL %q in %s and %s", page.URL, prev, page.SourcePath)
		}
		seenURLs[page.URL] = page.SourcePath
		return nil
	})
	if err != nil {
		return nil, err
	}

	// Posts: weight (desc) -> date (desc) -> slug (asc) for determinism.
	sort.SliceStable(site.Posts, func(i, j int) bool {
		a, b := site.Posts[i], site.Posts[j]
		if a.Weight != b.Weight {
			return a.Weight > b.Weight
		}
		if !a.Date.Equal(b.Date) {
			return a.Date.After(b.Date)
		}
		return a.Slug < b.Slug
	})
	sort.SliceStable(site.Pages, func(i, j int) bool {
		return site.Pages[i].URL < site.Pages[j].URL
	})

	wireGraph(site, cfg)
	return site, nil
}

func parseFile(path, section string, cfg *config.Config, md *markdown.Renderer) (*model.Page, error) {
	rawBytes, err := os.ReadFile(filepath.FromSlash(path))
	if err != nil {
		return nil, err
	}
	text := strings.ReplaceAll(string(rawBytes), "\r\n", "\n")
	rawFM, body := splitFrontmatter(text)

	fm := &frontmatter{}
	params := map[string]any{}
	if rawFM != "" {
		if err := yaml.Unmarshal([]byte(rawFM), fm); err != nil {
			return nil, fmt.Errorf("parse %s: %w", path, err)
		}
		_ = yaml.Unmarshal([]byte(rawFM), &params)
	}

	isPost := section == "posts"
	baseName := strings.TrimSuffix(filepath.Base(path), ".md")

	slug := strings.TrimSpace(fm.Slug)
	if slug == "" {
		slug = baseName
	}

	var pubDate, updDate time.Time
	if isPost {
		if strings.TrimSpace(fm.Title) == "" {
			return nil, fmt.Errorf("parse %s: missing required frontmatter field \"title\"", path)
		}
		if strings.TrimSpace(fm.Date) == "" {
			return nil, fmt.Errorf("parse %s: missing required frontmatter field \"date\"", path)
		}
		pubDate, err = parseDate(fm.Date)
		if err != nil {
			return nil, fmt.Errorf("parse %s: invalid date %q: %w", path, fm.Date, err)
		}
	} else if fm.Date != "" {
		if pubDate, err = parseDate(fm.Date); err != nil {
			return nil, fmt.Errorf("parse %s: invalid date %q: %w", path, fm.Date, err)
		}
	}
	if fm.UpdatedAt != "" {
		updDate, err = parseDate(fm.UpdatedAt)
		if err != nil {
			return nil, fmt.Errorf("parse %s: invalid updated_at %q: %w", path, fm.UpdatedAt, err)
		}
	}

	htmlOut, err := md.Convert(path, body)
	if err != nil {
		return nil, fmt.Errorf("render %s: %w", path, err)
	}

	plain := strings.TrimSpace(stripTags(htmlOut))
	wc := countWords(body)
	excerpt := strings.TrimSpace(fm.Description)
	if excerpt == "" {
		excerpt = strings.TrimSpace(fm.Summary)
	}
	if excerpt == "" {
		excerpt = extractExcerpt(body, htmlOut, md)
	}

	category := strings.TrimSpace(fm.Category)
	if isPost && category == "" {
		category = cfg.DefaultCategory
	}

	toc := isPost
	if fm.TOC != nil {
		toc = *fm.TOC
	}

	kind := model.KindPage
	layout := strings.TrimSpace(fm.Layout)
	var pageURL string
	if isPost {
		kind = model.KindPost
		if layout == "" {
			layout = "post"
		}
		pageURL = strings.TrimSpace(fm.URL)
		if pageURL == "" {
			pageURL = expandPostPermalink(cfg.Permalinks.Post, slug, category, pubDate)
		}
	} else {
		pageURL, layout = resolveStandalonePage(baseName, section, fm, layout)
	}

	title := strings.TrimSpace(fm.Title)
	if title == "" {
		title = humanize(baseName)
	}
	author := strings.TrimSpace(fm.Author)
	if author == "" {
		author = cfg.Site.Author
	}

	return &model.Page{
		Kind:           kind,
		SourcePath:     path,
		Section:        section,
		Slug:           slug,
		URL:            pageURL,
		Permalink:      cfg.Site.URL + pageURL,
		Title:          title,
		Description:    excerpt,
		Author:         author,
		Location:       strings.TrimSpace(fm.Location),
		Avatar:         strings.TrimSpace(fm.Avatar),
		Cover:          strings.TrimSpace(fm.Cover),
		Category:       category,
		Tags:           cleanTags(fm.Tags),
		Lang:           strings.TrimSpace(fm.Lang),
		Layout:         layout,
		Draft:          fm.Draft,
		TOC:            toc,
		Weight:         fm.Weight,
		Date:           pubDate,
		Updated:        updDate,
		Params:         params,
		Body:           body,
		Content:        template.HTML(htmlOut),
		Excerpt:        excerpt,
		PlainText:      plain,
		WordCount:      wc,
		ReadingMinutes: readingMinutes(wc, cfg.Reading.WordsPerMinute),
	}, nil
}

// resolveStandalonePage maps a non-post file under content/ to its output URL
// and default layout. Existing RainHush paths keep working out of the box:
//
//	content/about/about_CN.md  -> /about.html     (lang zh-CN)
//	content/about/about_EN.md  -> /about_EN.html  (lang en)
//	content/friends/friends.md -> /friends.html   (layout friends)
//	content/logs/logs.md       -> /logs.html
//	content/foo.md             -> /foo.html
//	content/notes/bar.md       -> /notes/bar.html
func resolveStandalonePage(baseName, section string, fm *frontmatter, layout string) (string, string) {
	if section == "about" && baseName == "about_CN" {
		if fm.Lang == "" {
			fm.Lang = "zh-CN"
		}
		baseName = "about"
	} else if section == "about" && baseName == "about_EN" && fm.Lang == "" {
		fm.Lang = "en"
	}

	if layout == "" {
		if section == "friends" || baseName == "friends" {
			layout = "friends"
		} else {
			layout = "page"
		}
	}

	if u := strings.TrimSpace(fm.URL); u != "" {
		if !strings.HasPrefix(u, "/") {
			u = "/" + u
		}
		return u, layout
	}
	if section == "" || section == baseName || section == "about" {
		return "/" + baseName + ".html", layout
	}
	return "/" + section + "/" + baseName + ".html", layout
}

func expandPostPermalink(pattern, slug, category string, date time.Time) string {
	catSlug := markdown.Slugify(category)
	if catSlug == "" {
		catSlug = "uncategorized"
	}
	r := strings.NewReplacer(
		":slug", slug,
		":year", date.Format("2006"),
		":month", date.Format("01"),
		":day", date.Format("02"),
		":category", catSlug,
	)
	u := r.Replace(pattern)
	if !strings.HasPrefix(u, "/") {
		u = "/" + u
	}
	return u
}

// wireGraph populates taxonomies, prev/next links and related posts.
func wireGraph(site *model.Site, cfg *config.Config) {
	// Bilingual about pages link to each other via LangAlt.
	byURL := map[string]*model.Page{}
	for _, p := range site.Pages {
		byURL[p.URL] = p
	}
	if cn, en := byURL["/about.html"], byURL["/about_EN.html"]; cn != nil && en != nil {
		cn.Params["lang_alt"] = site.Abs(en.URL)
		en.Params["lang_alt"] = site.Abs(cn.URL)
	}

	for i, post := range site.Posts {
		// Posts are ordered newest-first: Next is newer, Prev is older.
		if i > 0 {
			post.Next = site.Posts[i-1]
		}
		if i+1 < len(site.Posts) {
			post.Prev = site.Posts[i+1]
		}

		if post.Category != "" {
			slug := categorySlug(cfg, post.Category)
			term := site.Categories.Ensure(post.Category, slug)
			term.Pages = append(term.Pages, post)
			post.CategoryTerm = term
		}
		for _, tag := range post.Tags {
			slug := markdown.Slugify(tag)
			if slug == "" {
				continue
			}
			term := site.Tags.Ensure(tag, slug)
			term.Pages = append(term.Pages, post)
			post.TagTerms = append(post.TagTerms, term)
		}
	}

	if cfg.Features.Related {
		for _, post := range site.Posts {
			post.Related = findRelated(post, site.Posts, 3)
		}
	}
}

func categorySlug(cfg *config.Config, name string) string {
	for _, c := range cfg.Categories {
		if strings.EqualFold(c.Name, name) && c.Slug != "" {
			return c.Slug
		}
	}
	return markdown.Slugify(name)
}

func findRelated(target *model.Page, all []*model.Page, limit int) []*model.Page {
	type scored struct {
		page  *model.Page
		score int
	}
	tagSet := map[string]bool{}
	for _, t := range target.Tags {
		tagSet[strings.ToLower(t)] = true
	}
	var cand []scored
	for _, p := range all {
		if p == target {
			continue
		}
		score := 0
		for _, t := range p.Tags {
			if tagSet[strings.ToLower(t)] {
				score += 3
			}
		}
		if target.Category != "" && strings.EqualFold(target.Category, p.Category) {
			score += 1
		}
		if score > 0 {
			cand = append(cand, scored{p, score})
		}
	}
	sort.SliceStable(cand, func(i, j int) bool {
		if cand[i].score != cand[j].score {
			return cand[i].score > cand[j].score
		}
		return cand[i].page.Date.After(cand[j].page.Date)
	})
	if len(cand) > limit {
		cand = cand[:limit]
	}
	out := make([]*model.Page, len(cand))
	for i, c := range cand {
		out[i] = c.page
	}
	return out
}

func parseDate(raw string) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	for _, layout := range []string{"2006-01-02", time.RFC3339, "2006-01-02 15:04", "2006-01-02 15:04:05"} {
		if t, err := time.Parse(layout, raw); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("expected YYYY-MM-DD")
}

func splitFrontmatter(content string) (string, string) {
	content = strings.TrimPrefix(content, "\uFEFF")
	if !strings.HasPrefix(content, "---\n") {
		return "", content
	}
	rest := content[4:]
	if idx := strings.Index(rest, "\n---\n"); idx >= 0 {
		return rest[:idx], rest[idx+5:]
	}
	if strings.HasSuffix(rest, "\n---") {
		return rest[:len(rest)-4], ""
	}
	return "", content
}

func cleanTags(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, t := range in {
		t = strings.TrimSpace(t)
		key := strings.ToLower(t)
		if t == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, t)
	}
	return out
}

func countWords(body string) int {
	n := 0
	for _, r := range body {
		if !unicode.IsSpace(r) {
			n++
		}
	}
	return n
}

func readingMinutes(words, speed int) int {
	if speed < 1 {
		speed = 300
	}
	return max((words+speed-1)/speed, 1)
}

var excerptMoreRe = regexp.MustCompile(`<!--\s*more\s*-->`)

func extractExcerpt(body, renderedHTML string, md *markdown.Renderer) string {
	if loc := excerptMoreRe.FindStringIndex(body); loc != nil {
		if html, err := md.Convert("", strings.TrimSpace(body[:loc[0]])); err == nil {
			return truncateText(strings.TrimSpace(stripTags(html)), 200)
		}
	}
	if idx := strings.Index(renderedHTML, "</p>"); idx != -1 && idx < 400 {
		if pStart := strings.LastIndex(renderedHTML[:idx], "<p>"); pStart != -1 {
			text := strings.TrimSpace(stripTags(renderedHTML[pStart : idx+4]))
			return truncateText(text, 200)
		}
	}
	return ""
}

// stripTags reduces rendered HTML to plain text and decodes HTML entities.
func stripTags(s string) string {
	var b strings.Builder
	inTag := false
	for _, r := range s {
		switch {
		case r == '<':
			inTag = true
		case r == '>':
			inTag = false
		case !inTag:
			b.WriteRune(r)
		}
	}
	return stdhtml.UnescapeString(b.String())
}

func truncateText(text string, maxLen int) string {
	runes := []rune(text)
	if len(runes) <= maxLen {
		return text
	}
	return string(runes[:maxLen]) + "…"
}

func humanize(slug string) string {
	s := strings.ReplaceAll(strings.ReplaceAll(slug, "-", " "), "_", " ")
	if s == "" {
		return "Page"
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
