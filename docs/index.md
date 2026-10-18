# Tegami documentation

Tegami sends files encrypted with a password. The recipient opens them in a
browser, offline.

## Send and open files

- [Getting started](getting-started.md): download and verify Tegami, then send
  a first file.
- [Studio guide](studio.md): sending and opening files in the browser.
- [CLI reference](cli-reference.md): every command and option of `tegami`.

## Understand what is protected

- [Security model](security.md): what Tegami protects, what it doesn't, and
  what anyone holding a delivery can see.
- [File formats](formats.md): the `tegami/v1` specification, byte for byte.
- [Security policy](../SECURITY.md): how to report a vulnerability.

## Contribute

- [Developer documentation](development/index.md): setup, architecture, and
  releasing.

Every release reads and writes the `tegami/v1` format. A new format version
would be a breaking change, announced in the [changelog](../CHANGELOG.md).
