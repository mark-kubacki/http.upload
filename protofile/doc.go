// The need to write file contents first, and then creating
// that file, is recognized by this package.
//
// Lifecycle {CreateNew, Write, Close} becomes {IntentNew, Write, Persist or Zap}.
// A file emerges into observable namespace only after it has been persisted.
//
// Ideally, at least, due to limitations of operating- and filesystems.
// For example, in case of older Linux versions of if the filesystem does not
// support O_TMPFILE, graceful degradiation is attempted up to the use of
// of dot-files (like .gitignore).
//
// Streaming of file contents is currently not supported.
package protofile // import "blitznote.com/src/caddy.upload/protofile"
