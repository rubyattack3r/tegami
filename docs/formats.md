# File formats

Tegami writes one schema, `tegami/v1`, in two containers. Both encrypt the
file with a random 256-bit content key, seal the filename with the same key,
and wrap the key with a key derived from the password.

| Container | Used by | Max plaintext | Structure |
| --- | --- | --- | --- |
| Page | `html` deliveries | 500 MiB | Header JSON and 4 MiB encrypted records in an HTML page |
| Blob | `blob` deliveries (`.tegami`) | 3 GiB | Binary header followed by 4 MiB encrypted records |

Tegami reads `tegami/v1` only.

Conventions used below:

- `‖` joins strings with a single NUL byte (`0x00`).
- Integers in AAD strings are written in decimal ASCII.
- `b64` is standard Base64 without padding; `b64url` is URL-safe Base64
  without padding.
- AES-GCM uses a 96-bit nonce and 128-bit tag; ciphertexts include the tag.
  Every nonce is random.

## What is visible

Without the password, a delivery reveals its codename and its plaintext size.
The filename and contents are encrypted. Every filename is padded to 180 bytes
before encryption, hiding its length as well as its text.

## Codename

Each delivery has a random 16-byte ID. Its codename is
`adjectives[id[0]] + "-" + nouns[id[1]]`, using the two 256-word lists in
[`internal/envelope/assets/codenames.json`](../internal/envelope/assets/codenames.json),
for example `quiet-harbor`. The codename is derived, not stored, so it is
covered by the authentication of the ID. It is neither unique nor secret.

## Password

The only unlock slot type is `password`, which is also the `type` in the AAD
below. The password is 12 to 4096 bytes of UTF-8 without CR, LF, or NUL. The
wrapping key is
`Argon2id(password, salt, memory = 65536 KiB, passes = 3, lanes = 1,
version = 0x13, length = 32)`, with no secret or associated-data input.
The salt is 16 random bytes.

The slot stores `memory`, `passes`, and `lanes`. Readers must reject any
values other than those above before allocating memory or deriving a key.

## Content-key commitment

Every package and blob header includes a `commit` field:

`b64(HMAC-SHA256(content_key, tegami/v1 ‖ id ‖ commit))`.

The strings here use the same NUL-separated encoding as the AAD below;
`id` is its encoded `b64url` string. After unwrapping the 32-byte content key,
readers verify this 32-byte commitment **before** decrypting the name or
payload, with a constant-time comparison. A missing, malformed, or mismatched
commitment fails to open. This binds each delivery to one content key, which
GCM's authentication alone does not.

## Operations

| Operation | Key | AAD |
| --- | --- | --- |
| Wrap content key | wrapping key | `tegami/v1 ‖ id ‖ password` |
| Seal filename | content key | `tegami/v1 ‖ id ‖ name` |
| Encrypt chunk `i` of a page or blob | content key | `tegami/v1 ‖ id ‖ size ‖ i ‖ plaintext_len_i` |

The filename is UTF-8, up to 180 bytes, without `/`, `\`, CR, LF, or NUL.
It must be nonempty and cannot be `.` or `..`. Append NUL bytes on the right
to make exactly 180 bytes before sealing. The resulting ciphertext is always
196 bytes, including the GCM tag. Readers require exactly 180 decrypted bytes,
remove only trailing NUL bytes, and reject embedded NULs, invalid UTF-8, or
anything that violates the filename rule. A name of exactly 180 bytes has no
padding.

## Blob

```text
+----------------------+---------------------------+------------------+
| magic (8 bytes)      | header_len (u32, BE)      | header (JSON)    |
| "TEGAMI1\0"          | 1 .. 65536                | header_len bytes |
+----------------------+---------------------------+------------------+
| record 0 | record 1 | ... | record n-1                              |
+-----------------------------------------------------------------------+

record:
+------------------------+----------------+---------------------------+
| ciphertext_len (u32,BE)| nonce (12 B)   | ciphertext (len bytes)    |
+------------------------+----------------+---------------------------+
```

The header is UTF-8 JSON:

```json
{
  "schema": "tegami/v1",
  "id": "<b64url, 16 random bytes>",
  "size": 1073741824,
  "commit": "<b64, 32-byte HMAC>",
  "name": {"nonce": "<b64, 12 bytes>", "ciphertext": "<b64>"},
  "chunk_size": 4194304,
  "unlock": {
    "type": "password",
    "salt": "<b64, 16 bytes>",
    "nonce": "<b64, 12 bytes>",
    "wrapped_key": "<b64>",
    "memory": 65536,
    "passes": 3,
    "lanes": 1
  }
}
```

The plaintext is split into `ceil(size / chunk_size)` chunks of `chunk_size`
bytes, except the last, which holds the remainder.

Readers must reject a blob when:

- the magic, schema, `chunk_size` (4194304), Argon2id parameters, or
  unlock `type` (`password`) differ from the values above;
- `size` is outside 1 to 3 GiB;
- a record's `ciphertext_len` is below 17 or above `chunk_size + 16`;
- any record fails to authenticate or decrypts to an unexpected length;
- bytes remain after the record that completes `size`.

Binding the index and length of each chunk into its AAD prevents records
from being reordered, duplicated, or dropped, and the trailing-data check
prevents extension.

No output is published until every record and the trailing-data check pass.
The CLI decrypts to a private temporary file before renaming it or copying it
to stdout. The Studio closes its save transaction only on success and aborts
it on failure, including damage to the last record.

## Page

A page is an HTML delivery that carries a blob's header and records. The
header JSON, exactly as in a blob, is the content of the package element, and
each record follows it in its own inert element:

```text
<script id="tegami-package" type="application/json">HEADER</script>
<script id="tegami-r0" type="application/octet-stream">RECORD 0</script>
<script id="tegami-r1" type="application/octet-stream">RECORD 1</script>
...
<script id="tegami-rN-1" type="application/octet-stream">RECORD N-1</script>
```

Each record element starts with a single LF (`0x0A`) and directly follows the
previous element's closing `</script>`, the package element's for record 0.
Its content is `b64(nonce ‖ ciphertext)` for chunk `i`, encrypted exactly as a
blob chunk, so it is `ceil((12 + plaintext_len_i + 16) × 4 / 3)` characters.
The rest of the page follows the last record.

Readers must reject a page when:

- the header fails any of the blob's header checks, or `size` is outside
  1 byte to 500 MiB;
- a record element is missing, out of order, separated from the previous one
  by anything but one LF, or has the wrong length;
- any record fails to authenticate or decrypts to an unexpected length;
- an element for record `N` follows the last record.

`tegami open` and the Studio's Open tab check these at the exact byte
offsets. A delivery page opening itself checks the same positions in its
document: each record element is the next sibling after a text node holding
one LF. No output is published until every record passes; the pages assemble
the file as a `Blob` and offer it as a download only then.

## Finding the package

Readers outside the page, the Studio's Open tab and `tegami open`, treat HTML
as bytes. They look for the exact package marker shown above in the first
MiB of the file and parse only the JSON up to the first closing `</script>`
after it. They never run, render, or insert the sender's markup.

Version 0.1.0 wrote HTML deliveries with a different package: one JSON object
with a single `payload` and an `unlocks` array, specified in [the 0.1.0
formats](https://github.com/rubyattack3r/tegami/blob/v0.1.0/docs/formats.md).
Those files open only in their own page. Readers outside it reject them and
say so.
