// Package protofile implements temporary files that don't appear in
// filesystem namespace until closed.
//
// Unfortunately this only works on most, not all, Linux systems.
// For example, ancient Linux versions don't know flag O_TMPFILE.
// In such and similar cases a graceful degradiation is attempted,
// which worst-case results in the well-known dot-files (like ".gitignore").
//
// Unlike with traditional files with {CreateNew, Write, Close},
// these have a lifecycle described by {IntentNew, Write, Persist or Zap}.
// While a traditional file "emerges" the instant it is created with a name,
// "proto files" are named only after having been "persisted" (which closes them).
//
// Streaming of file contents is currently not supported.
package protofile // import "blitznote.com/src/caddy.upload/protofile"
