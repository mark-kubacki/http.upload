package upload // import "blitznote.com/src/caddy.upload"

import (
	"encoding/base64"
	"errors"
	"os"
	"strconv"
	"strings"
	"sync"
	"unicode"

	"github.com/mholt/caddy/caddy/setup"
	"github.com/mholt/caddy/middleware"
	"golang.org/x/text/unicode/norm"
)

// Setup configures an UploadHander instance.
//
// This is called by Caddy.
func Setup(c *setup.Controller) (middleware.Middleware, error) {
	config, err := parseCaddyConfig(c)
	if err != nil {
		return nil, err
	}

	if !config.siteHasTLS {
		if c.Dispenser.File() == "Testfile" {
			goto pass
		}
		for _, host := range []string{"127.0.0.1", "localhost", "[::1]", "::1"} {
			if c.Config.Host == host || strings.HasPrefix(c.Config.Host, host) {
				goto pass
			}
		}

		for _, scopeConf := range config.Scope {
			if !scopeConf.AcknowledgedNoTLS {
				return nil, c.Err("You are using plugin 'upload' on a site without TLS.")
			}
		}
	}

pass:
	return func(next middleware.Handler) middleware.Handler {
		return &Handler{
			Next:   next,
			Config: *config,
		}
	}, nil
}

// ScopeConfiguration represents the settings for a scope (path).
type ScopeConfiguration struct {
	// How big a difference between 'now' and the provided timestamp do we tolerate?
	// In seconds. Due to possible optimizations this should be an order of 2.
	// A reasonable default is 1<<2.
	TimestampTolerance uint64

	// the upload destination
	WriteToPath string

	// Already decoded. Request verification is disabled if this is empty.
	IncomingHmacSecrets     map[string][]byte
	IncomingHmacSecretsLock sync.RWMutex

	// A skilled attacked will monitor traffic, and timings.
	// Enabling this merely obscures the path.
	SilenceAuthErrors bool

	// The user must set a "flag of shame" for sites that don't use TLS with 'upload'. (read-only)
	AcknowledgedNoTLS bool

	// Set this to reject any filenames that are not normalized accordingly.
	UnicodeForm *struct{ Use norm.Form }

	// Limit the acceptable alphabet(s) for filenames by setting this value.
	RestrictFilenamesTo []*unicode.RangeTable
}

// HandlerConfiguration is the result of directives found in a 'Caddyfile'.
//
// Can be modified at runtime, except for values that are marked as 'read-only'.
type HandlerConfiguration struct {
	// Prefixes on which Caddy activates this plugin (read-only).
	//
	// Order matters because scopes can overlap.
	PathScopes []string

	// every scope (path) can be configured differently
	Scope map[string]*ScopeConfiguration

	// Used on initialization to effect a reminder to the user to use TLS on sites with 'upload'.
	siteHasTLS bool
}

func parseCaddyConfig(c *setup.Controller) (*HandlerConfiguration, error) {
	siteConfig := &HandlerConfiguration{
		PathScopes: make([]string, 0, 1),
		Scope:      make(map[string]*ScopeConfiguration),
		siteHasTLS: c.Config != nil && c.Config.TLS.Enabled,
	}

	for c.Next() {
		config := ScopeConfiguration{}
		config.TimestampTolerance = 1 << 2
		config.IncomingHmacSecrets = make(map[string][]byte)

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
			case "hmac_keys_in":
				keys := c.RemainingArgs()
				if len(keys) == 0 {
					return siteConfig, c.ArgErr()
				}
				err := config.AddHmacSecrets(keys)
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

// AddHmacSecrets decodes the arguments and adds/updates them to the existing HMAC shared secrets.
//
// The format of each element is:
//  key=(base64(value))
//
// For example:
//  hmac-key-1=yql3kIDweM8KYm+9pHzX0PKNskYAU46Jb5D6nLftTvo=
//
// The first tuple that cannot be decoded is returned as error string.
func (c *ScopeConfiguration) AddHmacSecrets(tuples []string) (err error) {
	c.IncomingHmacSecretsLock.Lock()
	defer c.IncomingHmacSecretsLock.Unlock()

	for idx := range tuples {
		p := strings.SplitN(tuples[idx], "=", 2)
		if len(p) != 2 {
			return errors.New(tuples[idx])
		}
		binary, err := base64.StdEncoding.DecodeString(p[1])
		if err != nil {
			return errors.New(tuples[idx])
		}
		c.IncomingHmacSecrets[p[0]] = binary
	}

	return
}
