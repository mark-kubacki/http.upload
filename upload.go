package upload // import "blitznote.com/src/caddy.upload"

import (
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/mholt/caddy/middleware"

	"blitznote.com/src/caddy.upload/protofile"
)

var (
	ErrCannotReadMIMEMultipart = errors.New("Error reading MIME multipart")
)

// Handler represents a configured instance of this plugin.
//
// If you want to use it outside of Caddy, then implement 'Next' as
// something with method ServeHTTP and at least the same member variables
// that you can find here.
type Handler struct {
	Next   middleware.Handler
	Config HandlerConfiguration
}

// ServeHTTP is a gateway to ServeMultipartUpload and WriteOneHTTPBlob on uploads, else a passthrough.
//
// POST
// is used with
//  curl -F bashrc=@.bashrc <url>
// PUT
// when you use
//  curl -T <filename> <url>
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) (int, error) {
	var scope string // aka 'target directory'
	switch r.Method {
	case "POST", "PUT":
		for _, pathPrefix := range h.Config.PathScopes {
			if middleware.Path(r.URL.Path).Matches(pathPrefix) {
				scope = pathPrefix
				break
			}
		}
	default:
		// Reads are not our responsibility.
		// Worst case the requestor gets a 404, 405, or 410.
		return h.Next.ServeHTTP(w, r)
	}
	if scope == "" {
		return h.Next.ServeHTTP(w, r)
	}

	if resp, err := h.authenticate(r); err != nil {
		if h.Config.SilenceAuthErrors {
			return h.Next.ServeHTTP(w, r)
		}
		if resp == 401 {
			// send this header to prevent the user from being asked for a username/password pair
			w.Header().Set("WWW-Authenticate", "Signature")
		}
		return resp, err
	}

	switch r.Method {
	case "POST":
		ctype := r.Header.Get("Content-Type")
		switch {
		case strings.HasPrefix(ctype, "multipart/form-data"):
			return h.ServeMultipartUpload(w, r, scope)
		case ctype != "": // other envelope formats, not implemented
			return http.StatusUnsupportedMediaType, nil // 415: unsupported media type
		}
		fallthrough
	case "PUT":
		if len(r.URL.Path) < 2 {
			return http.StatusBadRequest, nil // no filename given
		}
		fileName := r.URL.Path[1:]
		_, retval, err := h.WriteOneHTTPBlob(scope, fileName, r.Header.Get("Content-Length"), r.Body)
		return retval, err
	}

	// impossible to reach, but makes static code analyzers happy
	return h.Next.ServeHTTP(w, r)
}

// ServeMultipartUpload explodes one or more supplied files,
// and feeds them to WriteOneHTTPBlob one by one.
func (h *Handler) ServeMultipartUpload(w http.ResponseWriter, r *http.Request, scope string) (int, error) {
	mr, err := r.MultipartReader()
	if err != nil {
		return http.StatusUnsupportedMediaType, ErrCannotReadMIMEMultipart
	}

	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return http.StatusBadRequest, err
		}

		fileName := part.FileName()
		if fileName == "" {
			continue
		}

		_, retval, err := h.WriteOneHTTPBlob(scope, fileName, part.Header.Get("Content-Length"), part)
		if err != nil {
			return retval, err
		}
	}

	return http.StatusOK, nil
}

// Translates the 'scope' into a proper directory, and extracts the filename from the resulting string.
func (h *Handler) splitInDirectoryAndFilename(scope, providedName string) (string, string, *os.PathError) {
	s := strings.TrimPrefix(providedName, scope)               // "/upload/mine/my.blob" → "/mine/my.blob"
	s = h.Config.WriteToPath + "/" + strings.TrimLeft(s, "./") // → "/var/mine/my.blob"

	// stop any childish path trickery here
	ref := filepath.Clean(s) // "/var/mine/../mine/my.blob" → "/var/mine/my.blob"
	if !strings.HasPrefix(ref, h.Config.WriteToPath) {
		return "", "", &os.PathError{Op: "create", Path: ref, Err: os.ErrPermission}
	}

	// extract path from filename
	return filepath.Dir(ref), filepath.Base(ref), nil
}

// WriteOneHTTPBlob adapts WriteFileFromReader to HTTP conventions
// by translating input and output values.
func (h *Handler) WriteOneHTTPBlob(scope, fileName, anticipatedSize string, r io.Reader) (int64, int, error) {
	expectBytes, _ := strconv.ParseInt(anticipatedSize, 10, 64)
	if anticipatedSize != "" && expectBytes <= 0 { // we cannot work with that
		return 0, http.StatusLengthRequired, nil // 411: length required
		// Usually 411 is used for the outermost element.
		// We don't require any length; but if the key exists, the value must be valid.
	}

	path, fname, err1 := h.splitInDirectoryAndFilename(scope, fileName)
	if err1 != nil {
		return 0, 422, os.ErrPermission // 422: unprocessable entity
	}

	bytesWritten, err := WriteFileFromReader(path, fname, r, expectBytes)
	if err != nil {
		if os.IsExist(err) { // gets thrown on a double race condition when using O_TMPFILE and linkat
			return 0, http.StatusConflict, err // 409: conflict (most probably a write-after-write)
		}
		if bytesWritten > 0 && bytesWritten < expectBytes {
			return bytesWritten, 507, err // 507: insufficient storage
			// The client could've shortened us.
		}
		return bytesWritten, 500, err
	}
	if bytesWritten < expectBytes {
		return bytesWritten, 202, nil // 202: accepted (but not completed)
	}
	return bytesWritten, 200, nil // 200: all dope
}

// WriteFileFromReader implements an unit of work consisting of
// • creation of a temporary file,
// • writing to it,
// • discarding it on failure ('zap') or
// • its "emergence" ('persist') into observable namespace.
//
// If 'anticipatedSize' ≥ protofile.reserveFileSizeThreshold (usually 32 KiB)
// then disk space will be reserved before writing (by a ProtoFileBehaver).
func WriteFileFromReader(path, filename string, r io.Reader, anticipatedSize int64) (int64, error) {
	wp, err := protofile.IntentNew(path, filename)
	if err != nil {
		return 0, err
	}
	w := *wp
	defer w.Zap()

	w.SizeWillBe(anticipatedSize)

	n, err := io.Copy(w, r)
	if err != nil {
		return n, err
	}
	err = w.Persist()
	return n, err
}
