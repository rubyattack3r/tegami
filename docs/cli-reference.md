# CLI reference

```text
tegami pack     --input FILE|- --output FILE|- [--format html|blob]
                [--password-file FILE] [--name NAME] [--json]
tegami open     --input FILE|- [--output FILE|DIR|-] [--password-file FILE] [--force] [--json]
tegami inspect  --input FILE|- [--json]
tegami password
tegami studio   [--output tegami.html]
tegami version
tegami help
```

Files are written to a temporary file first, then renamed into place with
mode `0600`, so a failed run never leaves a partial file behind.

## `tegami pack`

Encrypts one file into a delivery and prints its codename.

| Flag | Default | Description |
| --- | --- | --- |
| `--input` | *(required)* | File to encrypt, or `-` for standard input. Its name is sealed inside the delivery and restored when it is opened. |
| `--output` | *(required)* | Delivery to create, or `-` for standard output. Overwritten if it exists. This name is visible to anyone who has the file, so don't reveal the contents in it. |
| `--format` | `html` | `html` or `blob`. See [Formats](#formats). |
| `--password-file` | standard input | Read the password from this file instead. Required with `--input -`. |
| `--name` | | The filename to seal when reading from standard input. Required with `--input -`. |
| `--json` | | Print a JSON result instead of the codename. Not with `--output -`. |

The codename, such as `quiet-harbor`, is the delivery's only visible name;
tell the recipient to expect it. The real filename appears only once the
delivery is unlocked. With `--output -`, the codename goes to standard error,
because standard output carries the delivery.

### Formats

| Format | Output | Input size | Opened with |
| --- | --- | --- | --- |
| `html` | One self-contained HTML page | 1 byte to 50 MiB | Any browser |
| `blob` | Streaming `.tegami` file | 1 byte to 3 GiB | The Studio in a Chromium-based browser |

Both can also be opened with `tegami open`. If a mail system blocks HTML
attachments, share the delivery through a file-sharing link or chat, or send a
`blob`. Zipping the HTML rarely helps, because such filters look inside ZIPs
too.

### Passwords

The password is read as one line from **standard input**, or from
`--password-file`, so it never appears in your shell history or the process
list. It must be 12 to 4096 bytes of UTF-8. A trailing newline is optional.

`tegami password` prints a random password in the same form the Studio
generates, such as `k7mxq-3vbtn-wr9ha-e2pjd`:

```sh
tegami password > password.txt
tegami pack --input report.pdf --output delivery.html --password-file password.txt
```

Send the password through a different channel than the delivery.

### JSON output

```json
{"codename":"quiet-harbor","format":"html","size":182044,"output":"delivery.html"}
```

`size` is the size of the original file.

## `tegami open`

Unlocks an HTML or `.tegami` delivery made by Tegami and writes the
original file. Supply the password the same way as for `pack`.

| Flag | Default | Description |
| --- | --- | --- |
| `--input` | *(required)* | Delivery to open, or `-` for standard input. |
| `--output` | the original filename, in the current directory | File to write, a directory to write the original filename into, or `-` for standard output. |
| `--password-file` | standard input | Read the password from this file instead. Required with `--input -`. |
| `--force` | | Replace an existing file. Without it, `open` refuses to overwrite anything, because the original filename comes from the delivery. |
| `--json` | | Print a JSON result instead of the output path. Not with `--output -`. |

The whole delivery is decrypted and checked before any output is published, so
a damaged delivery never produces partial output, even with `--output -`.

```json
{"codename":"quiet-harbor","name":"report.pdf","format":"html","size":182044,"output":"report.pdf"}
```

## `tegami inspect`

Shows what anyone holding a delivery can see without the password: its
codename, format, and the size of the original file. Use it to check that a
file is the delivery you were told to expect.

```console
$ tegami inspect --input delivery.html
codename  quiet-harbor
format    html
size      182044 bytes
```

With `--json`:

```json
{"codename":"quiet-harbor","format":"html","size":182044}
```

## `tegami studio`

Writes the offline Studio page.

| Flag | Default | Description |
| --- | --- | --- |
| `--output` | `tegami.html` | Path of the HTML page to create. |

The Studio is identical on every build, so a published copy can be checked by
rebuilding it; see [Verifying downloads](getting-started.md#verifying-downloads).

## Exit status

| Status | Meaning |
| --- | --- |
| 0 | Success. |
| 1 | Failure, such as an unreadable input, a file that already exists, or a password that is too short. |
| 2 | Usage error: an unknown command or flag, or a missing or conflicting option. |
| 3 | The password is incorrect, or the delivery is damaged. These can't be told apart, by design. |
| 4 | The input is not a Tegami delivery. |

## Examples

```sh
# Name the delivery after its codename.
name=$(tegami pack --input report.pdf --output delivery.html < password.txt)
mv delivery.html "$name.html"

# A large streaming blob. The recipient opens it with tegami.html in Chromium.
tegami pack --input footage.mov --output delivery.tegami --format blob < password.txt

# Encrypt a stream, such as a database dump.
pg_dump shop | tegami pack --input - --name shop.sql --output - --format blob \
  --password-file password.txt > shop.tegami

# Open a delivery into a directory and report the result as JSON.
tegami open --input delivery.html --output ~/Downloads --json < password.txt

# Decrypt to standard output.
tegami open --input shop.tegami --output - --password-file password.txt | psql shop

# Check the codename before opening.
tegami inspect --input delivery.html --json | jq -r .codename

# Branch on the result in a script.
tegami open --input delivery.html --output - < password.txt > report.pdf
case $? in
  0) echo "opened" ;;
  3) echo "wrong password, or the file is damaged" ;;
  4) echo "not a Tegami delivery" ;;
esac
```

With `--input -`, `pack` and `open` keep a private temporary copy of the input
while they work, so allow disk space for it.

## Filenames

Filenames may be up to 180 bytes of UTF-8 without `/`, `\`, line breaks, or
NUL. The Studio uses the same rule, so names such as `報告書.pdf` work in both.
