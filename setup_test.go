package upload // import "blitznote.com/src/caddy.upload"

import (
	"errors"
	"os"
	"testing"

	"github.com/mholt/caddy/caddy/setup"
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
			errors.New("Testfile:1 - Parse error: The destination path 'to' is missing"),
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
				timestamp_tolerance 33
			}`,
			errors.New("Testfile:3 - Parse error: must be ≤ 32"),
			HandlerConfiguration{},
		},
		{
			`upload / {
				to "` + scratchDir + `"
				timestamp_tolerance 64
			}`,
			errors.New("Testfile:3 - Parse error: we're sorry, but by this time Sol has already melted Terra"),
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
			errors.New("Testfile:3 - Parse error: hmac-key-1"),
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
	}

	Convey("Setup of the controller", t, func() {
		for idx := range tests {
			test := tests[idx]
			c := setup.NewTestController(test.config)
			gotConf, err := parseCaddyConfig(c)

			if test.expectedErr != nil {
				So(err, ShouldResemble, test.expectedErr)
			} else {
				So(*gotConf, ShouldResemble, test.expectedConf)
			}
		}
	})
}
