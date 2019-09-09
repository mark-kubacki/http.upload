// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// +build caddyserver0.9 caddyserver1.0

package upload

import (
	"net/http"
	"os"
	"strconv"
	"strings"
	"unicode"

	"github.com/caddyserver/caddy"
	"github.com/caddyserver/caddy/caddyhttp/httpserver"
	"golang.org/x/text/unicode/norm"
)

func init() {
	caddy.RegisterPlugin("upload", caddy.Plugin{
		ServerType: "http",
		Action:     Setup,
	})
}

// Setup configures an UploadHander instance.
//
// This is called by Caddy as consequence of invoking `caddy.RegisterPlugin` in init.
func Setup(c *caddy.Controller) error {
	config, err := parseCaddyConfig(c)
	if err != nil {
		return err
	}

	site := httpserver.GetConfig(c)
	site.AddMiddleware(func(next httpserver.Handler) httpserver.Handler {
		return &Handler{
			Next:   next,
			Config: *config,
		}
	})

	return nil
}

// HandlerConfiguration is the result of directives found in a 'Caddyfile'.
//
// Can be modified at runtime, except for values that are marked as 'read-only'.
//
// The same instance can be used to serve multiple paths, therefore we go through this struct
// to figure out the applicable configuration.
type HandlerConfiguration struct {
	// Prefixes on which Caddy activates this plugin (read-only).
	//
	// Order matters because scopes can overlap.
	PathScopes []string

	// Maps scopes (paths) to their own and potentially differently configurations.
	Scope map[string]*ScopeConfiguration
}

// Handler represents a configured instance of this plugin for uploads.
type Handler struct {
	Next   httpserver.Handler
	Config HandlerConfiguration
}

// ServeHTTP adapts the actual handler.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) (int, error) {
	// iterate over the scopes in the order they have been defined
	for _, scope := range h.Config.PathScopes {
		if httpserver.Path(r.URL.Path).Matches(scope) {
			config := h.Config.Scope[scope]
			return h.serveHTTP(w, r,
				scope, config,
				h.Next.ServeHTTP,
			)
		}
	}
	return h.Next.ServeHTTP(w, r)
}

func parseCaddyConfig(c *caddy.Controller) (*HandlerConfiguration, error) {
	siteConfig := &HandlerConfiguration{
		PathScopes: make([]string, 0, 1),
		Scope:      make(map[string]*ScopeConfiguration),
	}

	for c.Next() {
		config := NewDefaultConfiguration("")

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
			case "max_filesize":
				if !c.NextArg() {
					return siteConfig, c.ArgErr()
				}
				s, err := strconv.ParseUint(c.Val(), 10, 64)
				if err != nil {
					return siteConfig, c.Err(err.Error())
				}
				config.MaxFilesize = s
			case "max_transaction_size":
				if !c.NextArg() {
					return siteConfig, c.ArgErr()
				}
				s, err := strconv.ParseUint(c.Val(), 10, 64)
				if err != nil {
					return siteConfig, c.Err(err.Error())
				}
				config.MaxTransactionSize = s
			case "silent_auth_errors":
				config.SilenceAuthErrors = true
			case "yes_without_tls":
				// deprecated
				// nop
			case "enable_webdav":
				config.EnableWebdav = true
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
			default:
				return siteConfig, c.ArgErr()
			}
		}

		if config.WriteToPath == "" {
			return siteConfig, c.Errf("The destination path 'to' is missing")
		}

		for idx := range scopes {
			siteConfig.Scope[scopes[idx]] = config
		}
	}

	return siteConfig, nil
}
