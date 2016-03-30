// Package protofile resulted from the need to postpone emerging of a file into
// observable namespace: It has to be written first.
//
// This alters its lifecycle from {CreateNew, Write, Close}
// resulting in {IntentNew, Write, Persist or Zap}.
// A file "appears" only after having been persisted.
//
// Due to limitations of operating- and filesystems that happens in most cases,
// not all:
// For example, ancient Linux versions don't know flag O_TMPFILE.
// A graceful degradiation is attempted in such cases, eventually resulting in
// the well-known dot-files (like ".gitignore").
//
// Streaming of file contents is currently not supported.
package protofile // import "blitznote.com/src/caddy.upload/protofile"
