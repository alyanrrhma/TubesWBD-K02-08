// Package webassets menanamkan templates dan file statis ke dalam binary
package webassets

import "embed"

//go:embed templates static
var FS embed.FS