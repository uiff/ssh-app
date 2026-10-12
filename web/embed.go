// Package web bettet die Browser-Oberflaeche ins Programm ein.
package web

import "embed"

//go:embed static
var Static embed.FS
