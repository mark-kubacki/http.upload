// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package upload // import "blitznote.com/src/caddy.upload"

import (
	"bytes"
	"crypto/rand"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/mholt/caddy"
	"github.com/mholt/caddy/caddyhttp/httpserver"

	. "github.com/smartystreets/goconvey/convey"
)

var (
	scratchDir    string // tests will create files and directories here
	trivialConfig string
)

func init() {
	scratchDir = os.TempDir()

	// don't pull in package 'fmt' for this
	trivialConfig = `upload /subdir {
		to "` + scratchDir + `"
		promise_download_from /newdir
	}
	upload / {
		to "` + scratchDir + `"
	}`
}

func newTestUploadHander(t *testing.T, configExcerpt string) httpserver.Handler {
	c := caddy.NewTestController("http", configExcerpt)
	err := Setup(c)
	if err != nil {
		t.Fatal(err)
	}
	upstream := httpserver.GetConfig(c)
	mids := upstream.Middleware()
	m := mids[0]

	next := httpserver.HandlerFunc(func(w http.ResponseWriter, r *http.Request) (int, error) {
		return http.StatusTeapot, nil
	})

	return m(next)
}

// Generates a new temporary file name without a path.
func tempFileName() string {
	buffer := make([]byte, 16)
	_, _ = rand.Read(buffer)
	for i := range buffer {
		buffer[i] = (buffer[i] % 25) + 97 // a–z
	}
	return string(buffer)
}

func compareContents(filename string, contents []byte) {
	fd, err := os.Open(filename)
	So(err, ShouldBeNil)
	if err != nil {
		return
	}
	defer fd.Close()

	buffer := make([]byte, (len(contents)/4096+1)*4096)
	n, err := fd.Read(buffer)
	if err != nil {
		SkipSo(n, ShouldEqual, len(contents))
		SkipSo(buffer[0:len(contents)], ShouldResemble, contents)
		So(err, ShouldBeNil)
		return
	}
	So(n, ShouldEqual, len(contents))
	So(buffer[0:len(contents)], ShouldResemble, contents)
}

func TestUpload_ServeHTTP(t *testing.T) {
	Convey("GET is a no-op", t, func() {
		h := newTestUploadHander(t, trivialConfig)
		w := httptest.NewRecorder()
		req, err := http.NewRequest("GET", "/stuff", nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Accept", "text/*")

		code, err := h.ServeHTTP(w, req)
		So(code, ShouldEqual, http.StatusTeapot)
		So(err, ShouldBeNil)
	})

	Convey("Uploading files using PUT", t, func() {
		h := newTestUploadHander(t, trivialConfig)
		w := httptest.NewRecorder()

		Convey("succeeds with one trivially small file", func() {
			tempFName := tempFileName()
			req, err := http.NewRequest("PUT", "/"+tempFName, strings.NewReader("DELME"))
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Content-Length", "5")
			defer func() {
				os.Remove(filepath.Join(scratchDir, tempFName))
			}()

			code, err := h.ServeHTTP(w, req)
			if err != nil {
				t.Fatal(err)
			}
			So(code, ShouldEqual, 201)

			compareContents(filepath.Join(scratchDir, tempFName), []byte("DELME"))
		})

		Convey("responds with a correct Location with one uploaded file", func() {
			tempFName := tempFileName()
			req, err := http.NewRequest("PUT", "/subdir/"+tempFName, strings.NewReader("DELME"))
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Content-Length", "5")
			defer func() {
				os.Remove(filepath.Join(scratchDir, tempFName))
			}()

			_, err = h.ServeHTTP(w, req)
			if err != nil {
				t.Fatal(err)
			}
			So(w.Header().Get("Location"), ShouldEqual, "/newdir/"+tempFName)
		})

		Convey("strips the prefix correctly", func() {
			scopeName := tempFileName()
			pathName, fileName := tempFileName(), tempFileName()

			h := newTestUploadHander(t, `upload /`+scopeName+` { to "`+scratchDir+`" }`)
			req, err := http.NewRequest("PUT", "/"+scopeName+"/"+pathName+"/"+fileName, strings.NewReader("DELME"))
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				os.RemoveAll(filepath.Join(scratchDir, scopeName))
			}()
			defer func() {
				os.RemoveAll(filepath.Join(scratchDir, pathName))
			}()

			code, _ := h.ServeHTTP(w, req)
			So(code, ShouldEqual, 201)

			_, err = os.Stat(filepath.Join(scratchDir, scopeName))
			So(os.IsNotExist(err), ShouldBeTrue)
		})

		Convey("succeeds with a size announced too large", func() {
			tempFName := tempFileName()
			req, err := http.NewRequest("PUT", "/"+tempFName, strings.NewReader("DELME"))
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Content-Length", "20")
			defer func() {
				os.Remove(filepath.Join(scratchDir, tempFName))
			}()

			code, err := h.ServeHTTP(w, req)
			if err != nil {
				t.Fatal(err)
			}
			So(code, ShouldEqual, 202)

			compareContents(filepath.Join(scratchDir, tempFName), []byte("DELME"))
		})

		Convey("gets aborted for files below the writable path", func() {
			tempFName := tempFileName()
			req, err := http.NewRequest("PUT", "/nop/../../../tmp/../"+tempFName, strings.NewReader("DELME"))
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Content-Length", "5")

			code, err := h.ServeHTTP(w, req)
			So(err, ShouldNotBeNil)
			if err != nil {
				So(err.Error(), ShouldEqual, "permission denied")
			}
			So(code, ShouldEqual, 422)
		})
	})

	Convey("Uploading files using POST", t, func() {
		h := newTestUploadHander(t, trivialConfig)
		w := httptest.NewRecorder()

		Convey("works with one file which is not in an envelope", func() {
			tempFName := tempFileName()
			req, err := http.NewRequest("POST", "/"+tempFName, strings.NewReader("DELME"))
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				os.Remove(filepath.Join(scratchDir, tempFName))
			}()

			code, err := h.ServeHTTP(w, req)
			if err != nil {
				t.Fatal(err)
			}
			So(code, ShouldEqual, 201)

			compareContents(filepath.Join(scratchDir, tempFName), []byte("DELME"))
		})

		Convey("succeeds with two trivially small files", func() {
			tempFName, tempFName2 := tempFileName(), tempFileName()

			// START
			body := &bytes.Buffer{}
			writer := multipart.NewWriter(body)
			p, _ := writer.CreateFormFile("A", tempFName)
			p.Write([]byte("DELME"))
			p, _ = writer.CreateFormFile("B", tempFName2)
			p.Write([]byte("REMOVEME"))
			writer.Close()
			// END

			req, err := http.NewRequest("POST", "/", body)
			req.Header.Set("Content-Type", writer.FormDataContentType())
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				os.Remove(filepath.Join(scratchDir, tempFName))
			}()
			defer func() {
				os.Remove(filepath.Join(scratchDir, tempFName2))
			}()

			code, err := h.ServeHTTP(w, req)
			if err != nil {
				t.Fatal(err)
			}
			So(code, ShouldEqual, 201)

			compareContents(filepath.Join(scratchDir, tempFName), []byte("DELME"))
			compareContents(filepath.Join(scratchDir, tempFName2), []byte("REMOVEME"))
		})

		Convey("succeeds if two files have the same name (overwriting within the same transaction)", func() {
			tempFName := tempFileName()

			// START
			body := &bytes.Buffer{}
			writer := multipart.NewWriter(body)
			p, _ := writer.CreateFormFile("A", tempFName)
			p.Write([]byte("REMOVEME"))
			p, _ = writer.CreateFormFile("B", tempFName)
			p.Write([]byte("DELME"))
			writer.Close()
			// END

			req, err := http.NewRequest("POST", "/", body)
			req.Header.Set("Content-Type", writer.FormDataContentType())
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				os.Remove(filepath.Join(scratchDir, tempFName))
			}()

			code, err := h.ServeHTTP(w, req)
			if err != nil {
				t.Fatal(err)
			}
			So(code, ShouldEqual, 201)

			compareContents(filepath.Join(scratchDir, tempFName), []byte("DELME"))
		})

		Convey("fails on unknown envelope formats", func() {
			tempFName := tempFileName()
			req, err := http.NewRequest("POST", "/"+tempFName, strings.NewReader("QUJD\n\nREVG"))
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Content-Type", "chunks-of/base64")
			defer func() {
				os.Remove(filepath.Join(scratchDir, tempFName))
			}()

			code, _ := h.ServeHTTP(w, req)
			So(code, ShouldEqual, 415)
		})
	})

	Convey("A random suffix", t, func() {
		configWithSuffix := `upload / {
			to                    "` + scratchDir + `"
			promise_download_from /
			random_suffix_len     3
		}`

		h := newTestUploadHander(t, configWithSuffix)
		w := httptest.NewRecorder()

		Convey("can be used in a full filename as in NAME_XXX.EXT", func() {
			tempFName := tempFileName()

			// START
			body := &bytes.Buffer{}
			writer := multipart.NewWriter(body)
			p, _ := writer.CreateFormFile("A", "name.ext")
			p.Write([]byte("REMOVEME"))
			writer.Close()
			// END

			req, err := http.NewRequest("POST", "/", body)
			req.Header.Set("Content-Type", writer.FormDataContentType())
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				os.Remove(filepath.Join(scratchDir, tempFName))
			}()

			code, err := h.ServeHTTP(w, req)
			if err != nil {
				t.Fatal(err)
			}
			So(code, ShouldEqual, 201)

			uploadedAs := w.Header().Get("Location")
			So(uploadedAs, ShouldNotBeBlank)
			So(uploadedAs, ShouldStartWith, "/name_")
			So(uploadedAs, ShouldEndWith, ".ext")
			So(len(uploadedAs), ShouldEqual, 1+len("name.ext")+1+3) // /name_XXX.ext
		})

		Convey("will work with a suffix-only upload such as: .EXT", func() {
			tempFName := tempFileName()

			// START
			body := &bytes.Buffer{}
			writer := multipart.NewWriter(body)
			p, _ := writer.CreateFormFile("B", ".ext")
			p.Write([]byte("REMOVEME"))
			writer.Close()
			// END

			req, err := http.NewRequest("POST", "/", body)
			req.Header.Set("Content-Type", writer.FormDataContentType())
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				os.Remove(filepath.Join(scratchDir, tempFName))
			}()

			code, err := h.ServeHTTP(w, req)
			if err != nil {
				t.Fatal(err)
			}
			So(code, ShouldEqual, 201)

			uploadedAs := w.Header().Get("Location")
			So(uploadedAs, ShouldNotBeBlank)
			So(uploadedAs, ShouldStartWith, "/")
			So(uploadedAs, ShouldEndWith, ".ext")
			So(len(uploadedAs), ShouldEqual, 1+3+len(".ext")) // /XXX.ext
		})
	})

	Convey("Handling of conflicts includes", t, func() {
		h := newTestUploadHander(t, trivialConfig)
		w := httptest.NewRecorder()

		Convey("name clashes between directories and new filename", func() {
			tempFName := tempFileName()
			req, err := http.NewRequest("PUT", "/"+tempFName+"/"+tempFName, strings.NewReader("DELME"))
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				os.Remove(filepath.Join(scratchDir, tempFName, tempFName))
			}()

			code, _ := h.ServeHTTP(w, req)
			So(code, ShouldEqual, 201)

			// write to directory /var/tmp/${tempFName}
			req, err = http.NewRequest("PUT", "/"+tempFName, strings.NewReader("DELME"))
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				os.RemoveAll(filepath.Join(scratchDir, tempFName))
			}()

			code, _ = h.ServeHTTP(w, req)
			So(code, ShouldBeIn, 409, 500)
		})

		Convey("name clashes between filename and new directory", func() {
			tempFName := tempFileName()
			req, err := http.NewRequest("PUT", "/"+tempFName, strings.NewReader("DELME"))
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				os.Remove(filepath.Join(scratchDir, tempFName))
			}()

			code, _ := h.ServeHTTP(w, req)
			So(code, ShouldEqual, 201)

			// write to directory /var/tmp/${tempFName}
			req, err = http.NewRequest("PUT", "/"+tempFName+"/"+tempFName, strings.NewReader("DELME"))
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				os.RemoveAll(filepath.Join(scratchDir, tempFName, tempFName))
			}()

			code, _ = h.ServeHTTP(w, req)
			if runtime.GOOS == "windows" {
				So(code, ShouldBeIn, 409, 500)
			} else {
				So(code, ShouldEqual, 409) // 409: conflict
			}
		})
	})

	Convey("COPY, MOVE, and DELETE are supported", t, func() {
		h := newTestUploadHander(t, trivialConfig)
		w := httptest.NewRecorder()

		SkipConvey("COPY duplicates a file", func() {
			tempFName, copyFName := tempFileName(), tempFileName()
			req, _ := http.NewRequest("PUT", "/"+tempFName, strings.NewReader("DELME"))
			defer func() {
				os.Remove(filepath.Join(scratchDir, tempFName))
			}()

			code, _ := h.ServeHTTP(w, req)
			if code != 200 {
				So(code, ShouldEqual, 200)
				return
			}

			// COPY
			req, _ = http.NewRequest("COPY", "/"+tempFName, nil)
			req.Header.Set("Destination", "/"+copyFName)
			defer func() {
				os.Remove(filepath.Join(scratchDir, copyFName))
			}()

			code, _ = h.ServeHTTP(w, req)
			So(code, ShouldEqual, 201)

			_, err := os.Stat(filepath.Join(scratchDir, copyFName))
			So(os.IsNotExist(err), ShouldBeFalse)
		})

		Convey("MOVE renames a file", func() {
			tempFName, copyFName := tempFileName(), tempFileName()
			req, _ := http.NewRequest("PUT", "/"+tempFName, strings.NewReader("DELME"))
			defer func() {
				os.Remove(filepath.Join(scratchDir, tempFName))
			}()

			code, _ := h.ServeHTTP(w, req)
			if !(code == 200 || code == 201 || code == 204) {
				So(code, ShouldEqual, 201)
				return
			}

			// MOVE
			req, _ = http.NewRequest("MOVE", "/"+tempFName, nil)
			req.Header.Set("Destination", "/"+copyFName)
			defer func() {
				os.Remove(filepath.Join(scratchDir, copyFName))
			}()

			code, _ = h.ServeHTTP(w, req)
			So(code, ShouldEqual, 201)

			_, err := os.Stat(filepath.Join(scratchDir, tempFName))
			So(os.IsNotExist(err), ShouldBeTrue)
			_, err = os.Stat(filepath.Join(scratchDir, copyFName))
			So(os.IsNotExist(err), ShouldBeFalse)
		})

		Convey("DELETE removes a file", func() {
			tempFName := tempFileName()
			req, _ := http.NewRequest("PUT", "/"+tempFName, strings.NewReader("DELME"))
			defer func() {
				os.Remove(filepath.Join(scratchDir, tempFName))
			}()

			code, _ := h.ServeHTTP(w, req)
			if !(code == 200 || code == 201 || code == 204) {
				So(code, ShouldEqual, 201)
				return
			}

			// DELETE
			req, _ = http.NewRequest("DELETE", "/"+tempFName, nil)

			code, _ = h.ServeHTTP(w, req)
			So(code, ShouldEqual, 204)

			_, err := os.Stat(filepath.Join(scratchDir, tempFName))
			So(os.IsNotExist(err), ShouldBeTrue)
		})

		Convey("DELETE will not remove the target directory", func() {
			req, _ := http.NewRequest("DELETE", "/subdir", nil)

			code, _ := h.ServeHTTP(w, req)
			So(code, ShouldEqual, 403)

			_, err := os.Stat(scratchDir)
			So(os.IsNotExist(err), ShouldBeFalse)
		})
	})
}
