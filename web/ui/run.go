package ui

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed dist/*
var distFiles embed.FS

func BuildDistFS() (http.FileSystem, error) {
	public, err := fs.Sub(distFiles, "dist")
	if err != nil {
		return nil, err
	}
	return http.FS(public), nil
}
