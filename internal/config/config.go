// Package config loads and validates _config.yaml.
package config

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// FileName is the configuration file looked up in the site root.
const FileName = "_config.yaml"

type Site struct {
	Title       string `yaml:"title"`
	URL         string `yaml:"url"`
	Description string `yaml:"description"`
	Language    string `yaml:"language"`
	Author      string `yaml:"author"`
	Favicon     string `yaml:"favicon"`
	Keywords    string `yaml:"keywords"`
}

type Home struct {
	Title    string `yaml:"title"`
	Subtitle string `yaml:"subtitle"`
	Avatar   string `yaml:"avatar"`
	Owner    string `yaml:"owner"`
	// PostsPerCategory is how many recent posts each home column shows.
	PostsPerCategory int `yaml:"posts_per_category"`
}

type MenuItem struct {
	Name string `yaml:"name"`
	URL  string `yaml:"url"`
}

type Category struct {
	Name        string `yaml:"name"`
	Slug        string `yaml:"slug"`
	Description string `yaml:"description"`
}

type Permalinks struct {
	// Post is the URL pattern for posts. Tokens: :slug :year :month :day :category
	Post       string `yaml:"post"`
	Articles   string `yaml:"articles"` // first page of the article index
	Tags       string `yaml:"tags"`
	Categories string `yaml:"categories"`
	Archives   string `yaml:"archives"`
	Search     string `yaml:"search"`
}

type Pagination struct {
	PerPage int `yaml:"per_page"`
}

type Reading struct {
	WordsPerMinute int `yaml:"words_per_minute"`
}

// Features toggles the optional generators.
type Features struct {
	Tags       bool `yaml:"tags"`
	Categories bool `yaml:"categories"`
	Archives   bool `yaml:"archives"`
	Search     bool `yaml:"search"`
	RSS        bool `yaml:"rss"`
	Sitemap    bool `yaml:"sitemap"`
	Robots     bool `yaml:"robots"`
	LLMs       bool `yaml:"llms"`
	Related    bool `yaml:"related"`
	Fragments  bool `yaml:"fragments"` // *.frag.html for SPA navigation
	Minify     bool `yaml:"minify"`
}

type Feed struct {
	Limit       int  `yaml:"limit"`
	FullContent bool `yaml:"full_content"`
}

type Server struct {
	Host string `yaml:"host"`
	Port int    `yaml:"port"`
}

type Deploy struct {
	Remote  string `yaml:"remote"`
	Branch  string `yaml:"branch"`
	Message string `yaml:"message"`
}

type Config struct {
	Theme      string         `yaml:"theme"`
	Site       Site           `yaml:"site"`
	Home       Home           `yaml:"home"`
	Menu       []MenuItem     `yaml:"menu"`
	Categories []Category     `yaml:"categories"`
	// DefaultCategory files posts without a category front matter.
	DefaultCategory string         `yaml:"default_category"`
	Permalinks      Permalinks     `yaml:"permalinks"`
	Pagination      Pagination     `yaml:"pagination"`
	Reading         Reading        `yaml:"reading"`
	Features        Features       `yaml:"features"`
	Feed            Feed           `yaml:"feed"`
	Server          Server         `yaml:"server"`
	Deploy          Deploy         `yaml:"deploy"`
	Params          map[string]any `yaml:"params"`
	// Plugins maps a plugin name to true/false or an options mapping.
	Plugins map[string]any `yaml:"plugins"`
	// Extensions is the legacy RainHush spelling of Plugins.
	Extensions map[string]any `yaml:"extensions"`
}

// Default returns a configuration with every default filled in.
func Default() *Config {
	c := &Config{}
	c.Features = Features{
		Tags: true, Categories: true, Archives: true, Search: true, RSS: true,
		Sitemap: true, Robots: true, LLMs: true, Related: true, Fragments: true, Minify: true,
	}
	c.applyDefaults()
	return c
}

// Load reads _config.yaml from dir.
func Load(dir string) (*Config, error) {
	data, err := os.ReadFile(joinPath(dir, FileName))
	if err != nil {
		return nil, err
	}
	return Parse(data)
}

// Parse decodes configuration YAML on top of the defaults.
func Parse(data []byte) (*Config, error) {
	c := &Config{
		Features: Features{
			Tags: true, Categories: true, Archives: true, Search: true, RSS: true,
			Sitemap: true, Robots: true, LLMs: true, Related: true, Fragments: true, Minify: true,
		},
	}
	if err := yaml.Unmarshal(data, c); err != nil {
		return nil, fmt.Errorf("%s: %w", FileName, err)
	}
	c.applyDefaults()
	if err := c.validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", FileName, err)
	}
	return c, nil
}

func (c *Config) applyDefaults() {
	trim := func(s *string) { *s = strings.TrimSpace(*s) }
	for _, s := range []*string{&c.Theme, &c.Site.Title, &c.Site.URL, &c.Site.Description, &c.Site.Language,
		&c.Site.Author, &c.Site.Favicon, &c.Home.Title, &c.Home.Subtitle, &c.Home.Avatar, &c.Home.Owner,
		&c.DefaultCategory, &c.Deploy.Remote, &c.Deploy.Branch} {
		trim(s)
	}
	c.Site.URL = strings.TrimRight(c.Site.URL, "/")

	if c.Theme == "" {
		c.Theme = "blueprint"
	}
	// Legacy configs set the title under home:, new ones under site:.
	if c.Site.Title == "" {
		c.Site.Title = c.Home.Title
	}
	if c.Site.Title == "" {
		c.Site.Title = "My Site"
	}
	if c.Home.Title == "" {
		c.Home.Title = c.Site.Title
	}
	if c.Site.Author == "" {
		c.Site.Author = c.Home.Owner
	}
	if c.Home.Owner == "" {
		c.Home.Owner = c.Site.Author
	}
	if c.Site.Language == "" {
		c.Site.Language = "zh-CN"
	}
	if c.Home.PostsPerCategory <= 0 {
		c.Home.PostsPerCategory = 3
	}
	if len(c.Menu) == 0 {
		c.Menu = []MenuItem{
			{Name: "Home", URL: "/"},
			{Name: "Articles", URL: "/articles.html"},
			{Name: "About", URL: "/about.html"},
		}
	}
	for i := range c.Categories {
		c.Categories[i].Name = strings.TrimSpace(c.Categories[i].Name)
		c.Categories[i].Slug = strings.TrimSpace(c.Categories[i].Slug)
	}

	p := &c.Permalinks
	setDefault(&p.Post, "/articles/:slug/")
	setDefault(&p.Articles, "/articles.html")
	setDefault(&p.Tags, "/tags/")
	setDefault(&p.Categories, "/categories/")
	setDefault(&p.Archives, "/archives/")
	setDefault(&p.Search, "/search/")
	for _, s := range []*string{&p.Tags, &p.Categories} {
		if !strings.HasSuffix(*s, "/") {
			*s += "/"
		}
	}

	if c.Pagination.PerPage <= 0 {
		c.Pagination.PerPage = 10
	}
	if c.Reading.WordsPerMinute <= 0 {
		c.Reading.WordsPerMinute = 300
	}
	if c.Feed.Limit <= 0 {
		c.Feed.Limit = 20
	}
	if c.Server.Host == "" {
		c.Server.Host = "127.0.0.1"
	}
	if c.Server.Port == 0 {
		c.Server.Port = 8080
	}
	if c.Deploy.Branch == "" {
		c.Deploy.Branch = "gh-pages"
	}
	if c.Deploy.Message == "" {
		c.Deploy.Message = "Site update"
	}
	if c.Params == nil {
		c.Params = map[string]any{}
	}
}

func (c *Config) validate() error {
	for _, item := range c.Menu {
		if item.Name == "" || item.URL == "" {
			return fmt.Errorf("menu items need both name and url (got %+v)", item)
		}
	}
	for name, pattern := range map[string]string{
		"post": c.Permalinks.Post, "articles": c.Permalinks.Articles, "tags": c.Permalinks.Tags,
		"categories": c.Permalinks.Categories, "archives": c.Permalinks.Archives, "search": c.Permalinks.Search,
	} {
		if !strings.HasPrefix(pattern, "/") {
			return fmt.Errorf("permalinks.%s must start with '/' (got %q)", name, pattern)
		}
	}
	if !strings.Contains(c.Permalinks.Post, ":slug") {
		return fmt.Errorf("permalinks.post must contain :slug (got %q)", c.Permalinks.Post)
	}
	for _, cat := range c.Categories {
		if cat.Name == "" {
			return fmt.Errorf("categories entries need a name")
		}
	}
	return nil
}

// PluginSetting reports whether a plugin is enabled and its options.
// Plugins are enabled by default; `name: false` disables one.
func (c *Config) PluginSetting(name string) (enabled bool, options map[string]any) {
	for _, m := range []map[string]any{c.Extensions, c.Plugins} {
		raw, ok := m[name]
		if !ok {
			continue
		}
		switch v := raw.(type) {
		case bool:
			enabled, options = v, map[string]any{}
		case nil:
			enabled, options = true, map[string]any{}
		case map[string]any:
			enabled, options = true, v
			if e, ok := v["enabled"].(bool); ok {
				enabled = e
			}
		default:
			enabled, options = true, map[string]any{}
		}
	}
	if options == nil {
		return true, map[string]any{}
	}
	return enabled, options
}

func setDefault(s *string, v string) {
	*s = strings.TrimSpace(*s)
	if *s == "" {
		*s = v
	}
}

func joinPath(dir, name string) string {
	if dir == "" || dir == "." {
		return name
	}
	return strings.TrimRight(dir, "/") + "/" + name
}
