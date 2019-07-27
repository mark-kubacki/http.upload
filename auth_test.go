// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package upload // import "blitznote.com/src/http.upload"

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	. "github.com/smartystreets/goconvey/convey"
)

func computeSignature(secret []byte, headerContents []string) string {
	mac := hmac.New(sha256.New, secret)
	for _, v := range headerContents {
		mac.Write([]byte(v))
	}
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

func TestUploadAuthentication(t *testing.T) {
	Convey("Given authentication", t, func() {
		scratchDir := os.TempDir()
		cfg := NewDefaultConfiguration(scratchDir)
		cfg.IncomingHmacSecrets.Insert([]string{"hmac-key-1=TWFyaw==", "zween=dXBsb2Fk"})
		h, _ := NewHandler("/", cfg, next)
		w := httptest.NewRecorder()

		Convey("deny uploads lacking the expected header", func() {
			tempFName := tempFileName()
			req, err := http.NewRequest("PUT", "/"+tempFName, strings.NewReader("DELME"))
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Content-Length", "5")

			h.ServeHTTP(w, req)
			resp := w.Result()
			ioutil.ReadAll(resp.Body)

			So(resp.StatusCode, ShouldEqual, 401)
			So(resp.Header.Get("WWW-Authenticate"), ShouldEqual, "Signature")
		})

		Convey("pass the upload operation on valid input", func() {
			tempFName := tempFileName()
			req, err := http.NewRequest("PUT", "/"+tempFName, strings.NewReader("DELME"))
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Content-Length", "5")
			defer func() {
				os.Remove(filepath.Join(scratchDir, tempFName))
			}()
			ts := strconv.FormatInt(time.Now().Unix(), 10)
			req.Header.Set("Timestamp", ts)
			req.Header.Set("Token", "ABC")
			req.Header.Set("Authorization", fmt.Sprintf(`Signature keyId="%s",signature="%s"`,
				"zween", computeSignature([]byte("upload"), []string{ts, "ABC"})))

			h.ServeHTTP(w, req)
			resp := w.Result()
			ioutil.ReadAll(resp.Body)

			So(resp.StatusCode, ShouldEqual, 201)

			compareContents(filepath.Join(scratchDir, tempFName), []byte("DELME"))
		})
	})
}
