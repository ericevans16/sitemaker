package config

import "testing"

func TestParseLegacyRainHushConfig(t *testing.T) {
	raw := []byte(`
server:
  port: 9090
site:
  url: https://example.com/
  description: Demo
home:
  title: DemoTitle
  owner: Alice
extensions:
  image-layout: false
  custom:
    verbose: true
`)
	cfg, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if cfg.Site.Title != "DemoTitle" || cfg.Site.Author != "Alice" || cfg.Site.URL != "https://example.com" {
		t.Fatalf("unexpected site config: %+v", cfg.Site)
	}
	if enabled, _ := cfg.PluginSetting("image-layout"); enabled {
		t.Fatal("expected image-layout to be disabled")
	}
	enabled, opts := cfg.PluginSetting("custom")
	if !enabled || opts["verbose"] != true {
		t.Fatalf("unexpected custom plugin settings: enabled=%v opts=%v", enabled, opts)
	}
}

func TestValidateRejectsBadPermalink(t *testing.T) {
	if _, err := Parse([]byte("permalinks:\n  post: /articles/fixed\n")); err == nil {
		t.Fatal("expected validation error when :slug is missing")
	}
}
