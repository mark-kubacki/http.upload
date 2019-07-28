// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package upload // import "blitznote.com/src/http.upload"

import (
	"bytes"
	"crypto/rand"
	"io/ioutil"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

var (
	scratchDir string // tests will create files and directories here

	trivialConfig http.Handler
	sizeLimited   http.Handler

	next = new(teapotHandler)
)

// A dummy with a pre-defined return value not found in production,
// used in place of any actual chained handler.
// Enables us to see whether a request has been passed through.
type teapotHandler struct {
	http.Handler
}

// ServeHTTP implements the http.Handler interface.
func (n teapotHandler) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	code := http.StatusTeapot
	http.Error(w, http.StatusText(code), code)
}

func init() {
	scratchDir = os.TempDir()

	t := http.NewServeMux()
	{
		c1 := NewDefaultConfiguration(scratchDir)
		c1.EnableWebdav = true
		c1.ApparentLocation = "/newdir"
		h1, _ := NewHandler("/subdir", c1, next)
		t.Handle("/subdir/", h1)

		c2 := NewDefaultConfiguration(scratchDir)
		c2.EnableWebdav = true
		h2, _ := NewHandler("/", c2, next)
		t.Handle("/", h2)
	}
	trivialConfig = t

	u := http.NewServeMux()
	{
		c1 := NewDefaultConfiguration(scratchDir)
		c1.MaxFilesize = 64000
		c1.MaxTransactionSize = 0
		h1, _ := NewHandler("/filesize", c1, next)
		u.Handle("/filesize/", h1)

		c2 := NewDefaultConfiguration(scratchDir)
		c2.MaxFilesize = 0
		c2.MaxTransactionSize = 64000
		h2, _ := NewHandler("/transaction", c2, next)
		u.Handle("/transaction/", h2)

		c3 := NewDefaultConfiguration(scratchDir)
		c3.MaxFilesize = 64000
		c3.MaxTransactionSize = 128000
		h3, _ := NewHandler("/both/", c3, next)
		u.Handle("/both/", h3)
	}
	sizeLimited = u
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
		h := trivialConfig
		w := httptest.NewRecorder()
		req, err := http.NewRequest("GET", "/stuff", nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Accept", "text/*")

		h.ServeHTTP(w, req)
		resp := w.Result()
		ioutil.ReadAll(resp.Body)

		So(resp.StatusCode, ShouldEqual, http.StatusTeapot)
	})

	Convey("Uploading files using PUT", t, func() {
		h := trivialConfig

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

			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)
			resp := w.Result()
			ioutil.ReadAll(resp.Body)

			So(resp.StatusCode, ShouldEqual, 201)

			compareContents(filepath.Join(scratchDir, tempFName), []byte("DELME"))
		})

		Convey("succeeds with an empty file", func() {
			tempFName := tempFileName()
			req, err := http.NewRequest("PUT", "/"+tempFName, strings.NewReader(""))
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Content-Length", "0")
			defer func() {
				os.Remove(filepath.Join(scratchDir, tempFName))
			}()

			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)
			resp := w.Result()
			ioutil.ReadAll(resp.Body)

			So(resp.StatusCode, ShouldEqual, 201)

			fileStat, err := os.Stat(filepath.Join(scratchDir, tempFName))
			if err != nil {
				t.Fatal(err)
			}
			So(fileStat.Size(), ShouldEqual, 0)
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

			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)
			resp := w.Result()
			ioutil.ReadAll(resp.Body)

			So(resp.Header.Get("Location"), ShouldEqual, "/newdir/"+tempFName)
		})

		Convey("strips the prefix correctly", func() {
			scopeName := tempFileName()
			pathName, fileName := tempFileName(), tempFileName()

			cfg := NewDefaultConfiguration(scratchDir)
			h, _ := NewHandler("/"+scopeName+"/", cfg, next)

			req, err := http.NewRequest("PUT", "/"+scopeName+"/"+pathName+"/"+fileName, strings.NewReader("DELME"))
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Content-Length", "5")
			defer func() {
				os.RemoveAll(filepath.Join(scratchDir, scopeName))
			}()
			defer func() {
				os.RemoveAll(filepath.Join(scratchDir, pathName))
			}()

			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)
			resp := w.Result()
			ioutil.ReadAll(resp.Body)

			So(resp.StatusCode, ShouldEqual, 201)
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

			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)
			resp := w.Result()
			ioutil.ReadAll(resp.Body)

			So(resp.StatusCode, ShouldEqual, 202)

			compareContents(filepath.Join(scratchDir, tempFName), []byte("DELME"))
		})

		Convey("gets aborted for files below the writable path", func() {
			// Bypass http.ServeMux becuase it interferes with path parsing.
			h, _ := NewHandler("/", NewDefaultConfiguration(scratchDir), next)

			tempFName := tempFileName()
			req, err := http.NewRequest("PUT", "/nop/../../../tmp/../"+tempFName, strings.NewReader("DELME"))
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Content-Length", "5")

			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)
			resp := w.Result()
			ioutil.ReadAll(resp.Body)
			So(resp.StatusCode, ShouldEqual, 422)
		})
	})

	Convey("Uploading files using POST", t, func() {
		h := trivialConfig

		Convey("works with one file which is not in an envelope", func() {
			tempFName := tempFileName()
			req, err := http.NewRequest("POST", "/"+tempFName, strings.NewReader("DELME"))
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Content-Length", "5")
			defer func() {
				os.Remove(filepath.Join(scratchDir, tempFName))
			}()

			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)
			resp := w.Result()
			ioutil.ReadAll(resp.Body)

			So(resp.StatusCode, ShouldEqual, 201)

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

			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)
			resp := w.Result()
			ioutil.ReadAll(resp.Body)

			So(resp.StatusCode, ShouldEqual, 201)

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

			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)
			resp := w.Result()
			ioutil.ReadAll(resp.Body)

			So(resp.StatusCode, ShouldEqual, 201)

			compareContents(filepath.Join(scratchDir, tempFName), []byte("DELME"))
		})

		Convey("fails on unknown envelope formats", func() {
			tempFName := tempFileName()
			req, err := http.NewRequest("POST", "/"+tempFName, strings.NewReader("QUJD\n\nREVG"))
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Content-Type", "chunks-of/base64")
			req.Header.Set("Content-Length", "10")
			defer func() {
				os.Remove(filepath.Join(scratchDir, tempFName))
			}()

			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)
			resp := w.Result()
			ioutil.ReadAll(resp.Body)

			So(resp.StatusCode, ShouldEqual, 415)
		})
	})

	Convey("A random suffix", t, func() {
		cfg := NewDefaultConfiguration(scratchDir)
		cfg.ApparentLocation = "/"
		cfg.RandomizedSuffixLength = 3
		h, _ := NewHandler("/", cfg, next)

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

			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)
			resp := w.Result()
			ioutil.ReadAll(resp.Body)

			So(resp.StatusCode, ShouldEqual, 201)

			uploadedAs := resp.Header.Get("Location")
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

			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)
			resp := w.Result()
			ioutil.ReadAll(resp.Body)

			So(resp.StatusCode, ShouldEqual, 201)

			uploadedAs := resp.Header.Get("Location")
			So(uploadedAs, ShouldNotBeBlank)
			So(uploadedAs, ShouldStartWith, "/")
			So(uploadedAs, ShouldEndWith, ".ext")
			So(len(uploadedAs), ShouldEqual, 1+3+len(".ext")) // /XXX.ext
		})
	})

	Convey("Handling of conflicts includes", t, func() {
		h, _ := NewHandler("/", NewDefaultConfiguration(scratchDir), next)

		Convey("name clashes between directories and new filename", func() {
			tempFName := tempFileName()
			req, err := http.NewRequest("PUT", "/"+tempFName+"/"+tempFName, strings.NewReader("DELME"))
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Content-Length", "5")
			defer func() {
				os.Remove(filepath.Join(scratchDir, tempFName, tempFName))
			}()

			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)
			resp := w.Result()
			ioutil.ReadAll(resp.Body)
			So(resp.StatusCode, ShouldEqual, 201)

			// write to directory /var/tmp/${tempFName}
			req, err = http.NewRequest("PUT", "/"+tempFName, strings.NewReader("DELME"))
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Content-Length", "5")
			defer func() {
				os.RemoveAll(filepath.Join(scratchDir, tempFName))
			}()

			w = httptest.NewRecorder()
			h.ServeHTTP(w, req)
			resp = w.Result()
			ioutil.ReadAll(resp.Body)
			So(resp.StatusCode, ShouldBeIn, 409, 500)
		})

		Convey("name clashes between filename and new directory", func() {
			tempFName := tempFileName()
			req, err := http.NewRequest("PUT", "/"+tempFName, strings.NewReader("DELME"))
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Content-Length", "5")
			defer func() {
				os.Remove(filepath.Join(scratchDir, tempFName))
			}()

			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)
			resp := w.Result()
			ioutil.ReadAll(resp.Body)
			So(resp.StatusCode, ShouldEqual, 201)

			// write to directory /var/tmp/${tempFName}
			req, err = http.NewRequest("PUT", "/"+tempFName+"/"+tempFName, strings.NewReader("DELME"))
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Content-Length", "5")
			defer func() {
				os.RemoveAll(filepath.Join(scratchDir, tempFName, tempFName))
			}()

			w = httptest.NewRecorder()
			h.ServeHTTP(w, req)
			resp = w.Result()
			ioutil.ReadAll(resp.Body)

			if runtime.GOOS == "windows" {
				So(resp.StatusCode, ShouldBeIn, 409, 500)
			} else {
				So(resp.StatusCode, ShouldEqual, 409) // 409: conflict
			}
		})
	})

	Convey("COPY, MOVE, and DELETE are supported", t, func() {
		h := trivialConfig

		SkipConvey("COPY duplicates a file", func() {
			tempFName, copyFName := tempFileName(), tempFileName()
			req, _ := http.NewRequest("PUT", "/"+tempFName, strings.NewReader("DELME"))
			defer func() {
				os.Remove(filepath.Join(scratchDir, tempFName))
			}()
			req.Header.Set("Content-Length", "5")

			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)
			resp := w.Result()
			ioutil.ReadAll(resp.Body)
			if resp.StatusCode != 200 {
				So(resp.StatusCode, ShouldEqual, 200)
				return
			}

			// COPY
			req, _ = http.NewRequest("COPY", "/"+tempFName, nil)
			req.Header.Set("Destination", "/"+copyFName)
			defer func() {
				os.Remove(filepath.Join(scratchDir, copyFName))
			}()

			w = httptest.NewRecorder()
			h.ServeHTTP(w, req)
			resp = w.Result()
			ioutil.ReadAll(resp.Body)

			So(resp.StatusCode, ShouldEqual, 201)

			_, err := os.Stat(filepath.Join(scratchDir, copyFName))
			So(os.IsNotExist(err), ShouldBeFalse)
		})

		Convey("MOVE renames a file", func() {
			tempFName, copyFName := tempFileName(), tempFileName()
			req, _ := http.NewRequest("PUT", "/"+tempFName, strings.NewReader("DELME"))
			defer func() {
				os.Remove(filepath.Join(scratchDir, tempFName))
			}()
			req.Header.Set("Content-Length", "5")

			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)
			resp := w.Result()
			ioutil.ReadAll(resp.Body)
			So(resp.StatusCode, ShouldEqual, 201)

			// MOVE
			req, _ = http.NewRequest("MOVE", "/"+tempFName, nil)
			req.Header.Set("Destination", "/"+copyFName)
			defer func() {
				os.Remove(filepath.Join(scratchDir, copyFName))
			}()

			w = httptest.NewRecorder()
			h.ServeHTTP(w, req)
			resp = w.Result()
			ioutil.ReadAll(resp.Body)

			So(resp.StatusCode, ShouldEqual, 201)

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
			req.Header.Set("Content-Length", "5")

			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)
			resp := w.Result()
			ioutil.ReadAll(resp.Body)
			So(resp.StatusCode, ShouldEqual, 201)

			// DELETE
			req, _ = http.NewRequest("DELETE", "/"+tempFName, nil)

			w = httptest.NewRecorder()
			h.ServeHTTP(w, req)
			resp = w.Result()
			ioutil.ReadAll(resp.Body)

			So(resp.StatusCode, ShouldEqual, 204)

			_, err := os.Stat(filepath.Join(scratchDir, tempFName))
			So(os.IsNotExist(err), ShouldBeTrue)
		})

		Convey("DELETE will not remove the target directory", func() {
			cfg := NewDefaultConfiguration(scratchDir)
			cfg.EnableWebdav = true
			h, _ := NewHandler("/subdir", cfg, next)
			req, _ := http.NewRequest("DELETE", "/subdir", nil)

			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)
			resp := w.Result()
			ioutil.ReadAll(resp.Body)
			So(resp.StatusCode, ShouldEqual, 403)

			_, err := os.Stat(scratchDir)
			So(os.IsNotExist(err), ShouldBeFalse)
		})
	})

	Convey("Cap", t, func() {
		h := sizeLimited

		Convey("maximum filesize for single-file uploads", func() {
			for _, limitedBy := range [...]string{"filesize", "transaction", "both"} {
				Convey("by configuring a limit to "+limitedBy, func() {
					tempFName := tempFileName()
					req, err := http.NewRequest("POST", "/"+limitedBy+"/"+tempFName, strings.NewReader("DELME"))
					if err != nil {
						t.Fatal(err)
					}
					defer func() {
						os.Remove(filepath.Join(scratchDir, tempFName))
					}()

					// test header processing
					req.Header.Set("Content-Length", "64001")
					w := httptest.NewRecorder()
					h.ServeHTTP(w, req)
					resp := w.Result()
					ioutil.ReadAll(resp.Body)
					So(resp.StatusCode, ShouldEqual, 413) // too large, as indicated by the header

					req.Header.Set("Content-Length", "64000")
					w = httptest.NewRecorder()
					h.ServeHTTP(w, req)
					resp = w.Result()
					ioutil.ReadAll(resp.Body)
					So(resp.StatusCode, ShouldBeIn, 201, 202) // at the limit

					// now change the actual file contents
					req.Body = ioutil.NopCloser(strings.NewReader(strings.Repeat("\xcc", 64000)))
					w = httptest.NewRecorder()
					h.ServeHTTP(w, req)
					resp = w.Result()
					ioutil.ReadAll(resp.Body)
					So(resp.StatusCode, ShouldBeIn, 201, 202)

					req.Header.Del("Content-Length")
					req.Body = ioutil.NopCloser(strings.NewReader(strings.Repeat("\x33", 64001)))
					w = httptest.NewRecorder()
					h.ServeHTTP(w, req)
					resp = w.Result()
					ioutil.ReadAll(resp.Body)
					So(resp.StatusCode, ShouldEqual, 413)
				})
			}
		})

		Convey("maximum filesize for multi-file uploads", func() {
			for _, limitedBy := range [...]string{"filesize", "transaction", "both"} {
				Convey("by configuring a limit to "+limitedBy, func() {
					tempFName := tempFileName()

					// Test headers separately because multipart.NewWriter does not set them.
					ctype := "multipart/form-data; boundary=wall"
					headerOnlyBody := `--wall
Content-Disposition: form-data; name="fine"; filename="` + tempFName + `"
Content-Type: application/octet-stream
Content-Length: 1234

Winter is coming.
--wall--

`

					req, err := http.NewRequest("POST", "/"+limitedBy+"/", strings.NewReader(headerOnlyBody))
					req.Header.Set("Content-Type", ctype)
					if err != nil {
						t.Fatal(err)
					}
					defer func() {
						os.Remove(filepath.Join(scratchDir, tempFName))
					}()

					w := httptest.NewRecorder()
					h.ServeHTTP(w, req)
					resp := w.Result()
					ioutil.ReadAll(resp.Body)
					So(resp.StatusCode, ShouldBeIn, 201, 202)

					headerOnlyBody = strings.Replace(headerOnlyBody, "1234", "64001", 1)
					req, _ = http.NewRequest("POST", "/"+limitedBy+"/", strings.NewReader(headerOnlyBody))
					req.Header.Set("Content-Type", ctype)
					w = httptest.NewRecorder()
					h.ServeHTTP(w, req)
					resp = w.Result()
					ioutil.ReadAll(resp.Body)
					So(resp.StatusCode, ShouldBeIn, 413, 422)

					// As multipart.NewWriter does not set the Content-Length header this is about content only.
					body, ctype := payloadWithAttachments(tempFName, 64001)
					req, _ = http.NewRequest("POST", "/"+limitedBy+"/"+tempFName, body)
					req.Header.Set("Content-Type", ctype)
					w = httptest.NewRecorder()
					h.ServeHTTP(w, req)
					resp = w.Result()
					ioutil.ReadAll(resp.Body)
					So(resp.StatusCode, ShouldBeIn, 413, 422)

					body, ctype = payloadWithAttachments(tempFName, 64000)
					req, _ = http.NewRequest("POST", "/"+limitedBy+"/"+tempFName, body)
					req.Header.Set("Content-Type", ctype)
					w = httptest.NewRecorder()
					h.ServeHTTP(w, req)
					resp = w.Result()
					ioutil.ReadAll(resp.Body)
					So(resp.StatusCode, ShouldBeIn, 201, 202)

					body, ctype = payloadWithAttachments(tempFName, 64000, 64000)
					req, _ = http.NewRequest("POST", "/"+limitedBy+"/"+tempFName, body)
					req.Header.Set("Content-Type", ctype)
					w = httptest.NewRecorder()
					h.ServeHTTP(w, req)
					resp = w.Result()
					ioutil.ReadAll(resp.Body)
					switch limitedBy {
					case "transaction":
						So(resp.StatusCode, ShouldBeIn, 413, 422)
					default:
						So(resp.StatusCode, ShouldBeIn, 201, 202)
					}

					body, ctype = payloadWithAttachments(tempFName, 64000, 64000, 1)
					req, _ = http.NewRequest("POST", "/"+limitedBy+"/"+tempFName, body)
					req.Header.Set("Content-Type", ctype)
					w = httptest.NewRecorder()
					h.ServeHTTP(w, req)
					resp = w.Result()
					ioutil.ReadAll(resp.Body)
					switch limitedBy {
					case "transaction", "both":
						So(resp.StatusCode, ShouldBeIn, 413, 422)
					default:
						So(resp.StatusCode, ShouldBeIn, 201, 202)
					}

					body, ctype = payloadWithAttachments(tempFName, 64000, 64000, 64001)
					req, _ = http.NewRequest("POST", "/"+limitedBy+"/"+tempFName, body)
					req.Header.Set("Content-Type", ctype)
					w = httptest.NewRecorder()
					h.ServeHTTP(w, req)
					resp = w.Result()
					ioutil.ReadAll(resp.Body)
					So(resp.StatusCode, ShouldBeIn, 413, 422)
				})
			}
		})
	})
}

// payloadWithAttachments is a helper function to test MIME multipart uploads of different sizes.
func payloadWithAttachments(tempFName string, lengths ...int) (*bytes.Buffer, string) {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	for _, octets := range lengths {
		p, _ := writer.CreateFormFile("A", tempFName)
		p.Write([]byte(strings.Repeat("\x33", octets)))
	}
	writer.Close()

	return body, writer.FormDataContentType()
}
