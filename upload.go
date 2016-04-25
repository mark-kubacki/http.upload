package upload // import "blitznote.com/src/caddy.upload"

import (
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"blitznote.com/src/caddy.upload/protofile"
	"blitznote.com/src/caddy.upload/signature.auth"
	"github.com/mholt/caddy/middleware"
	"golang.org/x/text/unicode/norm"
)

const (
	// at this point this is an arbitrary number
	reportProgressEveryBytes = 1 << 15
)

// Errors used in functions that resemble the core logic of this plugin.
var (
	errCannotReadMIMEMultipart = errors.New("Error reading MIME multipart payload")
	errFileNameConflict        = errors.New("Name-Name Conflict")
	errInvalidFileName         = errors.New("Invalid filename and/or path")
)

// Handler represents a configured instance of this plugin for uploads.
//
// If you want to use it outside of Caddy, then implement 'Next' as
// something with method ServeHTTP and at least the same member variables
// that you can find here.
type Handler struct {
	Next   middleware.Handler
	Config HandlerConfiguration
}

// getTimestamp returns the current time as unix timestamp.
//
// Do not inline this one: Mark overwrites it for his flavour of Go.
var getTimestamp = func(r *http.Request) uint64 {
	t := time.Now().Unix()
	return uint64(t)
}

// ServeHTTP catches methods if meant for file manipulation, else is a passthrough.
// Directs HTTP methods and fields to the corresponding function calls.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) (int, error) {
	var (
		scope  string // a prefix we will need to replace with the target directory
		config *ScopeConfiguration
	)

	switch r.Method {
	case http.MethodPost, http.MethodPut, "COPY", "MOVE", "DELETE":
		// iterate over the scopes in the order they have been defined
		for _, scope = range h.Config.PathScopes {
			if middleware.Path(r.URL.Path).Matches(scope) {
				config = h.Config.Scope[scope]
				goto inScope
			}
		}
	}
	return h.Next.ServeHTTP(w, r)
inScope:

	config.IncomingHmacSecretsLock.RLock()
	if len(config.IncomingHmacSecrets) > 0 {
		if resp, err := auth.Authenticate(r.Header, config.IncomingHmacSecrets, getTimestamp(r), config.TimestampTolerance); err != nil {
			config.IncomingHmacSecretsLock.RUnlock()
			if config.SilenceAuthErrors {
				return h.Next.ServeHTTP(w, r)
			}
			if resp == 401 {
				// send this header to prevent the user from being asked for a username/password pair
				w.Header().Set("WWW-Authenticate", "Signature")
			}
			return resp, err
		}
	}
	config.IncomingHmacSecretsLock.RUnlock()

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
	case http.MethodPost:
		ctype := r.Header.Get("Content-Type")
		switch {
		case strings.HasPrefix(ctype, "multipart/form-data"):
			return h.ServeMultipartUpload(w, r, scope, config)
		case ctype != "": // other envelope formats, not implemented
			return http.StatusUnsupportedMediaType, nil // 415: unsupported media type
		}
		fallthrough
	case http.MethodPut:
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

// ServeMultipartUpload is used on HTTP POST to explode a MIME Multipart envelope
// into one or more supplied files. They are then supplied to WriteOneHTTPBlob one by one.
func (h *Handler) ServeMultipartUpload(w http.ResponseWriter, r *http.Request,
	scope string, config *ScopeConfiguration) (int, error) {
	mr, err := r.MultipartReader()
	if err != nil {
		return http.StatusUnsupportedMediaType, errCannotReadMIMEMultipart
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
		err = errInvalidFileName
		return
	}

	fsPath, fsFilename = filepath.Dir(translated), filepath.Base(translated)

	return
}

// MoveOneFile corresponds to HTTP method MOVE, and renames a file or path.
//
// The destination filename is parsed as if it were an URL.Path.
func (h *Handler) MoveOneFile(scope string, config *ScopeConfiguration,
	fromFilename, toFilename string) (int, error) {
	frompath, fromname, err := h.translateForFilesystem(scope, fromFilename, config)
	if err != nil {
		return 422, os.ErrPermission
	}
	moveFrom := filepath.Join(frompath, fromname)
	topath, toname, err := h.translateForFilesystem(scope, toFilename, config)
	if err != nil {
		return 422, os.ErrPermission
	}
	moveTo := filepath.Join(topath, toname)

	// Do not check for Unicode equivalence here:
	// The requestor might want to change forms!
	if moveFrom == moveTo {
		return http.StatusConflict, nil
	}
	if moveFrom == config.WriteToPath || moveTo == config.WriteToPath {
		return http.StatusForbidden, nil // refuse any tinkering with the scope's target directory
	}

	err = os.Rename(moveFrom, moveTo)
	if err == nil {
		return http.StatusCreated, nil // 201, but if something gets overwritten 204
	}
	if strings.HasSuffix(err.Error(), "directory not empty") {
		return http.StatusConflict, nil
	}
	return http.StatusInternalServerError, nil
}

// DeleteOneFile deletes from disk like "rm -r" and is used with HTTP DELETE.
// The term 'file' includes directories.
//
// Returns 200 (StatusOK) if the file did not exist ex ante.
func (h *Handler) DeleteOneFile(scope string, config *ScopeConfiguration, fileName string) (int, error) {
	path, fname, err := h.translateForFilesystem(scope, fileName, config)
	if err != nil {
		return 422, os.ErrPermission // 422: unprocessable entity
	}
	deleteThis := filepath.Join(path, fname)

	// no "os.Stat(); os.IsExist()" here: we don't check for 412 (Precondition Failed)

	if deleteThis == config.WriteToPath {
		return http.StatusForbidden, nil // refuse to delete the scope's target directory
	}

	err = os.RemoveAll(deleteThis)
	switch err {
	case nil:
		return http.StatusNoContent, nil // 204
	case os.ErrPermission:
		return http.StatusForbidden, nil
	}
	return http.StatusInternalServerError, nil
}

// WriteOneHTTPBlob handles HTTP PUT (and HTTP POST without envelopes),
// writes one file to disk by adapting WriteFileFromReader to HTTP conventions.
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

	callback := config.UploadProgressCallback
	if callback == nil {
		callback = noopUploadProgressCallback
	}
	bytesWritten, err := WriteFileFromReader(path, fname, r, expectBytes, callback)
	if err != nil {
		if os.IsExist(err) || // gets thrown on a double race condition when using O_TMPFILE and linkat
			strings.HasSuffix(err.Error(), "not a directory") {
			return 0, http.StatusConflict, errFileNameConflict // 409
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
//
// With uploadProgressCallback:
// The file has been successfully written if "error" remains 'io.EOF'.
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
