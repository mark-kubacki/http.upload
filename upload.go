package upload // import "blitznote.com/src/caddy.upload"

import (
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/mholt/caddy/config/setup"
	"github.com/mholt/caddy/middleware"
)

// Configures an UploadHander instance.
// This is called by Caddy every time the corresponding directive is used.
func Setup(c *setup.Controller) (middleware.Middleware, error) {
	h := UploadHandler{}

	return func(next middleware.Handler) middleware.Handler {
		h.Next = next
		return h
	}, nil
}

// Sink for files.
// XXX(mark): HMAC support
// XXX(mark): auto-cipher
// XXX(mark): lock, and timer-based lock reset
type UploadHandler struct {
	Next middleware.Handler
}

// Hijacks uploads, passthrough to everything else.
// POST is used with curl -F bashrc=@.bashrc <url>
// PUT when you use curl -T <filename> <url>
func (h UploadHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) (int, error) {
	// XXX(mark): check the HMAC
	// XXX(mark): passthrough if stealthy, else return 401
	switch r.Method {
	case "POST":
		ctype := r.Header.Get("Content-Type")
		if strings.HasPrefix(ctype, "multipart/form-data") {
			return h.ServeMultipartUpload(w, r)
		}
		if ctype != "" {
			return 415, nil
		}
		fallthrough
	case "PUT":
		// a single file: excess path is the name, the body its content
		// XXX(mark): store the file
		return 400, nil
	}

	// It's not up to us where the files go, and what happens on reads.
	// Worst case is the requestor gets a 404, 405, or 410.
	return h.Next.ServeHTTP(w, r)
}

// Used in HTTP multipart uploads.
func (h UploadHandler) ServeMultipartUpload(w http.ResponseWriter, r *http.Request) (int, error) {
	mr, err := r.MultipartReader()
	if err != nil {
		return 415, fmt.Errorf("Please try again using MIME multipart format for the upload.")
	}

	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return 400, err
		}

		fileName := part.FileName()
		if fileName == "" {
			continue
		}
		// XXX(mark): store the file
		fmt.Println(fileName)
	}

	return 200, nil
}
