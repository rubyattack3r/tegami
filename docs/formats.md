# File formats

Tegami writes one schema, `tegami/v1`, in two containers. Both encrypt the
file with a random 256-bit content key, seal the filename with the same key,
and wrap the key with a key derived from the password.

| Container | Used by | Max plaintext | Structure |
| --- | --- | --- | --- |
| Envelope | `html` deliveries | 50 MiB | JSON embedded in an HTML page |
| Blob | `blob` deliveries (`.tegami`) | 3 GiB | Binary header followed by 4 MiB encrypted records |

Tegami reads and writes `tegami/v1` only.

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

Every envelope and blob header includes a `commit` field:

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
| Encrypt envelope payload | content key | `tegami/v1 ‖ id ‖ size` |
| Encrypt blob chunk `i` | content key | `tegami/v1 ‖ id ‖ size ‖ i ‖ plaintext_len_i` |

The filename is UTF-8, up to 180 bytes, without `/`, `\`, CR, LF, or NUL.
It must be nonempty and cannot be `.` or `..`. Append NUL bytes on the right
to make exactly 180 bytes before sealing. The resulting ciphertext is always
196 bytes, including the GCM tag. Readers require exactly 180 decrypted bytes,
remove only trailing NUL bytes, and reject embedded NULs, invalid UTF-8, or
anything that violates the filename rule. A name of exactly 180 bytes has no
padding.

## Envelope

The envelope is a JSON object stored in a
`<script id="tegami-package" type="application/json">` element.

```json
{
  "schema": "tegami/v1",
  "id": "<b64url, 16 random bytes>",
  "size": 182044,
  "commit": "<b64, 32-byte HMAC>",
  "name": {"nonce": "<b64, 12 bytes>", "ciphertext": "<b64>"},
  "payload": {"nonce": "<b64, 12 bytes>", "ciphertext": "<b64>"},
  "unlocks": [
    {
      "type": "password",
      "salt": "<b64, 16 bytes>",
      "nonce": "<b64, 12 bytes>",
      "wrapped_key": "<b64, 32-byte key + 16-byte tag>",
      "memory": 65536,
      "passes": 3,
      "lanes": 1
    }
  ]
}
```

`unlocks` currently contains exactly one slot.

Readers outside the page, the Studio's Open tab and `tegami open`, read at
most 128 MiB of HTML as UTF-8 text, find the exact package marker shown above,
and parse only the JSON up to the first closing script tag. They never run,
render, or insert the sender's markup. The plaintext limit is still 50 MiB.

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
