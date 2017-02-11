// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package protofile implements temporary files that don't appear in
// filesystem namespace until closed.
//
// Unfortunately this only works on Linux with flag O_TMPFILE.
// In other cases a graceful degradiation is attempted,
// which results in the well-known dot-files (like ".gitignore").
//
// Unlike with traditional files with {CreateNew, Write, Close},
// proto files have a lifecycle {IntentNew, Write, Persist or Zap}.
// While a traditional file "emerges" the instant it is created with a name,
// "proto files" are named only after having been "persisted" (which closes them).
//
// Streaming of file contents is currently not supported.
package protofile // import "blitznote.com/src/caddy.upload/protofile"
