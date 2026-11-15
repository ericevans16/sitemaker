package theme

import "embed"

// EmbeddedBlueprint holds the built-in "blueprint" theme (layouts, static
// assets and theme.yaml) so `sitemaker` can build a site even when no local
// themes/ directory is present in the site repository.
//
//go:embed all:themes/blueprint
var EmbeddedBlueprint embed.FS
