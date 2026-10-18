# Development setup

This guide is for contributors building and validating Tegami from a checkout.
To install Tegami or send a first file, use the user-facing
[Getting started](../getting-started.md) guide instead.

## Toolchains

Tegami supports two local development paths:

- Go, at the version declared in `go.mod`. The crypto modules are pinned in
  `go.mod` and `go.sum`; there are no JavaScript dependencies or bundler.
  The embedded Argon2id WASM module is committed, so building the CLI or
  Studio does not require a C toolchain.
- Nix with flakes enabled. `nix develop` provides Go, golangci-lint,
  actionlint, govulncheck, GoReleaser, VHS, clang, and lld at locked versions.

## Build

```bash
make build    # bin/tegami
make studio   # build/tegami.html
```

## Run the gate

```bash
nix develop --command make check
nix develop --command make browser-test
nix flake check
```

`make check` checks formatting, runs `go vet`, golangci-lint, actionlint, all
tests, builds `bin/tegami`, and checks that the committed WASM module rebuilds
byte for byte. The individual targets are also available:

```bash
make fmt-check
make vet
make lint
make test
make build
make wasm-check  # uses the Nix-pinned clang and lld
make vulncheck   # needs network access to the Go vulnerability database
```

Tests run offline. Among them, `TestPagesAllowOnlyTheirInlineCode` keeps each
page's CSP hashes in step with its inline code, `TestCatalogsMatch` keeps the
translations in step, and the round-trip tests check Go against the formats.

## Test the pages in a browser

Go tests cannot run the pages' JavaScript, so changes to `internal/envelope/assets`
need a browser check. Build the Studio and a CLI delivery:

```bash
make studio                                    # build/tegami.html
printf '%s\n' 'a long shared password' |
  go run ./cmd/tegami pack --input README.md --output build/delivery.html --format html
```

Open both files in Chromium, and in Firefox or Safari for the HTML paths. The
checklist in [`AGENTS.md`](../../AGENTS.md#changes-that-touch-the-pages) lists
what a page change must be checked against: both themes, both languages, a
Studio-made HTML delivery, a CLI delivery, and a streaming round trip.

`make browser-test` runs the integration checks in headless Chromium. It uses
`chromium` from your `PATH`; set `CHROMIUM` to use another Chromium or Chrome
binary. CI runs it on every pull request. To watch the checks instead, run
`make browser-check` and open the printed local URL in Chromium.

The checks run the real pages: the RFC 9106 Argon2id vector, CLI and Studio
round trips in both directions, rejected parameters and commitments, HTML
deliveries with hostile markup, and damage to the last streaming chunk. The
harness serves fixtures from a local server; the pages themselves still make
no network requests.

See [`wasm/README.md`](../../wasm/README.md) for the vendored source, compiler
flags, checksums, and reproducible module build.

## Record the terminal demo

The README's terminal demo is recorded from a real session with
[VHS](https://github.com/charmbracelet/vhs):

```bash
nix develop --command make demo
```

This builds `bin/tegami`, creates sample files under `bin/demo`, and runs
[`docs/demo.tape`](../demo.tape), which writes `docs/assets/terminal.gif` and a
still `docs/assets/terminal.png`. Codenames and passwords are random, so every
recording differs; commit both files together.

## Add icons

Icons are Hugeicons in an inline sprite. Convert new ones with:

```bash
curl -sL https://registry.npmjs.org/@hugeicons/core-free-icons/-/core-free-icons-4.3.5.tgz | tar xz
go run ./scripts/icons package Translate=language
```

Paste the printed `<symbol>` into the sprite in `studio.html` or `page.html`,
and keep [`THIRD_PARTY_NOTICES.md`](../../THIRD_PARTY_NOTICES.md) current.

## Release

See [Releasing](releasing.md).
