// UploadHandler provides facilities to conduct uploads
// using the HTTP protocol as transport.
//
// If possible, i. e. if the operating- and filesystem implements it,
// files will not emerge before their first upload is completed.
// This is of importance to software that monitors a set of paths and
// reacts to new files. For example, with the intention to trigger uploads
// to other locations (mirrors).
//
// For request authentication, this is how you generate a HMAC in shell scripts
// and encode it using base64:
//  key="geheim"
//  timestamp="$(date --utc +%s)"
//  token="streng"
//
//  printf "${timestamp}${token}" \
//  | openssl dgst -sha256 -hmac "${key}" -binary \
//  | openssl enc -base64
//
// See also: https://en.wikipedia.org/wiki/List_of_HTTP_status_codes
package upload // import "blitznote.com/src/caddy.upload"

import (
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/mholt/caddy/caddy/setup"
	"github.com/mholt/caddy/middleware"

	"blitznote.com/src/caddy.upload/protofile"
)

// Configures an UploadHander instance.
// This is called by Caddy every time the corresponding directive is used.
func Setup(c *setup.Controller) (middleware.Middleware, error) {
	config, err := parseCaddyConfig(c)
	if err != nil {
		return nil, err
	}

	return func(next middleware.Handler) middleware.Handler {
		return &UploadHandler{
			Next:   next,
			Config: config,
		}
	}, nil
}

func parseCaddyConfig(c *setup.Controller) (UploadHandlerConfiguration, error) {
	var config UploadHandlerConfiguration
	config.TimestampTolerance = 1 << 2

	for c.Next() {
		config.PathScopes = c.RemainingArgs() // most likely only one path; but could be more

		for c.NextBlock() {
			key := c.Val()
			switch key {
			case "to":
				if !c.NextArg() {
					return config, c.ArgErr()
				}
				// must be a directory
				writeToPath := c.Val()
				finfo, err := os.Stat(writeToPath)
				if err != nil {
					return config, c.Err(err.Error())
				}
				if !finfo.IsDir() {
					return config, c.Err("'to' must be a directory or mount point")
				}
				config.WriteToPath = writeToPath
			case "hmac_key_in":
				if !c.NextArg() {
					return config, c.Err("'hmac_key_in' must be followed by a base64-encoded string")
				}
				k, err := base64.StdEncoding.DecodeString(c.Val())
				if err != nil {
					return config, c.Err(err.Error())
				}
				config.IncomingSharedHmacSecret = k
			case "timestamp_tolerance":
				if !c.NextArg() {
					return config, c.Err("'timestamp_tolerance' accepts a positive integer, which is missing")
				}
				s, err := strconv.ParseUint(c.Val(), 10, 32)
				if err != nil {
					return config, c.Err(err.Error())
				}
				if s > 60 { // someone configured a ridiculously high exponent
					return config, c.Err("we're sorry, but by this time Sol has already melted Terra")
				}
				if s > 32 {
					return config, c.Err("must be ≤ 32")
				}
				config.TimestampTolerance = 1 << s
			case "silent_auth_errors":
				config.SilenceAuthErrors = true
			}
		}
	}

	if config.PathScopes == nil || len(config.PathScopes) == 0 {
		return config, c.ArgErr()
	}
	return config, nil
}

type UploadHandler struct {
	Next   middleware.Handler
	Config UploadHandlerConfiguration
}

// XXX(mark): auto-cipher
// XXX(mark): lock, and timer-based lock reset

// State of UploadHandler, result of directives found in a 'Caddyfile'.
type UploadHandlerConfiguration struct {
	// How big a difference between 'now' and the provided timestamp do we tolerate?
	// In seconds. Due to possible optimizations this should be an order of 2.
	// A reasonable default is 1<<2.
	TimestampTolerance uint64

	// prefixes on which Caddy activates this plugin (read-only)
	PathScopes []string

	// the upload destination
	WriteToPath string

	// Already decoded. Request verification is disabled if this is empty.
	IncomingSharedHmacSecret []byte

	// A skilled attacked will monitor traffic, and timings. This merely obscures the path.
	SilenceAuthErrors bool
}

// Gateway to ServeMultipartUpload and WriteOneHttpBlob on uploads, else a passthrough.
//
// POST
// is used with
//  curl -F bashrc=@.bashrc <url>
// PUT
// when you use
//  curl -T <filename> <url>
func (h UploadHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) (int, error) {
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
		return resp, err
	}

	switch r.Method {
	case "POST":
		ctype := r.Header.Get("Content-Type")
		switch {
		case strings.HasPrefix(ctype, "multipart/form-data"):
			return h.ServeMultipartUpload(w, r, scope)
		case ctype != "": // other envelope formats, not implemented
			return 415, nil // 415: unsupported media type
		}
		fallthrough
	case "PUT":
		fileName := r.RequestURI[1:]
		_, retval, err := h.WriteOneHttpBlob(scope, fileName, r.Header.Get("Content-Length"), r.Body)
		return retval, err
	}

	// impossible to reach, but makes static code analyzers happy
	return h.Next.ServeHTTP(w, r)
}

// Unwraps one or more supplied files, and feeds them to WriteOneHttpBlob.
func (h UploadHandler) ServeMultipartUpload(w http.ResponseWriter, r *http.Request, scope string) (int, error) {
	mr, err := r.MultipartReader()
	if err != nil {
		return 415, fmt.Errorf("Malformed Content")
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

		_, retval, err := h.WriteOneHttpBlob(scope, fileName, part.Header.Get("Content-Length"), part)
		if err != nil {
			return retval, err
		}
	}

	return 200, nil
}

// Translates the 'scope' into a proper directory, and extracts the filename from the resulting string.
func (h UploadHandler) splitInDirectoryAndFilename(scope, providedName string) (string, string, *os.PathError) {
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

// Adapts WriteFileFromReader to HTTP conventions by translating input formats and output values.
func (h UploadHandler) WriteOneHttpBlob(scope, fileName, anticipatedSize string, r io.Reader) (int64, int, error) {
	expectBytes, _ := strconv.ParseInt(anticipatedSize, 10, 64)
	if anticipatedSize != "" && expectBytes <= 0 { // we cannot work with that
		return 0, 411, nil // 411: length required
		// Usually 411 is used for the outermost element.
		// We don't require any length; but if the key exists, the value must be valid.
	}

	path, fname, err1 := h.splitInDirectoryAndFilename(scope, fileName)
	if err1 != nil {
		return 0, 422, err1 // 422: unprocessable entity
	}

	bytesWritten, err := WriteFileFromReader(path, fname, r, expectBytes)
	if err != nil {
		if os.IsExist(err) { // gets thrown on a double race condition when using O_TMPFILE and linkat
			return 0, 409, err // 409: conflict (most probably a write-after-write)
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

// Unit of work implementing
// • creation of a temporary file,
// • writing to it,
// • discarding it on failure ('zap') or
// • its "emergence" ('persist') into observable namespace.
//
// If 'anticipatedSize' ≥ protofile.reserveFileSizeThreshold (usually 32 KiB)
// then disk space will be reserved before writing by the employed ProtoFileBehaver.
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
