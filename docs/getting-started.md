# Getting started

To send or open files in a browser, all you need is `tegami.html`, the
Studio. The `tegami` command is for terminals and scripts.

## Download a release

Each [release](https://github.com/rubyattack3r/tegami/releases/latest)
includes:

| Asset | What it is |
| --- | --- |
| `tegami.html` | The Studio. Open it in a browser; nothing to install. |
| `tegami_<version>_<os>_<arch>.tar.gz` or `.zip` | The `tegami` command for Linux, macOS (`darwin`), and Windows, on `amd64` or `arm64`. |
| `checksums.txt` | SHA-256 checksums for the assets. |

To install the command, extract the archive, put `tegami` (or `tegami.exe`)
on your `PATH`, and check that it runs:

```sh
tegami version
```

On macOS, if Gatekeeper blocks the downloaded binary, run
`xattr -d com.apple.quarantine tegami`.

## Verifying downloads

`tegami.html` sees your files and passwords, so check that your copy is
genuine before you use it or pass it on. Any one of these checks is enough.

**Checksum.** Download `checksums.txt` into the same folder and run:

```sh
sha256sum --check --ignore-missing checksums.txt      # Linux
shasum -a 256 --check --ignore-missing checksums.txt  # macOS
```

**Build provenance.** With the [GitHub CLI](https://cli.github.com/):

```sh
gh attestation verify tegami.html --repo rubyattack3r/tegami
```

This proves the file was built from this repository by its release workflow.

**Rebuild it yourself.** Every build of `tegami.html` is identical, so you can
compare a copy with the source without trusting GitHub:

```sh
git clone https://github.com/rubyattack3r/tegami
cd tegami
git checkout vX.Y.Z
go run ./cmd/tegami studio --output rebuilt.html
sha256sum rebuilt.html /path/to/downloaded/tegami.html
```

The two hashes match when the download is genuine.

## Other ways to install

```sh
# Go, at the version in go.mod
go install github.com/rubyattack3r/tegami/cmd/tegami@latest

# Nix: write the Studio without installing, or install the command
nix run github:rubyattack3r/tegami -- studio --output tegami.html
nix profile install github:rubyattack3r/tegami
```

To build from a checkout, see the [development setup](development/setup.md).

## Send your first file

### In the browser

1. Open `tegami.html` and drop a file onto **Send**.
2. Keep the generated password, or type your own of at least 12 characters.
3. Select **Encrypt**. Send the saved `<codename>.html` to the recipient, and
   give them the password another way, mentioning the codename.

The [Studio guide](studio.md) covers formats, folders, and opening files.

### From the command line

```sh
# Generate a password, or write your own of at least 12 characters.
tegami password > password.txt

# Encrypt. tegami prints the codename, the delivery's only visible name.
tegami pack --input report.pdf --output delivery.html < password.txt

# Open it, as the recipient would.
tegami open --input delivery.html < password.txt
```

The [CLI reference](cli-reference.md) covers every command and option.
