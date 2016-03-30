// Package upload contains a HTTP handler for Caddy,
// which provides facilities for uploading files.
//
// If the operating- and filesystem supports it,
// files will not appear in the observable namespace before they have been written and closed.
// This is important with system daemons which monitor a set of paths and
// trigger actions in the advent of new files:
// For example, with the effect of uploading said files to other locations (mirrors).
//
// The client is expected to authenticate requests
// by sending a header "Authorization" formatted like this:
//
//  Authorization: Signature keyId="(key_id)",algorithm="hmac-sha256",
//      headers="timestamp token",signature="(see below)"
//
// The first element in 'headers' must either be "timestamp" (recommended),
// or "date" referring to HTTP header "Date".
// github.com/joyent/gosign is an implementation in Golang,
// github.com/joyent/node-http-signature for Node.js.
//
// This is how you generate aforementioned 'signature' on the Linux shell:
//  secret="geheim"
//  timestamp="$(date --utc +%s)"
//  token="streng"
//
//  printf "${timestamp}${token}" \
//  | openssl dgst -sha256 -hmac "${secret}" -binary \
//  | openssl enc -base64
//
// After that it's using, for example, 'curl' like this:
//  curl -T \
//    --header 'Authorization: …' \
//    <filename> <url>
package upload // import "blitznote.com/src/caddy.upload"
