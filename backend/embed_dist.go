package main

import (
	"embed"
	"io/fs"
)

//go:embed all:webdist
var embeddedWebFS embed.FS

func getEmbeddedDistFS() (fs.FS, bool) {
	sub, err := fs.Sub(embeddedWebFS, "webdist")
	if err != nil {
		return nil, false
	}
	if _, err := fs.Stat(sub, "index.html"); err != nil {
		return nil, false
	}
	return sub, true
}
