Upload for Caddy
================

[![Build Status](https://semaphoreci.com/api/v1/wmark/caddy-upload/branches/master/badge.svg)](https://semaphoreci.com/wmark/caddy-upload)
[![Windows Build Status](https://img.shields.io/appveyor/ci/wmark/caddy-upload.svg?style=flat&label=windows+build)](https://ci.appveyor.com/project/wmark/caddy-upload/branch/master)
[![Coverage Status](https://coveralls.io/repos/github/wmark/caddy.upload/badge.svg?branch=master)](https://coveralls.io/github/wmark/caddy.upload?branch=master)
[![GoDoc](https://godoc.org/blitznote.com/src/caddy.upload?status.svg)](https://godoc.org/blitznote.com/src/caddy.upload)

Enables you to upload files, such as build artifacts, to your Caddyserver instance.

Use this with the built-in authentication, or a different authentication plugin such as **jwt**.

Licensed under a [BSD-style license](LICENSE).

Highlights
----------

 * uses HTTP PUT and POST for uploads
 * supports HTTP MOVE and DELETE
 * imposes limits on filenames:
   * rejects those that are not conforming to Unicode NFC or NFD
   * rejects any comprised of unexpected alphabets ϟ(ツ)╯
 * checks request authorization using scheme Signature
 * can be configured to silently discard unauthorized requests
 * (Linux only) files appear after having been written completely, not before
 * works with Caddy's browse plugin

Warnings
--------

Unless you use TLS with connections to your upload destination
your data and any authorization headers can be intercepted by third parties.
The authorization header is valid for some seconds and can be replayed:
Used by a third party to upload files on your behalf.

This plugin echoes errors to the uploader that are thrown by your filesystem implementation.

The way Golang currently decodes *MIME Multipart* (which is used with POST requests) results
in any files you are uploading being held in memory for the duration of the upload.

Configuration Syntax
--------------------

```
upload <path> {
	to                  "<directory>"
	yes_without_tls

	filenames_form      <none|NFC|NFD>
	filenames_in        <u0000-uff00> [<u0000-uff00>| …]

	hmac_keys_in        <keyid_0=base64(binary)> [<keyid_1=base64(binary)>| …]
	timestamp_tolerance <0..32>
	silent_auth_errors
}
```

These settings are required:

 * **path** is the *scope* below which the plugin will react to any upload requests.
   It will be stripped and no part of any resulting files and directories.
 * **to** is an existing target directory. Must be in quotes.
   When using Linux it is recommended to place this on a filesystem which supports
   **O_TMPFILE**, such as (but not limited to) *ext4* or *XFS*.

These are optional:

 * **yes_without_tls** must be set if the plugin is used on a location or host without TLS.
 * **filenames_form**: if given, filenames (this includes directories) that are not 
   conforming to Unicode NFC or NFD will be rejected.
   Set this to one of said values when you get errors indicating that your filesystem
   does not convert names properly. (If in doubt, go with NFC; on Mac PCs with NFD.)
   The default is to not enforce anything.
 * **filenames_in** allows you to limit filenames to specified Unicode ranges.
   The ranges' bounds must be given in hexadecimal and start with letter ```u```.
   Use this setting to prevent users from uploading files in, for example, Cyrillic
   when all you like to see is Latin and/or Chinese alphabets.

Unless you have decided to use a different authentication and/or authorization plugin:

 * **hmac_keys_in** is a space-delimited list of id=value elements.
   The *id* is the KeyID, which could identify the uploading entity and which
   is a reference to a shared secret, the *value*.
   The latter is binary data, encoded using *base64*, with a recommended length of 32 octets.
 * **timestamp_tolerance** sets the validity of a request with authorization,
   and is used to account for clock drift difference between the
   uploader's and the server's computer.
   Always being an order of 2 its default value is 2 (as in: ± two seconds).
   Set this to 1 or 0 with reliably synchronized clocks.
 * **silent_auth_errors** results in the plugin returning no HTTP Errors.
   Instead, the request will be handed over to the next middleware, which
   then will most probably return a HTTP Error of its own.
   This is a cheap way to obscure that your site accepts uploads.

Tutorial
--------

Add to your Caddyfile:

```
upload /web/path {
 	to "/var/tmp"
}
```

… and upload one file:

```bash
# HTTP PUT
curl \
  -T /etc/os-release \
  https://127.0.0.1/web/path/from-release
```

… or more files in one go (sub-directories will be created as needed):

```bash
# HTTP POST
curl \
  -F gitconfig=@.gitconfig \
  -F id_ed25519.pub=@.ssh/id_ed25519.pub \
  https://127.0.0.1/web/path/
```

… which you then can move and delete like this:

```bash
# MOVE is 'mv'
curl -X MOVE \
  -H "Destination: /web/path/to-release" \
  https://127.0.0.1/web/path/from-release

# DELETE is 'rm -r'
curl -X DELETE \
  https://127.0.0.1/web/path/to-release
```

Authorization: Signature
------------------------

Although this plugin comes with support for request authorization using scheme **Signature**,
the only supported *algorithm* is **hmac-sha256** and there are no *realms*.

A pre-shared secret, referenced by **keyId**,
is used together with a nonce—the concatenation of the current Unix time and a free-form string—
in a HMAC scheme.
In the end a header **Authorization** is sent formatted like this along with the two latter values:

```
Authorization: Signature keyId="(key_id)",algorithm="hmac-sha256",headers="timestamp token",signature="(see below)"
Timestamp: (current UNIX time)
Token: (some chars, to promote the timestamp to a full nonce)
```

You can generate new keys as easily as this using BASH:

```bash
SECRET="$(openssl rand -base64 32)"

# printf "%s\n" "${SECRET}"
# TWF0dCBIb2x0IGRvZXNuJ3QgdXBkYXRlIGhpcyBNYWM=
```

A full script for uploading something would be:

```bash
# hmac_keys_in mark=Z2VoZWlt
#
KEYID="mark"
SECRET="geheim"

TIMESTAMP="$(date --utc +%s)"
# length and contents are not important, "abcdef" would work as well
TOKEN="$(cat /dev/urandom | tr -d -c '[:alnum:]' | head -c $(( 32 - ${#TIMESTAMP} )))"

SIGNATURE="$(printf "${TIMESTAMP}${TOKEN}" \
             | openssl dgst -sha256 -hmac "${SECRET}" -binary \
             | openssl enc -base64)"

# order does not matter; any skipped fields in Authorization will be set to defaults
curl -T \
	--header "Timestamp: ${TIMESTAMP}" \
	--header "Token: ${TOKEN}" \
	--header "Authorization: Signature keyId='${KEYID}',signature='${SIGNATURE}'" \
	"<filename>" "<url>"
```

Configuration Examples
----------------------

A host used by someone in Central and West Europe would be configured like this
to accept filenames in Latin with some Greek runes and a few mathematical symbols:

```
upload /college/curriculum {
	to "/home/ellen_baker/inbox"
	filenames_form NFC
	filenames_in u0000–u007F u0100–u017F u0391–u03C9 u2018–u203D u2152–u217F
}
```

A host for Linux distribution packages would be more restrictive:

```
upload /binhost/gentoo {
	to "/var/portage/packages"
	filenames_in u0000–u007F
	timestamp_tolerance 0
}
```

… while someone in East Asia would share space on his blog with three friends:

```
upload /wp-uploads {
	to "/var/www/senpai/wp-uploads"
	filenames_in u0000–u007F u0100–u017F u0391–u03C9 u2018–u203D u3000–u303f u3040–u309f u30a0–u30ff u4e00–9faf uff00–uffef
	hmac_keys_in yui=eXVp hina=aGluYQ== olivia=b2xpdmlh james=amFtZXM=
	timestamp_tolerance 3
	silent_auth_errors
}
```

See Also
--------

You can find a very nice overview of Unicode Character ranges here:
http://jrgraphix.net/research/unicode_blocks.php

Here is the official list of Unicode blocks:
http://www.unicode.org/Public/UCD/latest/ucd/Blocks.txt

For *Authorization: Signature* please see:

 * https://tools.ietf.org/html/draft-cavage-http-signatures-05
 * github.com/joyent/gosign is an implementation in Golang,
 * github.com/joyent/node-http-signature for Node.js.
