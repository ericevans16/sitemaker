// Package cli implements the sitemaker command-line interface.
package cli

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/ericevans16/sitemaker/internal/build"
	"github.com/ericevans16/sitemaker/internal/config"
	"github.com/ericevans16/sitemaker/internal/content"
	"github.com/ericevans16/sitemaker/internal/markdown"
	"github.com/ericevans16/sitemaker/internal/server"
	"github.com/ericevans16/sitemaker/internal/watcher"
	"github.com/ericevans16/sitemaker/pkg/plugin"
	_ "github.com/ericevans16/sitemaker/plugins"
)

// Execute runs the CLI with args (normally os.Args[1:]) and returns an exit code.
func Execute(args []string, version string) int {
	if len(args) == 0 {
		printUsage()
		return 1
	}

	var err error
	switch args[0] {
	case "-v", "--version", "version":
		fmt.Printf("sitemaker version %s\n", version)
	case "-h", "--help", "help":
		printUsage()
	case "build":
		err = cmdBuild(args[1:])
	case "serve", "dev", "test":
		err = cmdServe(args[1:])
	case "new":
		err = cmdNew(args[1:])
	case "list", "ls":
		err = cmdList(args[1:])
	case "plugins":
		err = cmdPlugins()
	case "init":
		err = cmdInit(args[1:])
	case "deploy", "push":
		err = cmdDeploy(args[1:])
	case "clean", "clear":
		err = os.RemoveAll("public")
		if err == nil {
			fmt.Println("public/ directory removed.")
		}
	default:
		fmt.Fprintf(os.Stderr, "Unknown command %q\n\n", args[0])
		printUsage()
		return 1
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	return 0
}

func cmdBuild(args []string) error {
	fs := flag.NewFlagSet("build", flag.ContinueOnError)
	drafts := fs.Bool("drafts", false, "include draft posts")
	fs.BoolVar(drafts, "D", false, "include draft posts (shorthand)")
	outDir := fs.String("out", "public", "output directory")
	fs.StringVar(outDir, "o", "public", "output directory (shorthand)")
	noMinify := fs.Bool("no-minify", false, "disable HTML/CSS minification")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := config.Load(".")
	if err != nil {
		return err
	}
	if *noMinify {
		cfg.Features.Minify = false
	}

	stats, err := build.Run(cfg, build.Options{
		OutDir:        *outDir,
		IncludeDrafts: *drafts,
	})
	if err != nil {
		return err
	}
	fmt.Printf("Built %d posts, %d pages, %d tags (%d files) -> %s/ in %s\n",
		stats.Posts, stats.Pages, stats.Tags, stats.Files, *outDir, stats.Duration.Round(time.Millisecond))
	return nil
}

func cmdServe(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	drafts := fs.Bool("drafts", false, "include draft posts")
	fs.BoolVar(drafts, "D", false, "include draft posts (shorthand)")
	port := fs.Int("port", 0, "HTTP port (overrides _config.yaml)")
	fs.IntVar(port, "p", 0, "HTTP port (shorthand)")
	host := fs.String("host", "", "bind address (overrides _config.yaml)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	broker := server.NewBroker()
	rebuild := func() (*config.Config, error) {
		cfg, err := config.Load(".")
		if err != nil {
			return nil, err
		}
		if *port > 0 {
			cfg.Server.Port = *port
		}
		if *host != "" {
			cfg.Server.Host = *host
		}
		stats, err := build.Run(cfg, build.Options{
			IncludeDrafts: *drafts,
			InjectHead:    server.LiveReloadSnippet,
		})
		if err != nil {
			return nil, err
		}
		fmt.Printf("Built %d posts, %d pages -> public/ in %s\n",
			stats.Posts, stats.Pages, stats.Duration.Round(time.Millisecond))
		return cfg, nil
	}

	cfg, err := rebuild()
	if err != nil {
		return err
	}

	stopWatcher, err := watcher.Watch(func(changed string) {
		fmt.Printf("Change detected: %s\n", changed)
		if strings.Contains(filepath.ToSlash(changed), "/styles/") {
			compileThemeStyles(cfg.Theme)
		}
		if _, err := rebuild(); err != nil {
			fmt.Fprintf(os.Stderr, "Rebuild failed: %v\n", err)
			return
		}
		broker.Reload()
	})
	if err != nil {
		return err
	}
	defer stopWatcher()

	return server.Serve(context.Background(), server.Options{
		Host:   cfg.Server.Host,
		Port:   cfg.Server.Port,
		Dir:    "public",
		Broker: broker,
	})
}

func compileThemeStyles(themeName string) {
	script := filepath.Join("themes", themeName, "scripts", "build-styles.mjs")
	if _, err := os.Stat(script); err != nil {
		return
	}
	cmd := exec.Command("node", script)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	_ = cmd.Run()
}

// cmdNew creates a new post or page:
//
//	sitemaker new my-slug
//	sitemaker new --title "My Post" --category Tech --tags go,ssg --draft my-slug
//	sitemaker new page notes/ideas
func cmdNew(args []string) error {
	kind := "post"
	if len(args) > 0 && (args[0] == "post" || args[0] == "page") {
		kind = args[0]
		args = args[1:]
	}

	fs := flag.NewFlagSet("new", flag.ContinueOnError)
	title := fs.String("title", "", "post/page title")
	fs.StringVar(title, "t", "", "post/page title (shorthand)")
	category := fs.String("category", "", "post category")
	fs.StringVar(category, "c", "", "post category (shorthand)")
	tags := fs.String("tags", "", "comma-separated tags")
	draft := fs.Bool("draft", false, "create as draft")
	fs.BoolVar(draft, "d", false, "create as draft (shorthand)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := config.Load(".")
	if err != nil {
		cfg = config.Default()
	}

	slug := "new-post"
	if fs.NArg() > 0 {
		slug = strings.TrimSuffix(fs.Arg(0), ".md")
	}
	today := time.Now().Format("2006-01-02")

	var target string
	var body strings.Builder
	if kind == "page" {
		target = filepath.Join("content", filepath.FromSlash(slug)+".md")
		t := *title
		if t == "" {
			t = filepath.Base(slug)
		}
		fmt.Fprintf(&body, "---\ntitle: %q\ndate: %q\n---\n\nWrite your page content here.\n", t, today)
	} else {
		fileName := slug
		if !hasDatePrefix(fileName) {
			fileName = today + "-" + markdown.Slugify(slug)
		}
		target = filepath.Join("content", "posts", fileName+".md")
		t := *title
		if t == "" {
			t = "新文章标题"
		}
		cat := *category
		if cat == "" {
			cat = cfg.DefaultCategory
			if cat == "" {
				cat = "Tech"
			}
		}
		avatar := cfg.Home.Avatar
		if avatar == "" {
			avatar = "/static/imgs/avatar.png"
		}

		fmt.Fprintf(&body, "---\ntitle: %q\nauthor: %q\ndate: %q\nupdated_at: %q\n", t, cfg.Site.Author, today, today)
		fmt.Fprintf(&body, "avatar: %q\ncategory: %q\n", avatar, cat)
		if *tags != "" {
			var parts []string
			for _, part := range strings.Split(*tags, ",") {
				if p := strings.TrimSpace(part); p != "" {
					parts = append(parts, fmt.Sprintf("%q", p))
				}
			}
			fmt.Fprintf(&body, "tags: [%s]\n", strings.Join(parts, ", "))
		} else {
			body.WriteString("tags: []\n")
		}
		if *draft {
			body.WriteString("draft: true\n")
		}
		body.WriteString("---\n\n这里开始撰写你的文章正文...\n\n## 第一小节\n\n正文内容...\n")
	}

	if _, err := os.Stat(target); err == nil {
		return fmt.Errorf("%s already exists", target)
	}
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		return err
	}
	if err := os.WriteFile(target, []byte(body.String()), 0644); err != nil {
		return err
	}
	fmt.Printf("Created %s\n", filepath.ToSlash(target))
	return nil
}

func hasDatePrefix(name string) bool {
	if len(name) < 11 {
		return false
	}
	_, err := time.Parse("2006-01-02", name[:10])
	return err == nil && name[10] == '-'
}

func cmdList(args []string) error {
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	drafts := fs.Bool("drafts", true, "include drafts")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := config.Load(".")
	if err != nil {
		return err
	}
	site, err := content.Load(cfg, markdown.New(markdown.Options{}), content.Options{IncludeDrafts: *drafts})
	if err != nil {
		return err
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "DATE\tSTATUS\tCATEGORY\tTAGS\tTITLE\tURL")
	for _, p := range site.Posts {
		status := "published"
		if p.Draft {
			status = "draft"
		}
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n",
			p.DateString(), status, p.Category, strings.Join(p.Tags, ","), p.Title, p.URL)
	}
	return tw.Flush()
}

func cmdPlugins() error {
	cfg, err := config.Load(".")
	if err != nil {
		cfg = config.Default()
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "PLUGIN\tENABLED\tCAPABILITIES")
	for _, name := range plugin.Default.Names() {
		plugin.Default.SetEnabled(name, true)
	}
	for _, p := range plugin.Default.Enabled() {
		enabled, _ := cfg.PluginSetting(p.Name())
		var caps []string
		if _, ok := p.(plugin.FenceRenderer); ok {
			caps = append(caps, "fence")
		}
		if _, ok := p.(plugin.MarkdownExtender); ok {
			caps = append(caps, "markdown")
		}
		if _, ok := p.(plugin.AssetProvider); ok {
			caps = append(caps, "assets")
		}
		if _, ok := p.(plugin.TemplateFuncs); ok {
			caps = append(caps, "funcs")
		}
		if _, ok := p.(plugin.PageTransformer); ok {
			caps = append(caps, "transform")
		}
		if _, ok := p.(plugin.SiteHook); ok {
			caps = append(caps, "site")
		}
		if _, ok := p.(plugin.Generator); ok {
			caps = append(caps, "generator")
		}
		if _, ok := p.(plugin.PostBuildHook); ok {
			caps = append(caps, "post-build")
		}
		_, _ = fmt.Fprintf(tw, "%s\t%v\t%s\n", p.Name(), enabled, strings.Join(caps, ", "))
	}
	return tw.Flush()
}

func cmdInit(args []string) error {
	target := "."
	if len(args) > 0 {
		target = args[0]
	}
	cfgPath := filepath.Join(target, config.FileName)
	if _, err := os.Stat(cfgPath); err == nil {
		return fmt.Errorf("%s already exists", cfgPath)
	}
	for _, d := range []string{"content/posts", "content/about", "static/imgs"} {
		if err := os.MkdirAll(filepath.Join(target, d), 0755); err != nil {
			return err
		}
	}
	sampleCfg := `theme: blueprint

site:
  title: My Site
  url: https://example.com
  description: A technical blog built with sitemaker
  author: Author
  language: zh-CN

menu:
  - name: Home
    url: /
  - name: Articles
    url: /articles.html
  - name: Tags
    url: /tags/
  - name: Archives
    url: /archives/
  - name: Search
    url: /search/
  - name: About
    url: /about.html
`
	if err := os.WriteFile(cfgPath, []byte(sampleCfg), 0644); err != nil {
		return err
	}
	fmt.Printf("Initialized site in %s\n", target)
	return nil
}

func cmdDeploy(args []string) error {
	if err := cmdBuild(args); err != nil {
		return err
	}
	cfg, err := config.Load(".")
	if err != nil {
		return err
	}
	remote := cfg.Deploy.Remote
	if remote == "" {
		out, err := exec.Command("git", "remote", "get-url", "origin").Output()
		if err != nil {
			return fmt.Errorf("deploy.remote is not set and git remote 'origin' could not be read: %w", err)
		}
		remote = strings.TrimSpace(string(out))
	}
	branch := cfg.Deploy.Branch
	run := func(name string, arg ...string) error {
		cmd := exec.Command(name, arg...)
		cmd.Dir = "public"
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		return cmd.Run()
	}
	if _, err := os.Stat("public/.git"); os.IsNotExist(err) {
		if err := run("git", "init"); err != nil {
			return err
		}
	}
	if err := run("git", "checkout", "-B", branch); err != nil {
		return err
	}
	if err := run("git", "add", "-A"); err != nil {
		return err
	}
	_ = run("git", "commit", "-m", cfg.Deploy.Message)
	return run("git", "push", "-f", remote, branch)
}

func printUsage() {
	fmt.Print(`sitemaker — fast, extensible static site generator

Usage:
  sitemaker build   [-D|--drafts] [-o dir] [--no-minify]   Build the site into public/
  sitemaker serve   [-D|--drafts] [-p port] [--host addr]  Build, serve with live-reload & watch
  sitemaker new     [post|page] [-t title] [-c cat] [-tags a,b] [-d] <slug>
  sitemaker list    [--drafts=false]                       List all posts in a table
  sitemaker plugins                                        List registered plugins & status
  sitemaker deploy                                         Build and push public/ to deploy.branch
  sitemaker clean                                          Remove the public/ directory
  sitemaker init    [dir]                                  Initialize a minimal _config.yaml

Flags:
  -v, --version   Print version
  -h, --help      Print this help
`)
}
