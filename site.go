package bifrost

import (
	"embed"
	"io/fs"
)

//go:embed web/*
var site embed.FS

func WebFS() fs.FS {
	sub, err := fs.Sub(site, "web")
	if err != nil {
		panic(err)
	}
	return sub
}
