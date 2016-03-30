package upload // import "blitznote.com/src/caddy.upload"

import (
	"bytes"
	"crypto/rand"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/mholt/caddy/caddy/setup"
	"github.com/mholt/caddy/middleware"

	. "github.com/smartystreets/goconvey/convey"
)

const (
	// insist on /var/tmp for tests, because /tmp could be tmpfs
	trivialConfig = `upload / {
		to "/var/tmp"
	}`
)

func newTestUploadHander(t *testing.T, configExcerpt string) middleware.Handler {
	c := setup.NewTestController(configExcerpt)
	m, err := Setup(c)
	if err != nil {
		t.Fatal(err)
	}

	next := middleware.HandlerFunc(func(w http.ResponseWriter, r *http.Request) (int, error) {
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
		req, err := http.NewRequest("GET", "/stuff", strings.NewReader(""))
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
				os.Remove("/var/tmp/" + tempFName)
			}()

			code, err := h.ServeHTTP(w, req)
			if err != nil {
				t.Fatal(err)
			}
			So(code, ShouldEqual, 200)

			compareContents("/var/tmp/"+tempFName, []byte("DELME"))
		})

		Convey("succeeds with a size announced too large", func() {
			tempFName := tempFileName()
			req, err := http.NewRequest("PUT", "/"+tempFName, strings.NewReader("DELME"))
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Content-Length", "20")
			defer func() {
				os.Remove("/var/tmp/" + tempFName)
			}()

			code, err := h.ServeHTTP(w, req)
			if err != nil {
				t.Fatal(err)
			}
			So(code, ShouldEqual, 202)

			compareContents("/var/tmp/"+tempFName, []byte("DELME"))
		})

		Convey("gets aborted for files below the writable path", func() {
			tempFName := tempFileName()
			req, err := http.NewRequest("PUT", "/nop/../../../tmp/"+tempFName, strings.NewReader("DELME"))
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
				os.Remove("/var/tmp/" + tempFName)
			}()

			code, err := h.ServeHTTP(w, req)
			if err != nil {
				t.Fatal(err)
			}
			So(code, ShouldEqual, 200)

			compareContents("/var/tmp/"+tempFName, []byte("DELME"))
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
				os.Remove("/var/tmp/" + tempFName)
			}()
			defer func() {
				os.Remove("/var/tmp/" + tempFName2)
			}()

			code, err := h.ServeHTTP(w, req)
			if err != nil {
				t.Fatal(err)
			}
			So(code, ShouldEqual, 200)

			compareContents("/var/tmp/"+tempFName, []byte("DELME"))
			compareContents("/var/tmp/"+tempFName2, []byte("REMOVEME"))
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
				os.Remove("/var/tmp/" + tempFName)
			}()

			code, err := h.ServeHTTP(w, req)
			if err != nil {
				t.Fatal(err)
			}
			So(code, ShouldEqual, 200)

			compareContents("/var/tmp/"+tempFName, []byte("DELME"))
		})

		Convey("fails on unknown envelope formats", func() {
			tempFName := tempFileName()
			req, err := http.NewRequest("POST", "/"+tempFName, strings.NewReader("QUJD\n\nREVG"))
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Content-Type", "chunks-of/base64")
			defer func() {
				os.Remove("/var/tmp/" + tempFName)
			}()

			code, err := h.ServeHTTP(w, req)
			So(code, ShouldEqual, 415)
			So(err, ShouldBeNil)
		})
	})
}
