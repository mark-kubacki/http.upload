package upload // import "hub.blitznote.com/src/caddy.upload"

import (
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"hub.blitznote.com/src/caddy.upload/protofile"
	"hub.blitznote.com/src/caddy.upload/signature.auth"
	"github.com/mholt/caddy/caddyhttp/httpserver"
	"github.com/pkg/errors"
	"golang.org/x/text/unicode/norm"
)

const (
	// at this point this is an arbitrary number
	reportProgressEveryBytes = 1 << 15
)

// Errors used in functions that resemble the core logic of this plugin.
const (
	errCannotReadMIMEMultipart coreUploadError = "Error reading MIME multipart payload"
	errFileNameConflict        coreUploadError = "Name-Name Conflict"
	errInvalidFileName         coreUploadError = "Invalid filename and/or path"
	errNoDestination           coreUploadError = "A destination is missing"
	errUnknownEnvelopeFormat   coreUploadError = "Unknown envelope format"
	errLengthInvalid           coreUploadError = "Field 'length' has been set, but is invalid"
	errWebdavDisabled          coreUploadError = "WebDAV had been disabled"
)

// coreUploadError is returned for errors that are not in a leaf method,
// that have no specialized error
type coreUploadError string

// Error implements the error interface.
func (e coreUploadError) Error() string { return string(e) }

// Handler represents a configured instance of this plugin for uploads.
//
// If you want to use it outside of Caddy, then implement 'Next' as
// something with method ServeHTTP and at least the same member variables
// that you can find here.
type Handler struct {
	Next   httpserver.Handler
	Config HandlerConfiguration
}

// getTimestamp returns the current time as unix timestamp.
//
// Do not inline this one: Mark overwrites it for his flavour of Go.
var getTimestamp = func(r *http.Request) uint64 {
	t := time.Now().Unix()
	return uint64(t)
}

// ServeHTTP catches methods meant for file manipulation, else is a passthrough.
// Directs HTTP methods and fields to the corresponding function calls.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) (int, error) {
	var (
		scope  string // will be stripped later
		config *ScopeConfiguration
	)

	switch r.Method {
	case http.MethodPost, http.MethodPut, "COPY", "MOVE", "DELETE":
		// iterate over the scopes in the order they have been defined
		for _, scope = range h.Config.PathScopes {
			if httpserver.Path(r.URL.Path).Matches(scope) {
				config = h.Config.Scope[scope]
				goto inScope
			}
		}
	}
	return h.Next.ServeHTTP(w, r)
inScope:

	if config.DisableWebdav {
		switch r.Method {
		case "COPY", "MOVE", "DELETE":
			if config.SilenceAuthErrors {
				return h.Next.ServeHTTP(w, r)
			}
			return http.StatusMethodNotAllowed, errWebdavDisabled
		}
	}

	config.IncomingHmacSecretsLock.RLock()
	if len(config.IncomingHmacSecrets) > 0 {
		if err := auth.Authenticate(r.Header, config.IncomingHmacSecrets, getTimestamp(r), config.TimestampTolerance); err != nil {
			config.IncomingHmacSecretsLock.RUnlock()

			if config.SilenceAuthErrors {
				log.Printf("[WARNING] upload/auth: Request not authorized: %v", err) // Caddy has no proper logging atm
				return h.Next.ServeHTTP(w, r)
			}
			resp := err.SuggestedResponseCode()
			if resp == http.StatusUnauthorized {
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
			return http.StatusBadRequest, errNoDestination
		}
		return h.MoveOneFile(scope, config, r.URL.Path, destName)
	case "DELETE":
		if len(r.URL.Path) < 2 {
			return http.StatusBadRequest, errNoDestination
		}
		return h.DeleteOneFile(scope, config, r.URL.Path)
	case http.MethodPost:
		ctype := r.Header.Get("Content-Type")
		switch {
		case strings.HasPrefix(ctype, "multipart/form-data"):
			return h.ServeMultipartUpload(w, r, scope, config)
		case ctype != "": // other envelope formats, not implemented
			return http.StatusUnsupportedMediaType, errUnknownEnvelopeFormat
		}
		fallthrough
	case http.MethodPut:
		if len(r.URL.Path) < 2 {
			return http.StatusBadRequest, errNoDestination
		}
		_, retval, err := h.WriteOneHTTPBlob(scope, config, r.URL.Path,
			r.Header.Get("Content-Length"), r.Body)
		return retval, err
	default:
		return h.Next.ServeHTTP(w, r)
	}
}

// ServeMultipartUpload is used on HTTP POST to explode a MIME Multipart envelope
// into one or more supplied files.
func (h *Handler) ServeMultipartUpload(w http.ResponseWriter, r *http.Request,
	scope string, config *ScopeConfiguration) (int, error) {
	mr, err := r.MultipartReader()
	if err != nil {
		return http.StatusUnsupportedMediaType, errCannotReadMIMEMultipart
	}

	for partNum := 1; ; partNum++ {
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
			// Don't use the fileName here: it is controlled by the user.
			return retval, errors.Wrap(err, "MIME Multipart exploding failed on part "+strconv.Itoa(partNum))
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
		return 422, errors.Wrap(err, "Invalid source filepath")
	}
	moveFrom := filepath.Join(frompath, fromname)
	topath, toname, err := h.translateForFilesystem(scope, toFilename, config)
	if err != nil {
		return 422, errors.Wrap(err, "Invalid destination filepath")
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
	return http.StatusInternalServerError, errors.Wrap(err, "MOVE failed")
}

// DeleteOneFile deletes from disk like "rm -r" and is used with HTTP DELETE.
// The term 'file' includes directories.
//
// Returns 200 (StatusOK) if the file did not exist ex ante.
func (h *Handler) DeleteOneFile(scope string, config *ScopeConfiguration, fileName string) (int, error) {
	path, fname, err := h.translateForFilesystem(scope, fileName, config)
	if err != nil {
		return 422, err // 422: unprocessable entity
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
		return http.StatusForbidden, errors.Wrap(err, "DELETE failed")
	}
	return http.StatusInternalServerError, errors.Wrap(err, "DELETE failed")
}

// WriteOneHTTPBlob handles HTTP PUT (and HTTP POST without envelopes),
// writes one file to disk by adapting WriteFileFromReader to HTTP conventions.
func (h *Handler) WriteOneHTTPBlob(scope string, config *ScopeConfiguration, fileName,
	anticipatedSize string, r io.Reader) (uint64, int, error) {
	expectBytes, _ := strconv.ParseUint(anticipatedSize, 10, 64)
	if anticipatedSize != "" && expectBytes <= 0 {
		return 0, http.StatusLengthRequired, errLengthInvalid
		// Usually 411 is used for the outermost element.
		// We don't require any length; but it must be valid if given.
	}

	path, fname, err := h.translateForFilesystem(scope, fileName, config)
	if err != nil {
		return 0, 422, err // 422: unprocessable entity
	}

	if config.RandomizedSuffixLength > 0 {
		extension := filepath.Ext(fname)
		basename := strings.TrimSuffix(fname, extension)
		if basename == "" {
			fname = printableSuffix(config.RandomizedSuffixLength) + extension
		} else {
			fname = basename + "_" + printableSuffix(config.RandomizedSuffixLength) + extension
		}
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
