package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ericevans16/sitemaker/internal/config"
	"github.com/ericevans16/sitemaker/pkg/plugin"
	_ "github.com/ericevans16/sitemaker/plugins"
)

func TestFullSiteBuild(t *testing.T) {
	work := t.TempDir()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(work); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(orig) }()

	mustWrite := func(rel, body string) {
		p := filepath.Join(work, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}

	mustWrite("content/posts/2026-11-10-first.md", `---
title: "First Post"
date: "2026-11-10"
category: "Tech"
tags: ["Go", "SSG"]
---

Building an extensible static site generator in Go.
`)
	mustWrite("content/posts/2026-11-09-second.md", `---
title: "Second Post"
date: "2026-11-09"
category: "Tech"
tags: ["Go", "Agent"]
---

Agent loop architecture notes.
`)
	mustWrite("content/about/about_CN.md", "---\ntitle: About\n---\nHello.\n")
	mustWrite("content/friends/friends.md", "---\ntitle: Friends\n---\n- Alice: [alice](https://alice.example/)\n")

	cfg, err := config.Parse([]byte(`
site:
  title: Demo
  url: https://example.com
  description: Test site
  author: Tester
`))
	if err != nil {
		t.Fatal(err)
	}

	out := filepath.Join(work, "public")
	stats, err := Run(cfg, Options{OutDir: out, Plugins: plugin.Default})
	if err != nil {
		t.Fatalf("build.Run: %v", err)
	}
	if stats.Posts != 2 || stats.Tags != 3 {
		t.Fatalf("unexpected stats: %+v", stats)
	}

	for _, rel := range []string{
		"index.html",
		"index.frag.html",
		"articles.html",
		"articles/2026-11-10-first/index.html",
		"tags/index.html",
		"tags/go/index.html",
		"categories/index.html",
		"categories/tech/index.html",
		"archives/index.html",
		"search/index.html",
		"search-index.json",
		"about.html",
		"friends.html",
		"404.html",
		"feed.xml",
		"sitemap.xml",
		"llm.txt",
		"robots.txt",
	} {
		data, err := os.ReadFile(filepath.Join(out, rel))
		if err != nil || len(data) == 0 {
			t.Errorf("expected non-empty %s (err=%v)", rel, err)
		}
	}

	idx, _ := os.ReadFile(filepath.Join(out, "search-index.json"))
	if !strings.Contains(string(idx), "Agent") {
		t.Errorf("search-index.json missing post content: %s", idx)
	}
}
