# AGENTS.md — Tegami

Tegami encrypts files into deliveries that open offline in a browser: a
self-contained HTML page (up to 50 MiB) or a streaming `.tegami` blob (up to
3 GiB) opened with the Studio, `tegami.html`. Both use the `tegami/v1` schema.
The Go CLI and the browser pages implement the same formats.

## Boundaries

- Nothing leaves the device. Pages make no network requests, load nothing
  external, and keep `connect-src 'none'` in their Content-Security-Policy.
  Decrypted files are only offered as downloads, never rendered or run.
- Go and JavaScript must agree byte for byte. A change to a format, its
  additional authenticated data, a limit, or the filename rule lands in
  `internal/envelope/*.go` and `assets/*.js` together, with a round-trip test,
  and in `docs/formats.md`. Never weaken a check on one side only.
- Keep the formats stable. Existing deliveries must keep opening; a breaking
  change needs a new schema version.
- Filenames stay hidden until a delivery is unlocked. Before that, a delivery
  is known only by its codename and size. Don't add visible metadata, and
  don't name generated files after their contents.
- Codenames come from the envelope ID and `assets/codenames.json`, shared by
  Go and the Studio. The lists and their order are part of the format: never
  reorder or edit them; changing them needs a new schema version.
- Keep secrets out of deliveries and arguments. Passwords are read from
  standard input or a file, never embedded, logged, or put in the process
  list. Zero key material after use where the platform allows.
- Recipient-facing output does not name Tegami: the CLI's delivery page and
  the unlock view of a Studio-made HTML file. Only the Studio itself is
  branded.
- The Go crypto dependencies (`golang.org/x/crypto` and its `x/sys` dependency)
  are pinned in `go.mod` and `go.sum`. No JavaScript dependencies or bundler.
  The pages are plain HTML, CSS, and vanilla JavaScript, with vendored
  Monocypher compiled to embedded WebAssembly for Argon2id. `make wasm-check`
  must reproduce the committed module using the toolchain in `flake.lock`.
- `tegami/v1` deliveries use Argon2id with exactly 65536 KiB, three passes, and
  one lane, a content-key commitment, and names padded to 180 bytes. Reject
  any other KDF settings before deriving a key or allocating the WASM work
  area, and never add a second KDF path to v1; another KDF needs a new schema.

## Browser pages

- Pages live in `internal/envelope/assets/` and are embedded with `go:embed`.
  `studio.html` and `page.html` hold markers for the CSP, the one inline
  `<style>` (`base.css` plus the page's CSS), and the one executable inline
  `<script>`.
- The CSP allows that style and script by SHA-256 hash, so generated pages are
  byte-identical on every build. `assemblePage` inserts them after any
  template rendering, because `html/template` rewrites inline CSS and
  JavaScript. `TestPagesAllowOnlyTheirInlineCode` fails when they drift.
- CSP rules for markup: no `style=` attributes, no inline event handlers, no
  external `src`/`href`, no CSS `url()`. Build dynamic rows from `<template>`
  elements. Never write `</script>` inside a script; use `<\/script>`.
- `crypto.js` is shared by both pages. The WASM module is embedded as inert
  Base64 data; the CSP permits `wasm-unsafe-eval`, never `unsafe-eval`.
  Imported HTML deliveries are bounded strings: extract only their package
  JSON, without parsing or inserting sender markup as DOM.
- An HTML delivery made in the Studio is the Studio's own markup, captured as
  `pristine` before the script changes anything, with the package embedded
  and `body[data-mode=envelope]` set. Anything the script changes later
  (theme, selection) must not leak into deliveries.
- Icons are [Hugeicons](https://hugeicons.com) (MIT) in an inline `<symbol>`
  sprite, referenced with `<use href="#i-NAME"/>`. Add icons with
  `go run ./scripts/icons`, keep the `<desc>` credit in the sprite, and keep
  `THIRD_PARTY_NOTICES.md` current.

## Copy

- Keep user-facing text free of implementation mechanics. Say what the user
  can do or conclude, not how the cryptography works; that belongs in
  `docs/security.md` and `docs/formats.md`.
- Prefer short, direct labels and guidance over slogans or filler.
- Don't restate the README or docs in the UI. Give guidance only where the
  user needs it, such as the result card after encrypting.
- Error messages say what happened and what to do next.

## Translations

- Every user-facing string in the pages lives in `assets/i18n/*.json`, keyed
  the same in every language; English is the source. Mark up static text with
  `data-i18n`, `data-i18n-aria`, `data-i18n-title`, or `data-i18n-placeholder`,
  and use `t()` or `label()` in scripts. Never hard-code display text.
- Add a language by adding its catalog and listing it in `languages` in
  `i18n.go`. `TestCatalogsMatch` checks keys and `{placeholder}` names.
- Japanese follows the same copy rules: plain and direct, polite form
  (です／ます), Microsoft-style katakana (ブラウザー, フォルダー). Translations
  for user-facing security text need review by a native speaker.
- Codenames, filenames, `.tegami`, `tegami.html`, and units such as MiB stay
  untranslated. CLI messages stay English.
- The CLI delivery page embeds only the keys it uses, keeping it unbranded.

## CLI conventions

- Keep `cmd/tegami` a small entrypoint over `internal/envelope`.
- Write results to stdout and diagnostics to stderr. When stdout carries data
  (`--output -`), anything else goes to stderr.
- Exit 0 on success, 1 on failure, 2 on usage errors, 3 when the password
  is wrong or a delivery is damaged, and 4 for files that are not
  deliveries. Keep these stable; scripts depend on them.
- Write outputs through a temporary file and rename, with mode `0600`.

## Changes that touch the pages

A change to how a page looks or behaves lands with:

1. Both light and dark themes checked, and the Studio, a Studio-made HTML
   delivery, and a CLI delivery page opened in a browser.
2. `make browser-test` passing for any change to encryption, folder packing,
   or streaming, with a check added for new behavior.
3. Updated `docs/studio.md`, screenshots in `docs/assets/` when the look
   changes, and a `CHANGELOG.md` entry.

## Project layout

- `cmd/tegami/`: CLI entrypoint
- `internal/envelope/`: formats, encryption, page assembly, and tests
- `internal/envelope/assets/`: Studio and delivery page HTML, CSS, and JS
- `wasm/`: vendored Monocypher and the Argon2id WebAssembly build
- `scripts/icons/`: converts Hugeicons into the sprite
- `scripts/browsercheck/`: browser checks run by `make browser-test`
- `docs/`: user guides from `docs/index.md`; contributor guides in
  `docs/development/`; screenshots and the terminal demo in `docs/assets/`,
  the demo recorded from `docs/demo.tape` with `make demo`
- `SECURITY.md`: how to report a vulnerability
- `.github/`: CI, CodeQL, release workflows, and Dependabot

## Validation

Run the complete local gate before committing. The `browser` shell adds a
pinned Chromium on Linux; on macOS, point `CHROMIUM` at your Chrome.

```bash
nix develop --command make check
nix develop .#browser --command make browser-test
nix flake check
```
