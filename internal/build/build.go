// Package build orchestrates a full site build: plugin setup, asset
// bundling, content loading, page rendering and auxiliary generators.
package build

import (
	"fmt"
	"html/template"
	"math"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/ericevans16/sitemaker/internal/config"
	"github.com/ericevans16/sitemaker/internal/content"
	"github.com/ericevans16/sitemaker/internal/markdown"
	"github.com/ericevans16/sitemaker/internal/theme"
	"github.com/ericevans16/sitemaker/pkg/model"
	"github.com/ericevans16/sitemaker/pkg/plugin"
)

// Options controls a build run.
type Options struct {
	OutDir        string // defaults to "public"
	IncludeDrafts bool
	Plugins       *plugin.Registry // defaults to plugin.Default
	// InjectHead is appended before </head> (used by `serve` for live-reload).
	InjectHead string
}

// Stats summarizes a completed build.
type Stats struct {
	Posts      int
	Pages      int
	Tags       int
	Categories int
	Files      int
	Duration   time.Duration
}

// Run executes a full build.
func Run(cfg *config.Config, opts Options) (*Stats, error) {
	start := time.Now()
	if opts.OutDir == "" {
		opts.OutDir = "public"
	}
	if opts.Plugins == nil {
		opts.Plugins = plugin.Default
	}

	if err := configurePlugins(cfg, opts.Plugins); err != nil {
		return nil, err
	}

	if err := prepareOutputDir(opts.OutDir); err != nil {
		return nil, fmt.Errorf("prepare %s: %w", opts.OutDir, err)
	}

	eng, err := theme.Load(cfg.Theme, opts.Plugins)
	if err != nil {
		return nil, err
	}
	if err := eng.BundleAssets(opts.OutDir, cfg.Features.Minify, opts.Plugins); err != nil {
		return nil, fmt.Errorf("bundle assets: %w", err)
	}

	md := markdown.New(markdown.Options{Plugins: opts.Plugins, CodeStyle: "monokai"})
	site, err := content.Load(cfg, md, content.Options{IncludeDrafts: opts.IncludeDrafts})
	if err != nil {
		return nil, err
	}

	if err := plugin.Each(opts.Plugins, func(p plugin.PageTransformer) error {
		for _, page := range site.AllPages() {
			if err := p.TransformPage(page); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return nil, err
	}
	if err := plugin.Each(opts.Plugins, func(p plugin.SiteHook) error {
		return p.BeforeRender(site)
	}); err != nil {
		return nil, err
	}

	r := &runner{
		cfg:    cfg,
		opts:   opts,
		site:   site,
		eng:    eng,
		md:     md,
		record: map[string]sitemapEntry{},
	}
	if err := r.renderAll(); err != nil {
		return nil, err
	}

	if err := plugin.Each(opts.Plugins, func(p plugin.PostBuildHook) error {
		return p.AfterBuild(site, opts.OutDir)
	}); err != nil {
		return nil, err
	}

	return &Stats{
		Posts:      len(site.Posts),
		Pages:      len(site.Pages),
		Tags:       len(site.Tags.Terms),
		Categories: len(site.Categories.Terms),
		Files:      r.filesWritten,
		Duration:   time.Since(start),
	}, nil
}

func configurePlugins(cfg *config.Config, reg *plugin.Registry) error {
	for _, name := range reg.Names() {
		enabled, _ := cfg.PluginSetting(name)
		reg.SetEnabled(name, enabled)
	}
	return plugin.Each(reg, func(p plugin.Configurable) error {
		_, opts := cfg.PluginSetting(p.(plugin.Plugin).Name())
		return p.Configure(opts)
	})
}

type runner struct {
	cfg          *config.Config
	opts         Options
	site         *model.Site
	eng          *theme.Engine
	md           *markdown.Renderer
	record       map[string]sitemapEntry
	filesWritten int
}

type sitemapEntry struct {
	loc      string
	lastmod  string
	priority string
}

func (r *runner) renderAll() error {
	for _, post := range r.site.Posts {
		if err := r.renderPost(post); err != nil {
			return err
		}
	}
	if err := r.renderHome(); err != nil {
		return err
	}
	if err := r.renderArticles(); err != nil {
		return err
	}
	for _, page := range r.site.Pages {
		if err := r.renderStandalone(page); err != nil {
			return err
		}
	}
	if r.cfg.Features.Tags {
		if err := r.renderTaxonomy(r.site.Tags); err != nil {
			return err
		}
	}
	if r.cfg.Features.Categories {
		if err := r.renderTaxonomy(r.site.Categories); err != nil {
			return err
		}
	}
	if r.cfg.Features.Archives {
		if err := r.renderArchives(); err != nil {
			return err
		}
	}
	if r.cfg.Features.Search {
		if err := r.renderSearch(); err != nil {
			return err
		}
	}
	if err := r.render404(); err != nil {
		return err
	}
	if err := r.runPluginGenerators(); err != nil {
		return err
	}
	if r.cfg.Features.RSS {
		if err := r.writeRaw("feed.xml", renderRSS(r.site, r.cfg)); err != nil {
			return err
		}
	}
	if r.cfg.Features.Sitemap && r.site.URL != "" {
		if err := r.writeRaw("sitemap.xml", renderSitemap(r.site.URL, r.record)); err != nil {
			return err
		}
	}
	if r.cfg.Features.LLMs && r.site.URL != "" {
		if err := r.writeRaw("llm.txt", renderLLMTXT(r.site, r.cfg)); err != nil {
			return err
		}
	}
	if r.cfg.Features.Robots && r.site.URL != "" {
		if _, err := os.Stat("robots.txt"); os.IsNotExist(err) {
			if err := r.writeRaw("robots.txt", renderRobots(r.site.URL)); err != nil {
				return err
			}
		}
	}
	for _, f := range []string{"robots.txt", "CNAME", ".nojekyll", "vercel.json"} {
		if src, err := os.ReadFile(f); err == nil {
			if err := r.writeRaw(f, src); err != nil {
				return err
			}
		}
	}
	return nil
}

func (r *runner) renderPost(post *model.Page) error {
	data := r.pageData(post.URL, map[string]any{
		"Kind":           model.KindPost,
		"Page":           post,
		"Title":          post.Title,
		"Description":    post.Description,
		"Author":         post.Author,
		"Date":           post.DateString(),
		"UpdatedAt":      post.UpdatedString(),
		"Location":       post.Location,
		"Category":       post.Category,
		"CategoryTerm":   post.CategoryTerm,
		"Tags":           post.Tags,
		"TagTerms":       post.TagTerms,
		"Avatar":         post.Avatar,
		"Cover":          post.Cover,
		"Content":        post.Content,
		"WordCount":      post.WordCount,
		"ReadingMinutes": post.ReadingMinutes,
		"TOC":            post.TOC,
		"Draft":          post.Draft,
		"Prev":           post.Prev,
		"Next":           post.Next,
		"Related":        post.Related,
	})
	return r.writePage(post.Layout, post.URL, data, post.LastMod().Format("2006-01-02"), "0.7")
}

// HomeColumn is one category panel on the home page.
type HomeColumn struct {
	Name  string
	Slug  string
	URL   string
	Posts []*model.Page
}

func (r *runner) renderHome() error {
	perCat := r.cfg.Home.PostsPerCategory
	cols := r.buildHomeColumns(perCat)
	bySlug := map[string][]*model.Page{}
	for _, col := range cols {
		bySlug[col.Slug] = col.Posts
	}

	totalWords := 0
	for _, p := range r.site.Posts {
		totalWords += p.WordCount
	}
	cells, dl, ml, ht := computeHeatmap(r.site.Posts)

	data := r.pageData("/", map[string]any{
		"Kind":  model.KindHome,
		"Title": "Personal blog",
		"Home": map[string]string{
			"Title":    r.cfg.Home.Title,
			"Subtitle": r.cfg.Home.Subtitle,
			"Avatar":   r.cfg.Home.Avatar,
			"Owner":    r.cfg.Home.Owner,
		},
		"CategoryColumns": cols,
		// Legacy template fields so unmodified RainHush templates still work.
		"TechPosts":        bySlug["tech"],
		"LiteTechPosts":    bySlug["lite-tech"],
		"TriviaPosts":      bySlug["trivia"],
		"LifePosts":        bySlug["life"],
		"TotalPosts":       len(r.site.Posts),
		"TotalWords":       formatWords(totalWords),
		"TotalTags":        len(r.site.Tags.Terms),
		"TotalCategories":  len(r.site.Categories.Terms),
		"HeatmapCells":     cells,
		"HeatmapDayLabels": dl,
		"HeatmapMonths":    ml,
		"HeatmapTotal":     ht,
	})
	return r.writePage("index", "/", data, "", "1.0")
}

func (r *runner) buildHomeColumns(limit int) []HomeColumn {
	// If categories are configured in _config.yaml, show those in order;
	// otherwise fall back to the four classic RainHush buckets.
	specs := r.cfg.Categories
	if len(specs) == 0 {
		specs = []config.Category{
			{Name: "Tech", Slug: "tech"},
			{Name: "Lite Tech", Slug: "lite-tech"},
			{Name: "Trivia", Slug: "trivia"},
			{Name: "Life", Slug: "life"},
		}
	}
	cols := make([]HomeColumn, len(specs))
	idxBySlug := map[string]int{}
	fallbackIdx := -1
	for i, s := range specs {
		slug := s.Slug
		if slug == "" {
			slug = markdown.Slugify(s.Name)
		}
		cols[i] = HomeColumn{
			Name: s.Name,
			Slug: slug,
			URL:  r.cfg.Permalinks.Categories + slug + "/",
		}
		idxBySlug[slug] = i
		if slug == "life" || strings.EqualFold(s.Name, r.cfg.DefaultCategory) {
			fallbackIdx = i
		}
	}
	if fallbackIdx < 0 && len(cols) > 0 {
		fallbackIdx = len(cols) - 1
	}

	for _, p := range r.site.Posts {
		slug := markdown.Slugify(p.Category)
		i, ok := idxBySlug[slug]
		if !ok {
			i = fallbackIdx
		}
		if i >= 0 && len(cols[i].Posts) < limit {
			cols[i].Posts = append(cols[i].Posts, p)
		}
	}
	return cols
}

type pageItem struct {
	Number  int
	URL     string
	Current bool
}

func (r *runner) renderArticles() error {
	perPage := r.cfg.Pagination.PerPage
	posts := r.site.Posts
	totalPages := max(int(math.Ceil(float64(len(posts))/float64(perPage))), 1)
	firstURL := r.cfg.Permalinks.Articles
	pageURL := func(n int) string {
		if n <= 1 {
			return firstURL
		}
		return "/articles/page/" + strconv.Itoa(n) + "/"
	}

	for page := 1; page <= totalPages; page++ {
		start := (page - 1) * perPage
		end := min(start+perPage, len(posts))
		curURL := pageURL(page)

		items := make([]pageItem, totalPages)
		for i := 1; i <= totalPages; i++ {
			items[i-1] = pageItem{Number: i, URL: pageURL(i), Current: i == page}
		}
		prevURL, nextURL := "", ""
		if page > 1 {
			prevURL = pageURL(page - 1)
		}
		if page < totalPages {
			nextURL = pageURL(page + 1)
		}

		prio := "0.6"
		if page == 1 {
			prio = "0.9"
		}
		data := r.pageData(curURL, map[string]any{
			"Kind":       model.KindList,
			"Title":      "Articles",
			"Posts":      posts[start:end],
			"TotalCount": len(posts),
			"Page":       page,
			"TotalPages": totalPages,
			"HasPrev":    page > 1,
			"PrevURL":    prevURL,
			"HasNext":    page < totalPages,
			"NextURL":    nextURL,
			"PageItems":  items,
		})
		if err := r.writePage("articles", curURL, data, "", prio); err != nil {
			return err
		}
	}
	return nil
}

// friendLink is one bullet of content/friends/friends.md.
type friendLink struct {
	Nick string
	URL  string
	Host string
}

var friendLinkRe = regexp.MustCompile(`^\s{0,3}[-*+]\s+(.+?)\s*[:：]\s*\[[^\]]*\]\(\s*(\S+?)\s*\)\s*$`)

func parseFriendLinks(body string) ([]friendLink, string) {
	var links []friendLink
	var rest []string
	for _, line := range strings.Split(body, "\n") {
		m := friendLinkRe.FindStringSubmatch(line)
		if m == nil {
			rest = append(rest, line)
			continue
		}
		target := m[2]
		parsed, err := url.Parse(target)
		if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			rest = append(rest, line)
			continue
		}
		nick := strings.TrimSpace(m[1])
		if nick == "" {
			nick = parsed.Host
		}
		links = append(links, friendLink{Nick: nick, URL: target, Host: parsed.Host})
	}
	return links, strings.TrimSpace(strings.Join(rest, "\n"))
}

func (r *runner) renderStandalone(page *model.Page) error {
	extra := map[string]any{
		"Kind":        model.KindPage,
		"Page":        page,
		"Title":       page.Title,
		"Description": page.Description,
		"Content":     page.Content,
	}
	if page.Lang != "" {
		extra["Lang"] = page.Lang
	}
	if alt, ok := page.Params["lang_alt"].(string); ok && alt != "" {
		extra["LangAlt"] = alt
	}

	layout := page.Layout
	if layout == "friends" {
		friends, rest := parseFriendLinks(page.Body)
		var note string
		if len(friends) > 0 && rest != "" {
			note, _ = r.md.Convert(page.SourcePath, rest)
		}
		extra["Friends"] = friends
		extra["Note"] = template.HTML(note)
	}
	if !r.eng.HasLayout(layout) && r.eng.HasLayout("about") {
		layout = "about"
	}

	return r.writePage(layout, page.URL, r.pageData(page.URL, extra), page.LastMod().Format("2006-01-02"), "0.6")
}

func (r *runner) renderTaxonomy(tax *model.Taxonomy) error {
	// Index page: /tags/ or /categories/
	idxLayout := "terms"
	if !r.eng.HasLayout(idxLayout) {
		idxLayout = "about"
	}
	idxData := r.pageData(tax.URL, map[string]any{
		"Kind":     model.KindTerms,
		"Title":    tax.Title,
		"Taxonomy": tax,
		"Terms":    tax.ByCount(),
	})
	if err := r.writePage(idxLayout, tax.URL, idxData, "", "0.5"); err != nil {
		return err
	}

	termLayout := "term"
	if !r.eng.HasLayout(termLayout) {
		termLayout = "articles"
	}
	for _, term := range tax.Terms {
		if len(term.Pages) == 0 {
			continue
		}
		data := r.pageData(term.URL, map[string]any{
			"Kind":       model.KindTerm,
			"Title":      term.Name,
			"Taxonomy":   tax,
			"Term":       term,
			"Posts":      term.Pages,
			"TotalCount": len(term.Pages),
			"Page":       1,
			"TotalPages": 1,
		})
		if err := r.writePage(termLayout, term.URL, data, "", "0.5"); err != nil {
			return err
		}
	}
	return nil
}

// ArchiveYear groups posts by publication year for /archives/.
type ArchiveYear struct {
	Year  int
	Posts []*model.Page
}

func (r *runner) renderArchives() error {
	var years []ArchiveYear
	byYear := map[int]int{}
	for _, p := range r.site.Posts {
		y := p.Date.Year()
		idx, ok := byYear[y]
		if !ok {
			idx = len(years)
			byYear[y] = idx
			years = append(years, ArchiveYear{Year: y})
		}
		years[idx].Posts = append(years[idx].Posts, p)
	}

	layout := "archives"
	if !r.eng.HasLayout(layout) {
		layout = "articles"
	}
	u := r.cfg.Permalinks.Archives
	data := r.pageData(u, map[string]any{
		"Kind":       model.KindArchive,
		"Title":      "Archives",
		"Years":      years,
		"Posts":      r.site.Posts,
		"TotalCount": len(r.site.Posts),
	})
	return r.writePage(layout, u, data, "", "0.6")
}

func (r *runner) renderSearch() error {
	if err := r.writeRaw("search-index.json", buildSearchIndex(r.site)); err != nil {
		return err
	}
	if !r.eng.HasLayout("search") {
		return nil
	}
	u := r.cfg.Permalinks.Search
	data := r.pageData(u, map[string]any{
		"Kind":        model.KindSearch,
		"Title":       "Search",
		"SearchIndex": "/search-index.json",
		"TotalCount":  len(r.site.Posts),
	})
	return r.writePage("search", u, data, "", "0.4")
}

func (r *runner) render404() error {
	data := r.pageData("/404.html", map[string]any{
		"Kind":    model.KindNotFound,
		"Title":   "404",
		"NoIndex": true,
	})
	return r.writePage("404", "/404.html", data, "", "")
}

func (r *runner) runPluginGenerators() error {
	return plugin.Each(r.opts.Plugins, func(g plugin.Generator) error {
		vpages, files, err := g.Generate(r.site)
		if err != nil {
			return err
		}
		for _, vp := range vpages {
			layout := vp.Layout
			if layout == "" {
				layout = "page"
			}
			data := r.pageData(vp.URL, map[string]any{
				"Kind":    model.KindCustom,
				"Title":   vp.Title,
				"Content": vp.Content,
				"NoIndex": vp.NoIndex,
				"Data":    vp.Data,
			})
			prio := "0.5"
			if vp.NoIndex {
				prio = ""
			}
			if err := r.writePage(layout, vp.URL, data, "", prio); err != nil {
				return err
			}
		}
		for _, f := range files {
			if err := r.writeRaw(f.Path, f.Content); err != nil {
				return err
			}
		}
		return nil
	})
}
