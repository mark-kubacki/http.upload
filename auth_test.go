package upload // import "hub.blitznote.com/src/caddy.upload"

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
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

var (
	trivialConfigWithAuth string
)

func init() {
	scratchDir = os.TempDir()

	// don't pull in package 'fmt' for this
	trivialConfigWithAuth = `upload / {
		to "` + scratchDir + `"
		hmac_keys_in hmac-key-1=TWFyaw== zween=dXBsb2Fk
	}`
}

func computeSignature(secret []byte, headerContents []string) string {
	mac := hmac.New(sha256.New, secret)
	for _, v := range headerContents {
		mac.Write([]byte(v))
	}
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

func TestUploadAuthentication(t *testing.T) {
	Convey("Given authentication", t, func() {
		h := newTestUploadHander(t, trivialConfigWithAuth)
		w := httptest.NewRecorder()

		Convey("deny uploads lacking the expected header", func() {
			tempFName := tempFileName()
			req, err := http.NewRequest("PUT", "/"+tempFName, strings.NewReader("DELME"))
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Content-Length", "5")

			code, err := h.ServeHTTP(w, req)
			So(err, ShouldNotBeNil)
			So(code, ShouldEqual, 401)
			So(w.Header().Get("WWW-Authenticate"), ShouldEqual, "Signature")
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

			code, err := h.ServeHTTP(w, req)
			So(code, ShouldEqual, 200)
			So(err, ShouldBeNil)

			compareContents(filepath.Join(scratchDir, tempFName), []byte("DELME"))
		})
	})
}
