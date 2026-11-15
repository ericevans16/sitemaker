// Package plugins imports the built-in plugins so their init() functions
// register with plugin.Default.
package plugins

import (
	_ "github.com/ericevans16/sitemaker/plugins/callout"
	_ "github.com/ericevans16/sitemaker/plugins/friends"
	_ "github.com/ericevans16/sitemaker/plugins/imagelayout"
)
