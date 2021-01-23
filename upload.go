// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package upload

import (
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"blitznote.com/src/protofile/v2"
	"github.com/pkg/errors"
	"golang.org/x/text/unicode/norm"
)

// Errors used in functions that resemble the core logic of this plugin.
const (
	errCannotReadMIMEMultipart coreUploadError = "Error reading MIME multipart payload"
	errFileNameConflict        coreUploadError = "Name-Name Conflict"
	errInvalidFileName         coreUploadError = "Invalid filename and/or path"
	errNoDestination           coreUploadError = "A destination is missing"
	errUnknownEnvelopeFormat   coreUploadError = "Unknown envelope format"
	errLengthInvalid           coreUploadError = "Field 'length' has been set, but is invalid"
	errFileTooLarge            coreUploadError = "The uploaded file exceeds or would exceed max_filesize"
	errTransactionTooLarge     coreUploadError = "Upload(s) do or will exceed max_transaction_size"
)

// coreUploadError is returned for errors that are not in a leaf method,
// that have no specialized error
type coreUploadError string

// Error implements the error interface.
func (e coreUploadError) Error() string { return string(e) }

// ServeHTTP catches methods meant for file manipulation.
// Anything else will be delegated to h.Next, if not nil.
func (h Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	httpCode, err := h.serveHTTP(w, r)

	if httpCode == http.StatusMethodNotAllowed && err == nil && h.Next != nil {
		h.Next.ServeHTTP(w, r)
		return
	}
	if httpCode >= 400 && err != nil {
		http.Error(w, err.Error(), httpCode)
	} else {
		w.WriteHeader(httpCode)
	}
}

func (h *Handler) serveHTTP(w http.ResponseWriter, r *http.Request) (int, error) {
	switch r.Method {
	case http.MethodPost, http.MethodPut:
		// nop; always permitted
	case "COPY", "MOVE", "DELETE":
		if h.EnableWebdav { // also allow any other methods
			break
		}
		fallthrough
	default:
		return http.StatusMethodNotAllowed, nil
	}

	switch r.Method {
	case "COPY":
		return http.StatusNotImplemented, nil
	case "MOVE":
		destName := r.Header.Get("Destination")
		if len(r.URL.Path) < 2 || destName == "" {
			return http.StatusBadRequest, errNoDestination
		}
		return h.moveOneFile(r.URL.Path, destName)
	case "DELETE":
		if len(r.URL.Path) < 2 {
			return http.StatusBadRequest, errNoDestination
		}
		return h.deleteOneFile(r.URL.Path)
	case http.MethodPost:
		ctype := r.Header.Get("Content-Type")
		switch {
		case strings.HasPrefix(ctype, "multipart/form-data"):
			return h.serveMultipartUpload(w, r)
		case ctype != "": // other envelope formats, not implemented
			return http.StatusUnsupportedMediaType, errUnknownEnvelopeFormat
		}
		fallthrough
	case http.MethodPut:
		if len(r.URL.Path) < 2 {
			return http.StatusBadRequest, errNoDestination
		}

		writeQuota, overQuotaErr := h.MaxTransactionSize, errTransactionTooLarge
		if writeQuota == 0 || (h.MaxFilesize > 0 && h.MaxFilesize < writeQuota) {
			writeQuota, overQuotaErr = h.MaxFilesize, errFileTooLarge
		}

		var expectBytes int64
		if r.Header.Get("Content-Length") != "" { // unfortunately, sending this header is optional
			var perr error
			expectBytes, perr = strconv.ParseInt(r.Header.Get("Content-Length"), 10, 64)
			if perr != nil || expectBytes < 0 {
				return http.StatusBadRequest, errLengthInvalid
			}
			if writeQuota > 0 && expectBytes > writeQuota { // XXX(mark): skip this check if sparse files are allowed
				return http.StatusRequestEntityTooLarge, overQuotaErr // http.PayloadTooLarge
			}
		}

		bytesWritten, locationOnDisk, retval, err := h.writeOneHTTPBlob(r.URL.Path, expectBytes, writeQuota, r.Body)
		if writeQuota > 0 && bytesWritten > writeQuota {
			// The partially uploaded file gets discarded by writeOneHTTPBlob.
			return http.StatusRequestEntityTooLarge, overQuotaErr
		}

		if err == nil && h.ApparentLocation != "" {
			newApparentLocation := strings.Replace(locationOnDisk, h.WriteToPath, h.ApparentLocation, 1)
			if strings.HasPrefix(newApparentLocation, "//") {
				w.Header().Set("Location", newApparentLocation[1:])
			} else {
				w.Header().Set("Location", newApparentLocation)
			}
		}
		return retval, err
	default:
		return http.StatusMethodNotAllowed, nil
	}
}

// serveMultipartUpload is used on HTTP POST to explode a MIME Multipart envelope
// into one or more supplied files.
func (h *Handler) serveMultipartUpload(w http.ResponseWriter, r *http.Request) (int, error) {
	mr, err := r.MultipartReader()
	if err != nil {
		return http.StatusUnsupportedMediaType, errCannotReadMIMEMultipart
	}

	var bytesWrittenInTransaction int64

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

		writeQuota, overQuotaErr := h.MaxFilesize, errFileTooLarge
		if h.MaxTransactionSize > 0 {
			if bytesWrittenInTransaction >= h.MaxTransactionSize {
				return http.StatusRequestEntityTooLarge, errTransactionTooLarge
			}
			if writeQuota == 0 || (h.MaxTransactionSize-bytesWrittenInTransaction) < writeQuota {
				writeQuota, overQuotaErr = h.MaxTransactionSize-bytesWrittenInTransaction, errTransactionTooLarge
			}
		}

		var expectBytes int64
		if part.Header.Get("Content-Length") != "" {
			expectBytes, err = strconv.ParseInt(part.Header.Get("Content-Length"), 10, 64)
			if err != nil || expectBytes < 0 {
				return http.StatusBadRequest, errLengthInvalid
			}
			if writeQuota > 0 && expectBytes > writeQuota { // XXX(mark): sparse files would need this
				return http.StatusRequestEntityTooLarge, overQuotaErr
			}
		}

		bytesWritten, locationOnDisk, retval, err := h.writeOneHTTPBlob(fileName, expectBytes, writeQuota, part)
		bytesWrittenInTransaction += bytesWritten
		if writeQuota > 0 && bytesWritten > writeQuota {
			return http.StatusRequestEntityTooLarge, overQuotaErr
		}
		if err != nil {
			// Don't use the fileName here: it is controlled by the user.
			return retval, errors.Wrap(err, "MIME Multipart exploding failed on part "+strconv.Itoa(partNum))
		}

		if h.ApparentLocation != "" {
			newApparentLocation := strings.Replace(locationOnDisk, h.WriteToPath, h.ApparentLocation, 1)
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
func (h *Handler) translateForFilesystem(providedName string) (fsPath, fsFilename string, err error) {
	// 'uc' is freely controlled by the uploader
	uc := strings.TrimPrefix(providedName, h.Scope) // "/upload/mine/my.blob" → "/mine/my.blob"
	s := filepath.Join(h.WriteToPath, uc)           // → "/var/mine/my.blob"

	// stop any childish path trickery here
	translated := filepath.Clean(s) // "/var/mine/../mine/my.blob" → "/var/mine/my.blob"
	if !strings.HasPrefix(translated, h.WriteToPath) {
		err = os.ErrPermission
		return
	}

	var enforceForm *norm.Form
	if h.UnicodeForm != nil {
		enforceForm = &h.UnicodeForm.Use
	}
	if !InAlphabet(uc, h.RestrictFilenamesTo, enforceForm) {
		err = errInvalidFileName
		return
	}

	fsPath, fsFilename = filepath.Dir(translated), filepath.Base(translated)

	return
}

// moveOneFile corresponds to HTTP method MOVE, and renames a file or path.
//
// The destination filename is parsed as if it were an URL.Path.
func (h *Handler) moveOneFile(fromFilename, toFilename string) (int, error) {
	frompath, fromname, err := h.translateForFilesystem(fromFilename)
	if err != nil {
		return http.StatusUnprocessableEntity, errors.Wrap(err, "Invalid source filepath")
	}
	moveFrom := filepath.Join(frompath, fromname)
	topath, toname, err := h.translateForFilesystem(toFilename)
	if err != nil {
		return http.StatusUnprocessableEntity, errors.Wrap(err, "Invalid destination filepath")
	}
	moveTo := filepath.Join(topath, toname)

	// Do not check for Unicode equivalence here:
	// The requestor might want to change forms!
	if moveFrom == moveTo {
		return http.StatusConflict, nil
	}
	if moveFrom == h.WriteToPath || moveTo == h.WriteToPath {
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
func (h *Handler) deleteOneFile(fileName string) (int, error) {
	path, fname, err := h.translateForFilesystem(fileName)
	if err != nil {
		return http.StatusUnprocessableEntity, err // 422: unprocessable entity
	}
	deleteThis := filepath.Join(path, fname)

	// no "os.Stat(); os.IsExist()" here: we don't check for 412 (Precondition Failed)

	if deleteThis == h.WriteToPath {
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
func (h *Handler) writeOneHTTPBlob(fileName string,
	expectBytes, writeQuota int64, r io.Reader) (int64, string, int, error) {
	path, fname, err := h.translateForFilesystem(fileName)
	if err != nil {
		return 0, "", http.StatusUnprocessableEntity, err // 422: unprocessable entity
	}

	if h.RandomizedSuffixLength > 0 {
		extension := filepath.Ext(fname)
		basename := strings.TrimSuffix(fname, extension)
		if basename == "" {
			fname = printableSuffix(h.RandomizedSuffixLength) + extension
		} else {
			fname = basename + "_" + printableSuffix(h.RandomizedSuffixLength) + extension
		}
	}

	bytesWritten, err := writeFileFromReader(path, fname, r, expectBytes, writeQuota)
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
// If the bytes to be written exceed |writeQuota| then the
// partially or completely written file is discarded.
func writeFileFromReader(path, filename string, r io.Reader, anticipatedSize, writeQuota int64) (int64, error) {
	err := os.MkdirAll(path, os.FileMode(0755))
	if err != nil {
		return 0, err
	}
	w, err := protofile.CreateTemp(path) // Wraps ioutil.TempFile on Windows.
	if protofile.IsTempfileNotSupported(err) {
		w, err = ioutil.TempFile(path, ".*")
		defer os.Remove(w.Name())
	}
	if err != nil {
		return 0, err
	}
	if runtime.GOOS == "windows" {
		defer os.Remove(w.Name())
	}
	defer w.Close()

	if anticipatedSize > 4096 {
		w.Truncate(anticipatedSize)
	}

	var bytesWritten int64
	if writeQuota > 0 {
		bytesWritten, err = io.CopyN(w, r, 1+int64(writeQuota-bytesWritten))
	} else {
		bytesWritten, err = io.Copy(w, r)
	}

	if err != nil && err != io.EOF {
		return bytesWritten, err
	}
	if writeQuota > 0 && bytesWritten > writeQuota {
		return bytesWritten, errFileTooLarge
	}

	finalName := filepath.Join(path, filename)
	if runtime.GOOS == "windows" {
		err = w.Close()
		if err != nil {
			return bytesWritten, err
		}
		err = os.Rename(w.Name(), finalName)
		if os.IsExist(err) {
			if finfo, _ := os.Stat(finalName); !finfo.IsDir() {
				os.Remove(finalName)
				err = os.Rename(w.Name(), finalName)
			}
		}
		return bytesWritten, err
	}
	err = protofile.Hardlink(w, finalName)
	if os.IsExist(err) {
		if finfo, _ := os.Stat(finalName); !finfo.IsDir() {
			os.Remove(finalName)
			err = protofile.Hardlink(w, finalName)
		}
	}
	if err != nil {
		w.Close()
		return bytesWritten, err
	}
	return bytesWritten, w.Close()
}
