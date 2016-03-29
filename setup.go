package upload // import "blitznote.com/src/caddy.upload"

import (
	"encoding/base64"
	"errors"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/mholt/caddy/caddy/setup"
	"github.com/mholt/caddy/middleware"
)

// Configures an UploadHander instance.
// This is called by Caddy every time the corresponding directive is used.
func Setup(c *setup.Controller) (middleware.Middleware, error) {
	config, err := parseCaddyConfig(c)
	if err != nil {
		return nil, err
	}

	return func(next middleware.Handler) middleware.Handler {
		return &UploadHandler{
			Next:   next,
			Config: config,
		}
	}, nil
}

func parseCaddyConfig(c *setup.Controller) (UploadHandlerConfiguration, error) {
	var config UploadHandlerConfiguration
	config.TimestampTolerance = 1 << 2
	config.IncomingHmacSecrets = make(map[string][]byte)

	for c.Next() {
		config.PathScopes = c.RemainingArgs() // most likely only one path; but could be more

		for c.NextBlock() {
			key := c.Val()
			switch key {
			case "to":
				if !c.NextArg() {
					return config, c.ArgErr()
				}
				// must be a directory
				writeToPath := c.Val()
				finfo, err := os.Stat(writeToPath)
				if err != nil {
					return config, c.Err(err.Error())
				}
				if !finfo.IsDir() {
					return config, c.ArgErr()
				}
				config.WriteToPath = writeToPath
			case "hmac_keys_in":
				keys := c.RemainingArgs()
				if len(keys) == 0 {
					return config, c.ArgErr()
				}
				err := config.AddHmacSecrets(keys)
				if err != nil {
					return config, c.Err(err.Error())
				}
			case "timestamp_tolerance":
				if !c.NextArg() {
					return config, c.ArgErr()
				}
				s, err := strconv.ParseUint(c.Val(), 10, 32)
				if err != nil {
					return config, c.Err(err.Error())
				}
				if s > 60 { // someone configured a ridiculously high exponent
					return config, c.Err("we're sorry, but by this time Sol has already melted Terra")
				}
				if s > 32 {
					return config, c.Err("must be ≤ 32")
				}
				config.TimestampTolerance = 1 << s
			case "silent_auth_errors":
				config.SilenceAuthErrors = true
			}
		}
	}

	if config.WriteToPath == "" {
		return config, c.Errf("The destination path 'to' is missing")
	}
	if config.PathScopes == nil || len(config.PathScopes) == 0 {
		return config, c.ArgErr()
	}
	return config, nil
}

// State of UploadHandler, result of directives found in a 'Caddyfile'.
type UploadHandlerConfiguration struct {
	// How big a difference between 'now' and the provided timestamp do we tolerate?
	// In seconds. Due to possible optimizations this should be an order of 2.
	// A reasonable default is 1<<2.
	TimestampTolerance uint64

	// prefixes on which Caddy activates this plugin (read-only)
	PathScopes []string

	// the upload destination
	WriteToPath string

	// Already decoded. Request verification is disabled if this is empty.
	IncomingHmacSecrets     map[string][]byte
	IncomingHmacSecretsLock sync.RWMutex

	// A skilled attacked will monitor traffic, and timings.
	// Enabling this merely obscures the path.
	SilenceAuthErrors bool
}

// Decodes the arguments and adds/updates them to the existing HMAC shared secrets.
//
// The format of each element is:
//  key=(base64(value))
//
// For example:
//  hmac-key-1=yql3kIDweM8KYm+9pHzX0PKNskYAU46Jb5D6nLftTvo=
//
// The first tuple that cannot be decoded is returned as error string.
func (c UploadHandlerConfiguration) AddHmacSecrets(tuples []string) (err error) {
	c.IncomingHmacSecretsLock.Lock()
	defer c.IncomingHmacSecretsLock.Unlock()

	for idx, _ := range tuples {
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
