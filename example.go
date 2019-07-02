// This file is released into the public domain.

// +build ignore

// Package main implements a minimal http server that accepts uploads.
//
// For example, this is how you'd upload a file using `curl`:
//  go run "this file"
//  curl -T /etc/os-release http://127.0.0.1:9000/from-release
package main

import (
	"net/http"
	"os"

	"blitznote.com/src/http.upload"
)

func main() {
	var (
		scope     = "/"
		directory = os.TempDir()
		next      = http.FileServer(http.Dir(directory))
	)

	cfg := upload.NewDefaultConfiguration(directory)
	uploadHandler, _ := upload.NewHandler(scope, cfg, next)

	http.Handle(scope, uploadHandler)
	http.ListenAndServe(":9000", nil)
}
