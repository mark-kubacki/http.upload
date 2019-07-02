// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// +build !caddyserver0.9

package upload // import "blitznote.com/src/http.upload"

import (
	"net/http"

	"blitznote.com/src/http.upload"
)

func Example() {
	var (
		scope     = "/" // prefix to http.Request.URL.Path
		directory = "/var/tmp"
		next      = http.FileServer(http.Dir(directory))
	)

	cfg := upload.NewDefaultConfiguration(directory)
	uploadHandler, _ := upload.NewHandler(scope, cfg, next)

	http.Handle(scope, uploadHandler)
	// http.ListenAndServe(":9000", nil)
}
