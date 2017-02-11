// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package upload // import "blitznote.com/src/caddy.upload"

import (
	"log"
	"os"
	"strconv"
	"strings"
	"sync"
	"unicode"

	"blitznote.com/src/caddy.upload/signature.auth"
	"github.com/mholt/caddy"
	"github.com/mholt/caddy/caddyhttp/httpserver"
	"golang.org/x/text/unicode/norm"
)

const (
	// Part of this is used to display a substitute for a version number of the plugin.
	hashsumOfSetupSource = "$Id$"
)

func init() {
	caddy.RegisterPlugin("upload", caddy.Plugin{
		ServerType: "http",
		Action:     Setup,
	})
}

// Setup configures an UploadHander instance.
//
// This is called by Caddy.
func Setup(c *caddy.Controller) error {
	if c.Dispenser.File() != "Testfile" {
		log.Printf("Version of plugin 'upload': %s-%s-%s\n",
			hashsumOfSetupSource[5:13],
			hashsumOfUploadSource[5:13],
			hashsumOfFilenameSource[5:13])
	}

	config, err := parseCaddyConfig(c)
	if err != nil {
		return err
	}

	site := httpserver.GetConfig(c)
	if site.TLS == nil || !site.TLS.Enabled {
		if c.Dispenser.File() == "Testfile" {
			goto pass
		}
		for _, host := range []string{"127.0.0.1", "localhost", "[::1]", "::1"} {
			if site.Addr.Host == host || strings.HasPrefix(site.Addr.Host, host) {
				goto pass
			}
		}

		for _, scopeConf := range config.Scope {
			if !scopeConf.AcknowledgedNoTLS {
				return c.Err("You are using plugin 'upload' on a site without TLS.")
			}
		}
	}

pass:
	site.AddMiddleware(func(next httpserver.Handler) httpserver.Handler {
		return &Handler{
			Next:   next,
			Config: *config,
		}
	})

	return nil
}

// ScopeConfiguration represents the settings of a scope (URL path).
type ScopeConfiguration struct {
	// How big a difference between 'now' and the provided timestamp do we tolerate?
	// In seconds. Due to possible optimizations this should be an order of 2.
	// A reasonable default is 1<<2.
	TimestampTolerance uint64

	// Target directory on disk that serves as upload destination.
	WriteToPath string

	// Uploaded files can be gotten back from here.
	// If ≠ "" this will trigger sending headers such as "Location".
	ApparentLocation string

	// UploadProgressCallback is called every so often
	// to report the total bytes written to a single file and the current error,
	// including 'io.EOF'.
	UploadProgressCallback func(uint64, error)

	// Maps KeyIDs to shared secrets.
	// Here the latter are already decoded from base64 to binary.
	// Request verification is disabled if this is empty.
	IncomingHmacSecrets     auth.HmacSecrets
	IncomingHmacSecretsLock sync.RWMutex

	// If false, this plugin returns HTTP Errors.
	// If true, passes the given request to the next middleware
	// which could respond with an Error of its own, poorly obscuring where this plugin is used.
	SilenceAuthErrors bool

	// The user must set a "flag of shame" for sites that don't use TLS with 'upload'. (read-only)
	// This keeps track of whether said flags has been set.
	AcknowledgedNoTLS bool

	// This basically disables everything except POST and PUT.
	DisableWebdav bool

	// Set this to reject any non-conforming filenames.
	UnicodeForm *struct{ Use norm.Form }

	// Limit the acceptable alphabet(s) for filenames by setting this value.
	RestrictFilenamesTo []*unicode.RangeTable

	// Append '_' and a randomized suffix of that length.
	RandomizedSuffixLength uint32
}

// HandlerConfiguration is the result of directives found in a 'Caddyfile'.
//
// Can be modified at runtime, except for values that are marked as 'read-only'.
type HandlerConfiguration struct {
	// Prefixes on which Caddy activates this plugin (read-only).
	//
	// Order matters because scopes can overlap.
	PathScopes []string

	// Maps scopes (paths) to their own and potentially differently configurations.
	Scope map[string]*ScopeConfiguration
}

func parseCaddyConfig(c *caddy.Controller) (*HandlerConfiguration, error) {
	siteConfig := &HandlerConfiguration{
		PathScopes: make([]string, 0, 1),
		Scope:      make(map[string]*ScopeConfiguration),
	}

	for c.Next() {
		config := ScopeConfiguration{}
		config.TimestampTolerance = 1 << 2
		config.IncomingHmacSecrets = make(auth.HmacSecrets)
		config.UploadProgressCallback = noopUploadProgressCallback

		scopes := c.RemainingArgs() // most likely only one path; but could be more
		if len(scopes) == 0 {
			return siteConfig, c.ArgErr()
		}
		siteConfig.PathScopes = append(siteConfig.PathScopes, scopes...)

		for c.NextBlock() {
			key := c.Val()
			switch key {
			case "to":
				if !c.NextArg() {
					return siteConfig, c.ArgErr()
				}
				// must be a directory
				writeToPath := c.Val()
				finfo, err := os.Stat(writeToPath)
				if err != nil {
					return siteConfig, c.Err(err.Error())
				}
				if !finfo.IsDir() {
					return siteConfig, c.ArgErr()
				}
				config.WriteToPath = writeToPath
			case "promise_download_from":
				if !c.NextArg() {
					return siteConfig, c.ArgErr()
				}
				config.ApparentLocation = c.Val()
			case "hmac_keys_in":
				keys := c.RemainingArgs()
				if len(keys) == 0 {
					return siteConfig, c.ArgErr()
				}
				err := config.IncomingHmacSecrets.Insert(keys)
				if err != nil {
					return siteConfig, c.Err(err.Error())
				}
			case "timestamp_tolerance":
				if !c.NextArg() {
					return siteConfig, c.ArgErr()
				}
				s, err := strconv.ParseUint(c.Val(), 10, 32)
				if err != nil {
					return siteConfig, c.Err(err.Error())
				}
				if s > 60 { // someone configured a ridiculously high exponent
					return siteConfig, c.Err("we're sorry, but by this time Sol has already melted Terra")
				}
				if s > 32 {
					return siteConfig, c.Err("must be ≤ 32")
				}
				config.TimestampTolerance = 1 << s
			case "silent_auth_errors":
				config.SilenceAuthErrors = true
			case "yes_without_tls":
				config.AcknowledgedNoTLS = true
			case "disable_webdav":
				config.DisableWebdav = true
			case "filenames_form":
				if !c.NextArg() {
					return siteConfig, c.ArgErr()
				}
				switch c.Val() {
				case "NFC":
					config.UnicodeForm = &struct{ Use norm.Form }{Use: norm.NFC}
				case "NFD":
					config.UnicodeForm = &struct{ Use norm.Form }{Use: norm.NFD}
				case "none":
					// nop
				default:
					return siteConfig, c.ArgErr()
				}
			case "filenames_in":
				blocks := c.RemainingArgs()
				if len(blocks) == 0 {
					return siteConfig, c.ArgErr()
				}
				v, err := ParseUnicodeBlockList(strings.Join(blocks, " "))
				if err != nil {
					return siteConfig, c.Err(err.Error())
				}
				if v == nil {
					return siteConfig, c.ArgErr()
				}
				config.RestrictFilenamesTo = []*unicode.RangeTable{v}
			case "random_suffix_len":
				if !c.NextArg() {
					return siteConfig, c.ArgErr()
				}
				l, err := strconv.ParseUint(c.Val(), 10, 32)
				if err != nil {
					return siteConfig, c.Err(err.Error())
				}
				config.RandomizedSuffixLength = uint32(l)
			}
		}

		if config.WriteToPath == "" {
			return siteConfig, c.Errf("The destination path 'to' is missing")
		}

		for idx := range scopes {
			siteConfig.Scope[scopes[idx]] = &config
		}
	}

	return siteConfig, nil
}

// noopUploadProgressCallback NOP-functor, set as default.
func noopUploadProgressCallback(bytesWritten uint64, err error) {
	// I want to become a closure that updates a data structure.
}
