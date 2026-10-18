# Tegami

**English** | [日本語](README.ja.md)

Send files encrypted with a password. The recipient opens them in a browser,
offline, with no account and no install.

Everything happens on your own device. Tegami's pages make no network
requests, and a decrypted file is only downloaded, never opened or run.

![Tegami Studio with two files ready to encrypt](docs/assets/tegami-studio.png)

## Quick start

1. Download `tegami.html` from the [latest release][releases], and
   [verify it](docs/getting-started.md#verifying-downloads).
2. Open it in your browser. Under **Send**, drop in files or a folder and
   choose a format. A strong password is filled in for you; copy it, or type
   your own. Select **Encrypt**.
3. Send the result to your recipient. It is named with a codename such as
   `quiet-harbor`, so the real filename stays hidden. Send the password
   **another way**, such as a phone call, and mention the codename.

The recipient opens the file and enters the password. That's it.

<img src="docs/assets/tegami-recipient.png" alt="What the recipient sees: the codename, a password field, and Decrypt and download" width="480">

Pages appear in English or Japanese to match the browser, and a language menu
changes it.

## Choose a format

| Format | Best for | Size limit | Recipient needs |
| --- | --- | --- | --- |
| **HTML file** | Most deliveries; one file to attach | 50 MiB | Any modern browser |
| **Tegami file** (`.tegami`) | Large files | 3 GiB | A trusted copy of `tegami.html` and a Chromium-based browser (Chrome, Edge, Brave) |

## Command line

The `tegami` command creates and opens the same deliveries from a terminal or
script.

<picture>
  <source media="(prefers-reduced-motion: reduce)" srcset="docs/assets/terminal.png">
  <img src="docs/assets/terminal.gif" alt="Tegami terminal demo: generate a password, encrypt a CSV into an HTML delivery named by its codename, inspect it without revealing the filename, open it as JSON, fail with a wrong password, and pipe a folder into a streaming delivery." width="1200">
</picture>

```sh
# Generate a password to give the recipient another way.
tegami password > password.txt

# Encrypt a file. It prints the codename.
tegami pack --input report.pdf --output delivery.html < password.txt

# Open it again.
tegami open --input delivery.html < password.txt

# Pipes work too.
pg_dump shop | tegami pack --input - --name shop.sql --output - \
  --format blob --password-file password.txt > shop.tegami
```

## Install

- **Download**: binaries for Linux, macOS, and Windows, and `tegami.html`, are
  on the [releases page][releases].
- **Go**: `go install github.com/rubyattack3r/tegami/cmd/tegami@latest`
- **Nix**: `nix run github:rubyattack3r/tegami -- studio`

## Documentation

- [Getting started](docs/getting-started.md): download, verify, and send a
  first file
- [Studio guide](docs/studio.md) and [CLI reference](docs/cli-reference.md)
- [Security model](docs/security.md): what is protected and what isn't
- [File formats](docs/formats.md): the `tegami/v1` specification
- [Developer documentation](docs/development/index.md) for contributors

Report vulnerabilities as described in the [security policy](SECURITY.md).
Changes are listed in the [changelog](CHANGELOG.md).

## License

[MIT](LICENSE). Third-party components are listed in
[THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).

[releases]: https://github.com/rubyattack3r/tegami/releases/latest
