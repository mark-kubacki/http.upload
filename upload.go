// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package upload

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

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
		destName := r.Header.Get("Destination")
		if len(r.URL.Path) < 2 || destName == "" {
			return http.StatusBadRequest, errNoDestination
		}
		return h.copy(r.Context(), destName, r.URL.Path, false)
	case "MOVE":
		destName := r.Header.Get("Destination")
		if len(r.URL.Path) < 2 || destName == "" {
			return http.StatusBadRequest, errNoDestination
		}
		return h.copy(r.Context(), destName, r.URL.Path, true)
	case "DELETE":
		if len(r.URL.Path) < 2 {
			return http.StatusBadRequest, errNoDestination
		}
		return h.deleteOneFile(r.Context(), r.URL.Path)
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
		return h.serveOneUpload(w, r)
	default:
		return http.StatusMethodNotAllowed, nil
	}
}

// serveOneUpload usually is used with HTTP PUT, and writes one file.
func (h *Handler) serveOneUpload(w http.ResponseWriter, r *http.Request) (int, error) {
	if len(r.URL.Path) < 2 {
		return http.StatusBadRequest, errNoDestination
	}

	// Select the limiter, transaction- or file size.
	writeQuota, overQuotaErr := h.MaxTransactionSize, errTransactionTooLarge
	if writeQuota == 0 || (h.MaxFilesize > 0 && h.MaxFilesize < writeQuota) {
		writeQuota, overQuotaErr = h.MaxFilesize, errFileTooLarge
	}

	var expectBytes int64
	if r.Header.Get("Content-Length") != "" { // An optional header.
		var perr error
		expectBytes, perr = strconv.ParseInt(r.Header.Get("Content-Length"), 10, 64)
		if perr != nil || expectBytes < 0 {
			return http.StatusBadRequest, errLengthInvalid
		}
		if writeQuota > 0 && expectBytes > writeQuota {
			return http.StatusRequestEntityTooLarge, overQuotaErr // http.PayloadTooLarge
		}
	}

	bytesWritten, key, retval, err := h.writeOneHTTPBlob(r.Context(), r.URL.Path, expectBytes, writeQuota, r.Body)
	if writeQuota > 0 && bytesWritten > writeQuota {
		// The partially uploaded file gets discarded by writeOneHTTPBlob.
		return http.StatusRequestEntityTooLarge, overQuotaErr
	}

	if err == nil && h.ApparentLocation != "" {
		newApparentLocation := "/" + key
		if h.ApparentLocation != "/" {
			newApparentLocation = h.ApparentLocation + newApparentLocation
		}
		w.Header().Add("Location", newApparentLocation)
	}
	return retval, err
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
		// Part names are relative, and need the target directory still.
		if h.Scope == "/" {
			fileName = h.Scope + fileName
		} else {
			fileName = h.Scope + "/" + fileName
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
			if writeQuota > 0 && expectBytes > writeQuota {
				return http.StatusRequestEntityTooLarge, overQuotaErr
			}
		}

		bytesWritten, key, retval, err := h.writeOneHTTPBlob(r.Context(), fileName, expectBytes, writeQuota, part)
		bytesWrittenInTransaction += bytesWritten
		if writeQuota > 0 && bytesWritten > writeQuota {
			return http.StatusRequestEntityTooLarge, overQuotaErr
		}
		if err != nil {
			// Don't use the fileName here: it is controlled by the user.
			return retval, errors.Wrap(err, "MIME Multipart exploding failed on part "+strconv.Itoa(partNum))
		}

		if h.ApparentLocation != "" {
			newApparentLocation := "/" + key
			if h.ApparentLocation != "/" {
				newApparentLocation = h.ApparentLocation + newApparentLocation
			}
			w.Header().Add("Location", newApparentLocation)
			// Yes, we send this even though the next part might throw an error.
		}
	}

	return http.StatusCreated, nil
}

// translateToKey derives a key suitable for use with Storage Buckets.
func (h *Handler) translateToKey(path string) (key string, err error) {
	if path == h.Scope {
		return "", os.ErrPermission
	}
	canary := "/" + printableSuffix(15)
	key = filepath.Clean(canary + path) // "/var/mine/../mine/my.blob" → "/var/mine/my.blob"
	if !strings.HasPrefix(key, canary+h.Scope) {
		err = os.ErrPermission
		return
	}
	if h.Scope == "/" {
		key = key[len(canary)+1:]
	} else {
		key = key[len(canary)+len(h.Scope)+1:] // "/upload/mine/my.blob" → "/mine/my.blob"
	}

	var enforceForm *norm.Form
	if h.UnicodeForm != nil {
		enforceForm = &h.UnicodeForm.Use
	}
	if !InAlphabet(key, h.RestrictFilenamesTo, enforceForm) {
		err = errInvalidFileName
	}
	return
}

func (h *Handler) applyRandomizedSuffix(key string) string {
	if h.RandomizedSuffixLength <= 0 {
		return key
	}
	extension := filepath.Ext(key)
	basename := strings.TrimSuffix(key, extension)
	if basename == "" || strings.HasSuffix(basename, "/") {
		key = basename + printableSuffix(h.RandomizedSuffixLength) + extension
	} else {
		key = basename + "_" + printableSuffix(h.RandomizedSuffixLength) + extension
	}
	return key
}

// copy is meant to respond to HTTP COPY by duplicating a file,
// and MOVE if deleteSource is true.
//
// The destination filename is parsed as if it were an URL.Path.
func (h *Handler) copy(ctx context.Context, newPath, oldPath string, deleteSource bool) (int, error) {
	srcKey, err := h.translateToKey(oldPath)
	if err != nil {
		return http.StatusUnprocessableEntity, errors.Wrap(err, "Invalid source filepath")
	}
	dstKey, err := h.translateToKey(newPath)
	if err != nil {
		return http.StatusUnprocessableEntity, errors.Wrap(err, "Invalid destination filepath")
	}

	// Do not check for Unicode equivalence here:
	// The requestor might want to change forms!
	if srcKey == dstKey {
		return http.StatusForbidden, nil
	}

	if err := h.Bucket.Copy(ctx, dstKey, srcKey, nil); err != nil {
		// Because gcerr is an internal package.
		gcerr, _ := err.(interface{ Unwrap() error })
		// Both are thrown by a traditional (non-flat) file system, either
		// if the path is a directory (cannot contain any stream at rest)
		// or if part of a directory-to-be-created already is a file.
		switch e := gcerr.Unwrap().(type) {
		case *os.LinkError, *os.PathError:
			return http.StatusConflict, e
		default:
			return http.StatusInternalServerError, errors.Wrap(err, "COPY failed")
		}
	}
	if !deleteSource {
		return http.StatusCreated, nil // 201, but if something gets overwritten 204
	}
	if err := h.Bucket.Delete(ctx, srcKey); err != nil {
		return http.StatusInternalServerError, errors.Wrap(err, "MOVE failed")
	}
	return http.StatusCreated, nil // 201, but if something gets overwritten 204
}

// deleteOneFile deletes from disk like "rm -r" and is used with HTTP DELETE.
// The term 'file' includes directories.
//
// Returns 204 (StatusNoContent) if the file did not exist ex ante.
func (h *Handler) deleteOneFile(ctx context.Context, path string) (int, error) {
	key, err := h.translateToKey(path)
	if err != nil && err != os.ErrPermission {
		return http.StatusUnprocessableEntity, err // 422: unprocessable entity
	}
	if key == "" || key == "/" {
		return http.StatusForbidden, errors.Wrap(err, "DELETE has tried removing the parent directory")
	}

	err = h.Bucket.Delete(ctx, key)
	switch err {
	case nil:
		return http.StatusNoContent, nil // 204
	case os.ErrPermission:
		return http.StatusForbidden, errors.Wrap(err, "DELETE failed")
	}
	return http.StatusInternalServerError, errors.Wrap(err, "DELETE failed")
}

// writeOneHTTPBlob handles HTTP PUT (and HTTP POST without envelopes),
// writes one file to disk.
//
// Returns |bytesWritten|, |locationOnDisk|, |suggestHTTPResponseCode|, error.
func (h *Handler) writeOneHTTPBlob(ctx context.Context, path string,
	expectBytes, writeQuota int64, r io.Reader) (int64, string, int, error) {
	locationOnDisk, err := h.translateToKey(path)
	if err != nil {
		return 0, "", http.StatusUnprocessableEntity, err // 422: unprocessable entity
	}
	locationOnDisk = h.applyRandomizedSuffix(locationOnDisk)

	ctx, cancelWrite := context.WithCancel(ctx)
	blob, err := h.Bucket.NewWriter(ctx, locationOnDisk, nil)
	defer cancelWrite()
	if err != nil {
		return 0, locationOnDisk, http.StatusInternalServerError, err
	}
	bytesWritten, err := io.Copy(blob, r)
	if err != nil && err != io.EOF {
		cancelWrite() // Discards the file.
		blob.Close()
		if bytesWritten > 0 && bytesWritten < expectBytes {
			return bytesWritten, locationOnDisk, http.StatusInsufficientStorage, err // 507: insufficient storage
		}
		return bytesWritten, locationOnDisk, http.StatusInternalServerError, err
	}
	if expectBytes > 0 && bytesWritten != expectBytes {
		cancelWrite()
		blob.Close()
		return bytesWritten, locationOnDisk, http.StatusUnprocessableEntity, nil
	}

	if err := blob.Close(); err != nil {
		gcerr, _ := err.(interface{ Unwrap() error })
		switch e := gcerr.Unwrap().(type) {
		case *os.LinkError, *os.PathError:
			return bytesWritten, locationOnDisk, http.StatusConflict, e
		default:
			return bytesWritten, locationOnDisk, http.StatusInternalServerError, err
		}
	}
	return bytesWritten, locationOnDisk, http.StatusCreated, nil // 201: Created
}
