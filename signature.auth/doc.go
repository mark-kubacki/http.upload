// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package auth implements authorization scheme Signature,
// which works using MIME headers.
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
//    --header 'Timestamp: …' --header 'Token: …' \
//    <filename> <url>
package auth // import "blitznote.com/src/http.upload/v3/signature.auth"
