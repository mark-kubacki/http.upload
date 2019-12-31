// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package upload

import (
	"net/http"
)

func Example() {
	var (
		scope     = "/" // prefix to http.Request.URL.Path
		directory = "/var/tmp"
		next      = http.FileServer(http.Dir(directory))
	)

	cfg := NewDefaultConfiguration(directory)
	uploadHandler, _ := NewHandler(scope, cfg, next)

	http.Handle(scope, uploadHandler)
	// http.ListenAndServe(":9000", nil)
}
