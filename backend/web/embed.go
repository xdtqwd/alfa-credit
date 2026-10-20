// Package web встраивает статический фронтенд в бинарь — один артефакт, один контейнер.
package web

import (
	"embed"
	"io/fs"
)

//go:embed static
var files embed.FS

func Static() fs.FS {
	sub, err := fs.Sub(files, "static")
	if err != nil {
		panic(err)
	}
	return sub
}
