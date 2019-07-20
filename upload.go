// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package upload // import "blitznote.com/src/http.upload"

import (
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"blitznote.com/src/http.upload/signature.auth"
	"blitznote.com/src/protofile"
	"github.com/pkg/errors"
	"golang.org/x/text/unicode/norm"
)

const (
	// Every so many bytes the |uploadProgressCallback| is called,
	// and checked if a file receives more bytes than any quota permits.
	// For one, this is cheaper than doing it for every byte,
	// and second, future versions will switch to blockwise writing, which is
	// more efficient and necessary for some high-speed NICs (think: 10GbE+).
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
	errWebdavDisabled          coreUploadError = "WebDAV has been disabled"
	errFileTooLarge            coreUploadError = "The uploaded file exceeds or would exceed max_filesize"
	errTransactionTooLarge     coreUploadError = "Upload(s) do or will exceed max_transaction_size"
)

// coreUploadError is returned for errors that are not in a leaf method,
// that have no specialized error
type coreUploadError string

// Error implements the error interface.
func (e coreUploadError) Error() string { return string(e) }

// getTimestamp returns the current time as unix timestamp.
//
// Do not inline this one: Mark overwrites it for his flavour of Go.
var getTimestamp = func(r *http.Request) uint64 {
	t := time.Now().Unix()
	return uint64(t)
}

// ServeHTTP catches methods meant for file manipulation, else is a passthrough.
// Directs HTTP methods and fields to the corresponding function calls.
func (h *Handler) serveHTTP(w http.ResponseWriter, r *http.Request,
	scope string,
	config *ScopeConfiguration,
	nextFn func(http.ResponseWriter, *http.Request) (int, error),
) (int, error) {

	switch r.Method {
	case http.MethodPost, http.MethodPut:
		// nop; always permitted
	default:
		if config.EnableWebdav { // also allow any other methods
			break
		}

		if config.SilenceAuthErrors {
			return nextFn(w, r)
		}
		return http.StatusMethodNotAllowed, errWebdavDisabled
	}

	config.IncomingHmacSecretsLock.RLock()
	if len(config.IncomingHmacSecrets) > 0 {
		if err := auth.Authenticate(r.Header, config.IncomingHmacSecrets, getTimestamp(r), config.TimestampTolerance); err != nil {
			config.IncomingHmacSecretsLock.RUnlock()

			if config.SilenceAuthErrors {
				log.Printf("[WARNING] upload/auth: Request not authorized: %v", err) // Caddy has no proper logging atm
				return nextFn(w, r)
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
		return h.moveOneFile(scope, config, r.URL.Path, destName)
	case "DELETE":
		if len(r.URL.Path) < 2 {
			return http.StatusBadRequest, errNoDestination
		}
		return h.deleteOneFile(scope, config, r.URL.Path)
	case http.MethodPost:
		ctype := r.Header.Get("Content-Type")
		switch {
		case strings.HasPrefix(ctype, "multipart/form-data"):
			return h.serveMultipartUpload(w, r, scope, config)
		case ctype != "": // other envelope formats, not implemented
			return http.StatusUnsupportedMediaType, errUnknownEnvelopeFormat
		}
		fallthrough
	case http.MethodPut:
		if len(r.URL.Path) < 2 {
			return http.StatusBadRequest, errNoDestination
		}

		writeQuota, overQuotaErr := config.MaxTransactionSize, errTransactionTooLarge
		if writeQuota == 0 || (config.MaxFilesize > 0 && config.MaxFilesize < writeQuota) {
			writeQuota, overQuotaErr = config.MaxFilesize, errFileTooLarge
		}

		var expectBytes uint64
		if r.Header.Get("Content-Length") != "" { // unfortunately, sending this header is optional
			var perr error
			expectBytes, perr = strconv.ParseUint(r.Header.Get("Content-Length"), 10, 64)
			if perr != nil || expectBytes < 0 {
				return http.StatusBadRequest, errLengthInvalid
			}
			if writeQuota > 0 && expectBytes > writeQuota { // XXX(mark): skip this check if sparse files are allowed
				return http.StatusRequestEntityTooLarge, overQuotaErr // http.PayloadTooLarge
			}
		}

		bytesWritten, locationOnDisk, retval, err := h.writeOneHTTPBlob(scope, config, r.URL.Path, expectBytes, writeQuota, r.Body)
		if writeQuota > 0 && bytesWritten > writeQuota {
			// The partially uploaded file gets discarded by writeOneHTTPBlob.
			return http.StatusRequestEntityTooLarge, overQuotaErr
		}

		if err == nil && config.ApparentLocation != "" {
			newApparentLocation := strings.Replace(locationOnDisk, config.WriteToPath, config.ApparentLocation, 1)
			if strings.HasPrefix(newApparentLocation, "//") {
				w.Header().Set("Location", newApparentLocation[1:])
			} else {
				w.Header().Set("Location", newApparentLocation)
			}
		}
		return retval, err
	default:
		return nextFn(w, r)
	}
}

// serveMultipartUpload is used on HTTP POST to explode a MIME Multipart envelope
// into one or more supplied files.
func (h *Handler) serveMultipartUpload(w http.ResponseWriter, r *http.Request,
	scope string, config *ScopeConfiguration) (int, error) {
	mr, err := r.MultipartReader()
	if err != nil {
		return http.StatusUnsupportedMediaType, errCannotReadMIMEMultipart
	}

	var bytesWrittenInTransaction uint64

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

		writeQuota, overQuotaErr := config.MaxFilesize, errFileTooLarge
		if config.MaxTransactionSize > 0 {
			if bytesWrittenInTransaction >= config.MaxTransactionSize {
				return http.StatusRequestEntityTooLarge, errTransactionTooLarge
			}
			if writeQuota == 0 || (config.MaxTransactionSize-bytesWrittenInTransaction) < writeQuota {
				writeQuota, overQuotaErr = config.MaxTransactionSize-bytesWrittenInTransaction, errTransactionTooLarge
			}
		}

		var expectBytes uint64
		if part.Header.Get("Content-Length") != "" {
			expectBytes, err = strconv.ParseUint(part.Header.Get("Content-Length"), 10, 64)
			if err != nil || expectBytes < 0 {
				return http.StatusBadRequest, errLengthInvalid
			}
			if writeQuota > 0 && expectBytes > writeQuota { // XXX(mark): sparse files would need this
				return http.StatusRequestEntityTooLarge, overQuotaErr
			}
		}

		bytesWritten, locationOnDisk, retval, err := h.writeOneHTTPBlob(scope, config, fileName, expectBytes, writeQuota, part)
		bytesWrittenInTransaction += bytesWritten
		if writeQuota > 0 && bytesWritten > writeQuota {
			return http.StatusRequestEntityTooLarge, overQuotaErr
		}
		if err != nil {
			// Don't use the fileName here: it is controlled by the user.
			return retval, errors.Wrap(err, "MIME Multipart exploding failed on part "+strconv.Itoa(partNum))
		}

		if config.ApparentLocation != "" {
			newApparentLocation := strings.Replace(locationOnDisk, config.WriteToPath, config.ApparentLocation, 1)
			if strings.HasPrefix(newApparentLocation, "//") {
				w.Header().Add("Location", newApparentLocation[1:])
			} else {
				w.Header().Add("Location", newApparentLocation)
			}
			// Yes, we send this even though the next part might throw an error.
		}
	}

	return http.StatusCreated, nil
}

// Translates the 'scope' into a proper directory, and extracts the filename from the resulting string.
func (h *Handler) translateForFilesystem(scope, providedName string, config *ScopeConfiguration) (fsPath, fsFilename string, err error) {
	// 'uc' is freely controlled by the uploader
	uc := strings.TrimPrefix(providedName, scope) // "/upload/mine/my.blob" → "/mine/my.blob"
	s := filepath.Join(config.WriteToPath, uc)    // → "/var/mine/my.blob"

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

// moveOneFile corresponds to HTTP method MOVE, and renames a file or path.
//
// The destination filename is parsed as if it were an URL.Path.
func (h *Handler) moveOneFile(scope string, config *ScopeConfiguration,
	fromFilename, toFilename string) (int, error) {
	frompath, fromname, err := h.translateForFilesystem(scope, fromFilename, config)
	if err != nil {
		return http.StatusUnprocessableEntity, errors.Wrap(err, "Invalid source filepath")
	}
	moveFrom := filepath.Join(frompath, fromname)
	topath, toname, err := h.translateForFilesystem(scope, toFilename, config)
	if err != nil {
		return http.StatusUnprocessableEntity, errors.Wrap(err, "Invalid destination filepath")
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

// deleteOneFile deletes from disk like "rm -r" and is used with HTTP DELETE.
// The term 'file' includes directories.
//
// Returns 204 (StatusNoContent) if the file did not exist ex ante.
func (h *Handler) deleteOneFile(scope string, config *ScopeConfiguration, fileName string) (int, error) {
	path, fname, err := h.translateForFilesystem(scope, fileName, config)
	if err != nil {
		return http.StatusUnprocessableEntity, err // 422: unprocessable entity
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

// writeOneHTTPBlob handles HTTP PUT (and HTTP POST without envelopes),
// writes one file to disk by adapting writeFileFromReader to HTTP conventions.
//
// Returns |bytesWritten|, |locationOnDisk|, |suggestHTTPResponseCode|, error.
func (h *Handler) writeOneHTTPBlob(scope string, config *ScopeConfiguration, fileName string,
	expectBytes, writeQuota uint64, r io.Reader) (uint64, string, int, error) {
	path, fname, err := h.translateForFilesystem(scope, fileName, config)
	if err != nil {
		return 0, "", http.StatusUnprocessableEntity, err // 422: unprocessable entity
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
	bytesWritten, err := writeFileFromReader(path, fname, r, expectBytes, writeQuota, callback)
	locationOnDisk := filepath.Join(path, fname)
	if err != nil && err != io.EOF {
		if os.IsExist(err) || // gets thrown on a double race condition when using O_TMPFILE and linkat
			strings.HasSuffix(err.Error(), "not a directory") {
			return 0, locationOnDisk, http.StatusConflict, errFileNameConflict // 409
		}
		if bytesWritten > 0 && bytesWritten < expectBytes {
			return bytesWritten, locationOnDisk, http.StatusInsufficientStorage, err // 507: insufficient storage
		}
		return bytesWritten, locationOnDisk, http.StatusInternalServerError, err
	}
	if bytesWritten < expectBytes {
		// We don't return 422 on incomplete uploads, because it could be a sparse file.
		// Its contents are kept in any case, therefore returning an error would be inappropriate.
		return bytesWritten, locationOnDisk, http.StatusAccepted, nil // 202: accepted (but not completed)
	}
	return bytesWritten, locationOnDisk, http.StatusCreated, nil // 201: Created
}

// writeFileFromReader implements an unit of work consisting of
// • creation of a temporary file,
// • writing to it,
// • discarding it on failure ('zap') or
// • its "emergence" ('persist') into observable namespace.
//
// If 'anticipatedSize' ≥ protofile.reserveFileSizeThreshold (usually 32 KiB)
// then disk space will be reserved before writing (by a ProtoFileBehaver).
// If the bytes to be written exceed |writeQuota| then the
// partially or completely written file is discarded.
//
// With uploadProgressCallback:
// The file has been successfully written if "error" remains 'io.EOF'.
func writeFileFromReader(path, filename string, r io.Reader, anticipatedSize, writeQuota uint64,
	uploadProgressCallback func(uint64, error)) (uint64, error) {
	wp, err := protofile.IntentNew(path, filename)
	if err != nil {
		return 0, err
	}
	w := *wp
	defer w.Zap()

	err = w.SizeWillBe(anticipatedSize) // if > writeQuota then this could be a sparse file
	if err != nil {
		return 0, err
	}

	var bytesWritten uint64
	var n int64
	for err == nil {
		if writeQuota > 0 && bytesWritten > writeQuota {
			break
		}
		n, err = io.CopyN(w, r, reportProgressEveryBytes)
		if err == nil || err == io.EOF {
			bytesWritten += uint64(n)
			uploadProgressCallback(bytesWritten, err)
		}
	}

	if err != nil && err != io.EOF {
		return bytesWritten, err
	}
	if writeQuota > 0 && bytesWritten > writeQuota {
		// The file will be removed automatically due to the deferred w.Zap().
		// XXX(mark): on err == io.EOF and a later blockwise writing, don't return here but keep the file instead.
		uploadProgressCallback(bytesWritten, errFileTooLarge)
		return bytesWritten, errFileTooLarge
	}

	err = w.Persist()
	if err != nil {
		uploadProgressCallback(bytesWritten, err)
	}
	return bytesWritten, err
}
