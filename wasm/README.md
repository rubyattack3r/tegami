# Browser Argon2id

The committed `internal/envelope/assets/argon2.wasm` implements Argon2id v1.3
using Monocypher 4.0.3. The Go CLI uses `golang.org/x/crypto/argon2` instead.
Both use 65536 KiB, three passes, one lane, and a 32-byte output. Delivery
parameters are checked before allocating memory or running either KDF.

## Source provenance

`monocypher/monocypher.c`, `monocypher/monocypher.h`, and `LICENCE.md` are from
the [Monocypher 4.0.3 release](https://monocypher.org/download/monocypher-4.0.3.tar.gz).
The C and header files match the upstream `4.0.3` Git tag except for the
release's substitution of `__git__` with `4.0.3` on their first line.
They are otherwise unchanged. The project uses the CC0-1.0 license option.

SHA-256 of the vendored files:

```text
57eb914fc88136119bd41655cccb8c250048bf54d470540625186f8ab16f64be  monocypher.c
c494da712122da7ff679fdcf318a5317e84972b6c950fe9d896212947797facd  monocypher.h
5f8360e4c06ddcc584bdb4b210c6af824c4bb301e6a9a521869b6d90795ca4b3  LICENCE.md
```

`argon2.c` is the small wrapper maintained here. It exports `argon2id` and
takes pointers to caller-owned buffers, including the work area. It needs
no allocator, C library, host imports, filesystem, or network. The production
JavaScript wrapper passes only fixed parameters. The more general WASM
export also permits the RFC vector, including its optional secret and
associated-data inputs, to be checked without changing the production KDF.

## Rebuild and verify

```sh
nix develop --command make wasm
nix develop --command make wasm-check
```

`flake.lock` pins the clang and lld used by these commands. The complete
compiler and linker flags live in `Makefile`: wasm32, optimization level 2,
bulk memory, freestanding mode, no standard library or entrypoint, a 65 MiB
linear-memory maximum, garbage collection of unused functions, and stripped
metadata. No timestamps or build paths enter the module. `make check`
rebuilds it and compares it byte for byte with the committed module. Ordinary
Go builds embed the committed bytes and need no WASM compiler.

`crypto.js` decodes the embedded module without fetching anything, checks it
against the SHA-256 that Go writes into the CSP-hashed script, caches only
its compiled code, creates a fresh instance for each derivation, and
wipes its memory after importing the wrapping key into Web Crypto. The page
CSP allows WASM compilation with `wasm-unsafe-eval` while blocking JavaScript
string evaluation and all connections.

`make browser-test` (or `make browser-check`, interactively) checks the full
Argon2id test vector from
[RFC 9106 §5.3](https://www.rfc-editor.org/rfc/rfc9106.html#section-5.3),
including its four lanes, secret, and associated data. It then exercises the
production parameter set through CLI/Studio round trips. The Go tests also
pin the production KDF to the same known output as Monocypher.
