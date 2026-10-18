# Changelog

All notable changes to this project are documented in this file. The format
is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [Unreleased]

## [0.1.0] - 2026-10-18

### Added

- **Tegami Studio** (`tegami.html`), a single offline page that encrypts and
  opens files in the browser. Drop in files or a folder (sent as one ZIP),
  get a strong generated password, and follow the next steps on the result
  card. Light, dark, and system themes.
- **HTML deliveries** up to 50 MiB that open in any modern browser, and
  **Tegami files** (`.tegami`) up to 3 GiB, opened with the Studio in
  Chromium-based browsers.
- **Codenames** such as `quiet-harbor` name each delivery, so the real
  filename stays hidden until it is unlocked, and stays out of tab titles and
  browser history afterwards.
- The Studio's **Open** tab also opens HTML deliveries without running the
  sender's page, for deliveries from people you don't trust.
- **English and Japanese** pages, matched to the browser's language and
  changed from a language menu.
- **Accessible pages**: every password field has a visible label that stays
  in place while you type, and every delivery has a main landmark with its
  codename as the page heading.
- **Linux fonts**: pages use the desktop's own font on GNOME, Ubuntu, and
  other Linux desktops, including GNOME Web, and the Japanese form of Noto
  Sans CJK for Japanese text.
- **The `tegami` command**: `pack` (HTML by default, or `--format blob`),
  `open`, `inspect` (codename, format, and size without the password),
  `password`, `studio`, and `version`. It supports pipes (`--input -`,
  `--output -`, `--name`), `--password-file`, `--json` output, and stable exit
  statuses for scripts.
- **Security**: Argon2id password hashing (64 MiB, three passes, one lane), a
  commitment to each delivery's key, and filenames padded so their length
  stays hidden. See the [security model](docs/security.md).
- **Verifiable releases**: binaries for Linux, macOS, and Windows on `amd64`
  and `arm64`, with `tegami.html`, checksums, and build provenance. Every
  build of `tegami.html` is identical, so it can be checked by rebuilding it.
