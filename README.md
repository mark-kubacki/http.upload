Upload for HTTP servers
=======================

[![PkgGoDev](https://pkg.go.dev/badge/blitznote.com/src/http.upload/v4)](https://pkg.go.dev/blitznote.com/src/http.upload/v4)

Enables you to upload files, such as build artifacts, to your HTTP server instance.

Use this with the built-in authentication, or a different authentication plugin such as **jwt**.

Licensed under a [BSD-style license](LICENSE).

Highlights
----------

 * uses HTTP PUT and POST for uploads
 * supports HTTP MOVE and DELETE
 * imposes limits on filenames:
   * rejects those that are not conforming to Unicode NFC or NFD
   * rejects any comprised of unexpected alphabets ϟ(ツ)╯
 * checks request authorization using scheme **Signature**
 * can be configured to silently discard unauthorized requests
 * (Linux only) files appear after having been written completely, not before
 * limits to file- and transaction sizes independent from any *transport encoding*

Versions
--------

Version | Change
------- | ------
/v3     | The last version to come with wrappers for some webservers.
/v4     | Call to `http.Handler` is henceforth the preferred way to use this.
/v5     | No longer limited to local filesystems as backend, the syntax for `to` (`WriteToPath`) has changed.

Warnings
--------

Use TLS when uploading, or your data and authorization tokens can be intercepted by
third parties and used against you.

This plugin reveals some errors thrown by your filesystem implementation to the uploader,
for example about insufficient space on the target device..

The way Golang currently decodes *MIME Multipart* (which is used with POST requests) results
in any files you are uploading being held in memory for the duration of the upload.

Configuration Syntax
--------------------

Let this be a legible shorthand for instances of `ScopeConfiguration`:

```
upload <path> {
	to                    "<directory>"

	enable_webdav
	filenames_form        <none|NFC|NFD>
	filenames_in          <u0000-uff00> [<u0000-uff00>| …]
	random_suffix_len     0..N
	promise_download_from <path>

	max_filesize          0..N
	max_transaction_size  0..N

	hmac_keys_in          <keyid_0=base64(binary)> [<keyid_1=base64(binary)>| …]
	timestamp_tolerance   <0..32>
	silent_auth_errors
}
```

These settings are required:

 * **path** is the *scope* below which the plugin will react to any *uploads*.
   It will be stripped and no part of any resulting file and directories.
 * **to** is an existing target directory. Must be a quoted absolute path.
   When using Linux it is recommended to place this on a filesystem which supports
   **O_TMPFILE**, such as (but not limited to) *ext4* or *XFS*.

These are optional:

 * **enable_webdav**: Enables other methods than POST and PUT,
   especially MOVE and DELETE. Is a flag and has no parameters.  
   (`disable_webdav` will no longer be recognized because it's the new default.)
 * **filenames_form**: if given, filenames and directories that are not 
   conforming to Unicode NFC or NFD will be rejected.  
   Set this to one of either values when you get errors indicating that your filesystem
   does not convert names properly. (If in doubt, go with NFC; on Mac PCs with NFD.)  
   The default is to not enforce anything.
 * **filenames_in** allows you to limit filenames to specified Unicode ranges.
   The ranges' bounds must be given in hexadecimal, and start with letter ```u```.  
   Use this setting to prevent users from uploading files in, for example, Cyrillic
   when expect Latin and/or Chinese alphabets only.
 * **random_suffix_len**, if > 0, will result in all filenames getting a randomized suffix.  
   The suffix will start in a `_` (underscore letter) and placed before any extension.  
   For example, `image.png` will be written as `image_a107xm.png` with configuration value *6*.
   Utilize `promise_download_from` to get the resulting filename.  
   The default is 0 for *off*.
 * **promise_download_from** is a string that represents an *URI reference*, such as a path.  
   It will be used to indicate where the uploaded file can be downloaded,
   by responding with HTTP header `Location` (multiple times if need be) for all received files.  
   You will most probably want to set this to the *upload `path`*.  
   The default value is "", which means no HTTP header `Location` will be sent.

 * By **max_filesize** you can limit the size of individual files.
   Unless set to `0`, which means "unlimited" and is the default value, it's in *bytes*.
 * **max_transaction_size** is similar, but applies to uploads of one or more file in one request.
   For example, when using *MIME Multipart* uploads.  
   The behaviour with `max_filesize > max_transaction_size` is currently undefined;
   set *max_transaction_size* to a multiple of *max_filesize*.

Some transfer encodings, such as **base64**, know comments. Those, or super-long headers and the such,
can be exploited to transfer many more bytes than for example *max_transaction_size* would otherwise allow.
Mitigate this by utilizing a different plugin, **http.limits**, which counts incoming bytes
ignorant of any encoding. Set a limit of about 1.4× to 2.05× the *max_transaction_size*.  
This plugin writes files blockwise for a better performance. Limits are rounded up by a few kilobytes to
the next full block.

Optional, but required if you want to use the built-in authorization feature:

 * **hmac_keys_in** is a space-delimited list of `username → shared secret` associations.  
   The latter is binary data, encoded using *base64*, with a recommended length of 32 octets.
 * **timestamp_tolerance** sets the validity of a request with *authorization*,
   and is used to account for clock drift difference between the uploader's and the server's computer.  
   It is given as power of 2, and its default value is 2 (as in: ± four seconds = 1<<2 = 2**2).
   Set this to 1 or 0 with reliably synchronized clocks.
 * **silent_auth_errors**, if set the plugin's built-in authorization will return no HTTP errors of its own.  
   Instead, the request will be handed over to the next middleware, which
   then will most probably return a HTTP error.
   This is a cheap way to obscure that your site accepts uploads.

Tutorial
--------

Setup a minimal configuration like this, or `go run example.go &` after copying it into a separate
directory and removing the line with `+build ignore` (mind the port number, which is `9000` there):

```
upload /web/path {
 	to "/var/tmp"
 	enable_webdav
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

This plugin comes with support for *request authorization* scheme **Signature**,
though does not support its *realms* or any other *algorithm* than **hmac-sha256**.  
It's a HMAC scheme with a pre-shared secret and nonce||timestamp.

Send header **Authorization** and two other, formatted like this, with every upload request:

```
Authorization: Signature keyId="(username)",algorithm="hmac-sha256",headers="timestamp token",signature="(see below)"
Timestamp: (current UNIX time)
Token: (a nonce)
```

You can generate new keys (passwords, pre-shared keys) using BASH and OpenSSL, and encode them to *base64*:

```bash
SECRET="$(openssl rand -base64 32)"

# printf "%s\n" "${SECRET}"
# TWF0dCBIb2x0IGRvZXNuJ3QgdXBkYXRlIGhpcyBNYWM=
```

A full script for uploading something would be:

```bash
#!/bin/bash
# hmac_keys_in mark=Z2VoZWlt
#
UPLOADER="mark"
SECRET="geheim"

TIMESTAMP="$(date --utc +%s)"
# length and contents are not important, "abcdef" would work as well
NONCE="$(cat /dev/urandom | tr -d -c '[:alnum:]' | head -c $(( 32 - ${#TIMESTAMP} )))"

SIGNATURE="$(printf "${TIMESTAMP}${NONCE}" \
             | openssl dgst -sha256 -hmac "${SECRET}" -binary \
             | openssl enc -base64)"

# order does not matter; any skipped fields in Authorization will be set to defaults
exec curl -T \
  --header "Timestamp: ${TIMESTAMP}" \
  --header "Token: ${NONCE}" \
  --header "Authorization: Signature keyId='${UPLOADER}',signature='${SIGNATURE}'" \
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

A host for Linux distribution packages can be more restrictive:

```
upload /binhost/gentoo {
	to "/var/portage/packages"
	filenames_in u0000–u007F
	timestamp_tolerance 0
}
tls {
	…
	clientcas /etc/ssl/dist-uploaders-CA.crt
}
```

… while someone in East Asia would share space on his blog with three friends like this:

```
upload /wp-uploads {
	to "/var/www/senpai/wp-uploads"
	enable_webdav
	max_filesize 16777216
	filenames_in u0000–u007F u0100–u017F u0391–u03C9 u2018–u203D u3000–u303f u3040–u309f u30a0–u30ff u4e00–9faf uff00–uffef

	timestamp_tolerance 3
	silent_auth_errors

	hmac_keys_in yui=eXVp hina=aGluYQ== olivia=b2xpdmlh james=amFtZXM=
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
