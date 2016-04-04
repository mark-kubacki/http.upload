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
	"golang.org/x/text/unicode/norm"

	"blitznote.com/src/caddy.upload/protofile"
)

const (
	// at this point this is an arbitrary number
	reportProgressEveryBytes = 1 << 15
)

var (
	ErrCannotReadMIMEMultipart = errors.New("Error reading MIME multipart")
	ErrFileNameConflict        = errors.New("Name-Name Conflict")
	ErrInvalidFileName         = errors.New("Invalid filename") // includes the path
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
	var (
		scope  string // a prefix we will need to replace with the target directory
		config *ScopeConfiguration
	)

	switch r.Method {
	case "POST", "PUT", "COPY", "MOVE", "DELETE":
		// iterate the scopes in the order they have been defined
		for idx := range h.Config.PathScopes {
			if middleware.Path(r.URL.Path).Matches(h.Config.PathScopes[idx]) {
				scope = h.Config.PathScopes[idx]
				config = h.Config.Scope[scope]
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

	if resp, err := h.authenticate(r, config); err != nil {
		if config.SilenceAuthErrors {
			return h.Next.ServeHTTP(w, r)
		}
		if resp == 401 {
			// send this header to prevent the user from being asked for a username/password pair
			w.Header().Set("WWW-Authenticate", "Signature")
		}
		return resp, err
	}

	switch r.Method {
	case "COPY":
		return http.StatusNotImplemented, nil
	case "MOVE":
		destName := r.Header.Get("Destination")
		if len(r.URL.Path) < 2 || destName == "" {
			return http.StatusBadRequest, nil
		}
		return h.MoveOneFile(scope, config, r.URL.Path, destName)
	case "DELETE":
		if len(r.URL.Path) < 2 {
			return http.StatusBadRequest, nil
		}
		return h.DeleteOneFile(scope, config, r.URL.Path)
	case "POST":
		ctype := r.Header.Get("Content-Type")
		switch {
		case strings.HasPrefix(ctype, "multipart/form-data"):
			return h.ServeMultipartUpload(w, r, scope, config)
		case ctype != "": // other envelope formats, not implemented
			return http.StatusUnsupportedMediaType, nil // 415: unsupported media type
		}
		fallthrough
	case "PUT":
		if len(r.URL.Path) < 2 {
			return http.StatusBadRequest, nil
		}
		_, retval, err := h.WriteOneHTTPBlob(scope, config, r.URL.Path,
			r.Header.Get("Content-Length"), r.Body)
		return retval, err
	}

	// impossible to reach, but makes static code analyzers happy
	return h.Next.ServeHTTP(w, r)
}

// ServeMultipartUpload explodes one or more supplied files,
// and feeds them to WriteOneHTTPBlob one by one.
func (h *Handler) ServeMultipartUpload(w http.ResponseWriter, r *http.Request,
	scope string, config *ScopeConfiguration) (int, error) {
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

		_, retval, err := h.WriteOneHTTPBlob(scope, config, fileName, part.Header.Get("Content-Length"), part)
		if err != nil {
			return retval, err
		}
	}

	return http.StatusOK, nil
}

// Translates the 'scope' into a proper directory, and extracts the filename from the resulting string.
func (h *Handler) translateForFilesystem(scope, providedName string, config *ScopeConfiguration) (fsPath, fsFilename string, err error) {
	// 'uc' is freely controlled by the uploader
	uc := strings.TrimPrefix(providedName, scope)                      // "/upload/mine/my.blob" → "/mine/my.blob"
	s := filepath.Join(config.WriteToPath, strings.TrimLeft(uc, "./")) // → "/var/mine/my.blob"

	// stop any childish path trickery here
	translated := filepath.Clean(s) // "/var/mine/../mine/my.blob" → "/var/mine/my.blob"
	if !strings.HasPrefix(translated, config.WriteToPath) {
		err = os.ErrPermission
		return
	}

	var enforceForm *norm.Form
	if config.UnicodeForm != nil {
		enforceForm = &config.UnicodeForm.Use
	}
	if !IsAcceptableFilename(uc, config.RestrictFilenamesTo, enforceForm) {
		err = ErrInvalidFileName
		return
	}

	fsPath, fsFilename = filepath.Dir(translated), filepath.Base(translated)

	return
}

// MoveOneFile renames a file or path.
//
// The destination filename is parsed as if it were an URL.Path.
func (h *Handler) MoveOneFile(scope string, config *ScopeConfiguration,
	fromFilename, toFilename string) (int, error) {
	frompath, fromname, err := h.translateForFilesystem(scope, fromFilename, config)
	if err != nil {
		return 422, os.ErrPermission
	}
	topath, toname, err := h.translateForFilesystem(scope, toFilename, config)
	if err != nil {
		return 422, os.ErrPermission
	}

	if fromname == toname && frompath == topath {
		return http.StatusConflict, nil
	}

	err = os.Rename(filepath.Join(frompath, fromname), filepath.Join(topath, toname))
	if err == nil {
		return http.StatusOK, nil
	}
	if strings.HasSuffix(err.Error(), "directory not empty") {
		return http.StatusConflict, nil
	}
	return http.StatusInternalServerError, nil
}

// DeleteOneFile deletes from disk.
//
// Returns 200 (StatusOK) if the file did not exist ex ante.
func (h *Handler) DeleteOneFile(scope string, config *ScopeConfiguration, fileName string) (int, error) {
	path, fname, err := h.translateForFilesystem(scope, fileName, config)
	if err != nil {
		return 422, os.ErrPermission // 422: unprocessable entity
	}

	// no "os.Stat(); os.IsExist()" here: we don't check for 412 (Precondition Failed)

	err = os.RemoveAll(filepath.Join(path, fname))
	switch err {
	case nil:
		return http.StatusOK, nil
	case os.ErrPermission:
		return http.StatusForbidden, nil
	}
	return http.StatusInternalServerError, nil
}

// WriteOneHTTPBlob adapts WriteFileFromReader to HTTP conventions
// by translating input and output values.
func (h *Handler) WriteOneHTTPBlob(scope string, config *ScopeConfiguration, fileName,
	anticipatedSize string, r io.Reader) (uint64, int, error) {
	expectBytes, _ := strconv.ParseUint(anticipatedSize, 10, 64)
	if anticipatedSize != "" && expectBytes <= 0 { // we cannot work with that
		return 0, http.StatusLengthRequired, nil // 411: length required
		// Usually 411 is used for the outermost element.
		// We don't require any length; but if the key exists, the value must be valid.
	}

	path, fname, err := h.translateForFilesystem(scope, fileName, config)
	if err != nil {
		return 0, 422, os.ErrPermission // 422: unprocessable entity
	}

	bytesWritten, err := WriteFileFromReader(path, fname, r, expectBytes, noopUploadProgressCallback)
	if err != nil {
		if os.IsExist(err) || // gets thrown on a double race condition when using O_TMPFILE and linkat
			strings.HasSuffix(err.Error(), "not a directory") {
			return 0, http.StatusConflict, ErrFileNameConflict // 409
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

func noopUploadProgressCallback(bytesWritten uint64, err error) {
	// I want to become a closure that updates a data structure.
}

// WriteFileFromReader implements an unit of work consisting of
// • creation of a temporary file,
// • writing to it,
// • discarding it on failure ('zap') or
// • its "emergence" ('persist') into observable namespace.
//
// If 'anticipatedSize' ≥ protofile.reserveFileSizeThreshold (usually 32 KiB)
// then disk space will be reserved before writing (by a ProtoFileBehaver).
//
// uploadProgressCallback is called every so often with the number of bytes
// read for the file, and any errors that might have occured.
// "error" remaining 'io.EOF' after all bytes have been read indicates success.
func WriteFileFromReader(path, filename string, r io.Reader, anticipatedSize uint64,
	uploadProgressCallback func(uint64, error)) (uint64, error) {
	wp, err := protofile.IntentNew(path, filename)
	if err != nil {
		return 0, err
	}
	w := *wp
	defer w.Zap()

	err = w.SizeWillBe(anticipatedSize)
	if err != nil {
		return 0, err
	}

	var bytesWritten uint64
	var n int64
	for err == nil {
		n, err = io.CopyN(w, r, reportProgressEveryBytes)
		if err == nil || err == io.EOF {
			bytesWritten += uint64(n)
			uploadProgressCallback(bytesWritten, err)
		}
	}

	if err != nil && err != io.EOF {
		return bytesWritten, err
	}
	err = w.Persist()
	if err != nil {
		uploadProgressCallback(bytesWritten, err)
	}
	return bytesWritten, err
}
