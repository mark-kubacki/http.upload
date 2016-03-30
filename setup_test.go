package upload // import "blitznote.com/src/caddy.upload"

import (
	"errors"
	"testing"

	"github.com/mholt/caddy/caddy/setup"
	. "github.com/smartystreets/goconvey/convey"
)

func TestSetupParse(t *testing.T) {
	tests := []struct {
		config       string
		expectedErr  error
		expectedConf HandlerConfiguration
	}{
		{
			`upload / { to "/var/tmp" }`,
			nil,
			HandlerConfiguration{
				PathScopes: []string{"/"},
				Scope: map[string]*ScopeConfiguration{
					"/": {
						TimestampTolerance:  1 << 2,
						WriteToPath:         "/var/tmp",
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
				to "/var/tmp"
				silent_auth_errors
			}`,
			nil,
			HandlerConfiguration{
				PathScopes: []string{"/"},
				Scope: map[string]*ScopeConfiguration{
					"/": {
						TimestampTolerance:  1 << 2,
						WriteToPath:         "/var/tmp",
						SilenceAuthErrors:   true,
						IncomingHmacSecrets: make(map[string][]byte),
					},
				},
			},
		},
		{
			`upload / {
				to "/var/tmp"
				timestamp_tolerance 8
			}`,
			nil,
			HandlerConfiguration{
				PathScopes: []string{"/"},
				Scope: map[string]*ScopeConfiguration{
					"/": {
						TimestampTolerance:  1 << 8,
						WriteToPath:         "/var/tmp",
						IncomingHmacSecrets: make(map[string][]byte),
					},
				},
			},
		},
		{
			`upload / {
				to "/var/tmp"
				timestamp_tolerance 33
			}`,
			errors.New("Testfile:3 - Parse error: must be ≤ 32"),
			HandlerConfiguration{},
		},
		{
			`upload / {
				to "/var/tmp"
				timestamp_tolerance 64
			}`,
			errors.New("Testfile:3 - Parse error: we're sorry, but by this time Sol has already melted Terra"),
			HandlerConfiguration{},
		},
		{
			`upload / {
				to "/var/tmp"
				hmac_keys_in hmac-key-1=TWFyaw==
			}`,
			nil,
			HandlerConfiguration{
				PathScopes: []string{"/"},
				Scope: map[string]*ScopeConfiguration{
					"/": {
						TimestampTolerance: 1 << 2,
						WriteToPath:        "/var/tmp",
						IncomingHmacSecrets: map[string][]byte{
							"hmac-key-1": []byte("Mark"),
						},
					},
				},
			},
		},
		{
			`upload / {
				to "/var/tmp"
				hmac_keys_in hmac-key-1
			}`,
			errors.New("Testfile:3 - Parse error: hmac-key-1"),
			HandlerConfiguration{},
		},
		{
			`upload / {
				to "/var/tmp"
				hmac_keys_in hmac-key-1=TWFyaw== zween=dXBsb2Fk
			}`,
			nil,
			HandlerConfiguration{
				PathScopes: []string{"/"},
				Scope: map[string]*ScopeConfiguration{
					"/": {
						TimestampTolerance: 1 << 2,
						WriteToPath:        "/var/tmp",
						IncomingHmacSecrets: map[string][]byte{
							"hmac-key-1": []byte("Mark"),
							"zween":      []byte("upload"),
						},
					},
				},
			},
		},
		{
			`upload /store { to "/var/tmp" }
			upload /space { to "/tmp" }`,
			nil,
			HandlerConfiguration{
				PathScopes: []string{"/store", "/space"},
				Scope: map[string]*ScopeConfiguration{
					"/store": {
						TimestampTolerance:  1 << 2,
						WriteToPath:         "/var/tmp",
						IncomingHmacSecrets: make(map[string][]byte),
					},
					"/space": {
						TimestampTolerance:  1 << 2,
						WriteToPath:         "/tmp",
						IncomingHmacSecrets: make(map[string][]byte),
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
