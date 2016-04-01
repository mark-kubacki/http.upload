Upload for Caddy
================

[![Build Status](https://semaphoreci.com/api/v1/wmark/caddy-upload/branches/master/badge.svg)](https://semaphoreci.com/wmark/caddy-upload)
[![Coverage Status](https://coveralls.io/repos/github/wmark/caddy.upload/badge.svg?branch=master)](https://coveralls.io/github/wmark/caddy.upload?branch=master)
[![GoDoc](https://godoc.org/blitznote.com/src/caddy.upload?status.svg)](https://godoc.org/blitznote.com/src/caddy.upload)

Enables you to upload files, such as build artifacts, to your Caddyserver instance.

Licensed under a [BSD-style license](LICENSE).

Warnings
--------

Unless you use TLS to contact the upload destination (your host running Caddy)
your data and any authorization headers can be intercepted by third parties.
The authorization header is valid for some seconds, determined by
```timestamp_tolerance```, and can replayed: Used by a third party to upload
files on your behalf.

Limitations of the filesystem you want to write to are not taken into account
by this plugin. Any errors are echoed back to the uploader.

The way Golang decodes MIME Multipart (which is used with POST requests) results
in any files you are uploading being held in memory for the duration of the upload.

Usage
-----

Add to your Caddyfile:

```
upload /web/path {
	to "/var/tmp"
	hmac_keys_in secretkey=TWFyaw==
}
```
