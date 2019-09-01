// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// +build caddyserver0.9 caddyserver1.0

package upload

import (
	"errors"
	"os"
	"testing"
	"unicode"

	"github.com/caddyserver/caddy"
	"github.com/caddyserver/caddy/caddyhttp/httpserver"
	. "github.com/smartystreets/goconvey/convey"
	"golang.org/x/text/unicode/norm"
)

func TestSetupParse(t *testing.T) {
	scratchDir := os.TempDir()

	tests := []struct {
		config       string
		expectedErr  error
		expectedConf HandlerConfiguration
	}{
		{
			`upload / { to "` + scratchDir + `" }`,
			nil,
			HandlerConfiguration{
				PathScopes: []string{"/"},
				Scope: map[string]*ScopeConfiguration{
					"/": {
						TimestampTolerance:  1 << 2,
						WriteToPath:         scratchDir,
						IncomingHmacSecrets: make(map[string][]byte),
					},
				},
			},
		},
		{
			`upload /`,
			errors.New("Testfile:1 - Error during parsing: The destination path 'to' is missing"),
			HandlerConfiguration{},
		},
		{
			`upload / {
				to "` + scratchDir + `"
				silent_auth_errors
			}`,
			nil,
			HandlerConfiguration{
				PathScopes: []string{"/"},
				Scope: map[string]*ScopeConfiguration{
					"/": {
						TimestampTolerance:  1 << 2,
						WriteToPath:         scratchDir,
						SilenceAuthErrors:   true,
						IncomingHmacSecrets: make(map[string][]byte),
					},
				},
			},
		},
		{
			`upload / {
				to "` + scratchDir + `"
				timestamp_tolerance 8
			}`,
			nil,
			HandlerConfiguration{
				PathScopes: []string{"/"},
				Scope: map[string]*ScopeConfiguration{
					"/": {
						TimestampTolerance:  1 << 8,
						WriteToPath:         scratchDir,
						IncomingHmacSecrets: make(map[string][]byte),
					},
				},
			},
		},
		{
			`upload / {
				to "` + scratchDir + `"
				max_filesize 2048000
			}`,
			nil,
			HandlerConfiguration{
				PathScopes: []string{"/"},
				Scope: map[string]*ScopeConfiguration{
					"/": {
						TimestampTolerance:  1 << 2,
						MaxFilesize:         2048000,
						WriteToPath:         scratchDir,
						IncomingHmacSecrets: make(map[string][]byte),
					},
				},
			},
		},
		{
			`upload / {
				to "` + scratchDir + `"
				max_transaction_size 8196000
			}`,
			nil,
			HandlerConfiguration{
				PathScopes: []string{"/"},
				Scope: map[string]*ScopeConfiguration{
					"/": {
						TimestampTolerance:  1 << 2,
						MaxTransactionSize:  8196000,
						WriteToPath:         scratchDir,
						IncomingHmacSecrets: make(map[string][]byte),
					},
				},
			},
		},
		{
			`upload / {
				to "` + scratchDir + `"
				timestamp_tolerance 33
			}`,
			errors.New("Testfile:3 - Error during parsing: must be ≤ 32"),
			HandlerConfiguration{},
		},
		{
			`upload / {
				to "` + scratchDir + `"
				timestamp_tolerance 64
			}`,
			errors.New("Testfile:3 - Error during parsing: we're sorry, but by this time Sol has already melted Terra"),
			HandlerConfiguration{},
		},
		{
			`upload / {
				to "` + scratchDir + `"
				hmac_keys_in hmac-key-1=TWFyaw==
			}`,
			nil,
			HandlerConfiguration{
				PathScopes: []string{"/"},
				Scope: map[string]*ScopeConfiguration{
					"/": {
						TimestampTolerance: 1 << 2,
						WriteToPath:        scratchDir,
						IncomingHmacSecrets: map[string][]byte{
							"hmac-key-1": []byte("Mark"),
						},
					},
				},
			},
		},
		{
			`upload / {
				to "` + scratchDir + `"
				hmac_keys_in hmac-key-1
			}`,
			errors.New("Testfile:3 - Error during parsing: hmac-key-1"),
			HandlerConfiguration{},
		},
		{
			`upload / {
				to "` + scratchDir + `"
				hmac_keys_in hmac-key-1=TWFyaw== zween=dXBsb2Fk
			}`,
			nil,
			HandlerConfiguration{
				PathScopes: []string{"/"},
				Scope: map[string]*ScopeConfiguration{
					"/": {
						TimestampTolerance: 1 << 2,
						WriteToPath:        scratchDir,
						IncomingHmacSecrets: map[string][]byte{
							"hmac-key-1": []byte("Mark"),
							"zween":      []byte("upload"),
						},
					},
				},
			},
		},
		{
			`upload /store { to "` + scratchDir + `" }
			upload /space { to "/" }`,
			nil,
			HandlerConfiguration{
				PathScopes: []string{"/store", "/space"},
				Scope: map[string]*ScopeConfiguration{
					"/store": {
						TimestampTolerance:  1 << 2,
						WriteToPath:         scratchDir,
						IncomingHmacSecrets: make(map[string][]byte),
					},
					"/space": {
						TimestampTolerance:  1 << 2,
						WriteToPath:         "/",
						IncomingHmacSecrets: make(map[string][]byte),
					},
				},
			},
		},
		{
			`upload /test-11 {
				to "` + scratchDir + `"
				filenames_form NFC
			}`,
			nil,
			HandlerConfiguration{
				PathScopes: []string{"/test-11"},
				Scope: map[string]*ScopeConfiguration{
					"/test-11": {
						TimestampTolerance:  1 << 2,
						WriteToPath:         scratchDir,
						IncomingHmacSecrets: make(map[string][]byte),
						UnicodeForm:         &struct{ Use norm.Form }{Use: norm.NFC},
					},
				},
			},
		},
		{
			`upload /test-12 {
				to "` + scratchDir + `"
				filenames_in u0000-u007F u0100-u017F u0391-u03C9  u2018–u203D u2152–u217F // Liceum in Europe
			}`,
			nil,
			HandlerConfiguration{
				PathScopes: []string{"/test-12"},
				Scope: map[string]*ScopeConfiguration{
					"/test-12": {
						TimestampTolerance:  1 << 2,
						WriteToPath:         scratchDir,
						IncomingHmacSecrets: make(map[string][]byte),
						RestrictFilenamesTo: []*unicode.RangeTable{{
							R16: []unicode.Range16{
								{0x0000, 0x007f, 1},
								{0x0100, 0x017f, 1},
								{0x0391, 0x03c9, 1},
								{0x2018, 0x203d, 1},
								{0x2152, 0x217f, 1},
							},
							LatinOffset: 1,
						}},
					},
				},
			},
		},
	}

	Convey("Setup of the controller", t, func() {
		for idx := range tests {
			test := tests[idx]
			c := caddy.NewTestController("http", test.config)
			err := Setup(c)
			if test.expectedErr != nil {
				So(err, ShouldResemble, test.expectedErr)
				continue
			}

			mids := httpserver.GetConfig(c).Middleware()
			So(len(mids), ShouldEqual, 1)

			i := mids[0](httpserver.EmptyNext)
			myHandler, ok := i.(*Handler)
			So(ok, ShouldBeTrue)

			// strip functors (cannot compare them)
			for _, scopeConf := range myHandler.Config.Scope {
				scopeConf.UploadProgressCallback = nil
			}

			So(myHandler.Config, ShouldResemble, test.expectedConf)
			So(err, ShouldResemble, test.expectedErr)
		}
	})
}
