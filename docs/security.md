# Security model

Tegami keeps a file's contents and name private with a password you share.
It is meant for sending files over channels you don't fully trust, such as
email, chat, or cloud storage, to someone you can give the password to another
way.

## What Tegami protects

- **The contents.** Each delivery is encrypted with its own random key, and
  that key can only be recovered with the password. Password guessing is slowed
  with Argon2id, which costs 64 MiB of memory per guess.
- **The filename.** The real name is encrypted with the contents and padded to
  a fixed length, so neither the name nor its length shows. Until it is
  unlocked, a delivery is known only by its [codename](#codenames).
- **Integrity.** A delivery that was changed, truncated, reordered, or added to
  fails to open. You never get a silently corrupted file.
- **Your device stays offline.** Tegami's pages make no network requests and
  load nothing from elsewhere. Decrypted files are only downloaded, never
  opened or run by the page. This holds for genuine copies; see
  [untrusted HTML deliveries](#untrusted-html-deliveries).
- **The password.** It is never stored in a delivery. The CLI reads it from
  standard input or a file, never from command-line arguments, so it stays
  out of shell history and the process list.

## What Tegami does not protect

- **The codename and size.** Anyone holding a delivery can see its codename
  and the exact size of the original file. They can also see the delivery's
  own filename. The Studio names it after the codename; with the CLI, you
  choose the name, so don't describe the contents in it.
- **The filename after unlocking.** Once opened, the file is saved under its
  real name, so the name appears in the browser's downloads list and wherever
  the file is saved. The page itself keeps showing the codename, so the name
  stays out of tab titles and browser history.
- **Weak passwords.** Slowing guesses can't save a short or reused password.
  Use the generated password or another long random one, and never send it
  with the delivery.
- **A tampered Studio or browser.** `tegami.html` sees both the file and the
  password. Use a [verified copy](getting-started.md#verifying-downloads), and
  have recipients get theirs from a trusted source, not from the message that
  carries the `.tegami` file. Browser extensions can read pages, so use a
  profile without extensions you don't trust.
- **Passwords in browser memory.** Browsers give pages no way to wipe text
  from memory. The pages clear the password field and their own copies after
  use, but a typed password may stay in memory until the tab closes.
- **The recipient.** Once someone opens a file, they have it. Tegami can't
  revoke, expire, or stop forwarding of a delivery.
- **Who sent it.** Anyone who knows the password can make a delivery with it.
  Tegami shows that a delivery wasn't changed after it was made, not who made
  it.

### Untrusted HTML deliveries

Opening an HTML delivery directly runs a page the sender made. A dishonest
sender can fake the unlock screen and capture the password you type, so the
protections above apply only to pages that came from Tegami unchanged.

For a delivery from someone you don't trust, open your own verified
`tegami.html`, select **Open**, and choose the HTML file, or use
`tegami open`. Both read the encrypted data from the file without running the
sender's page. Get the Studio or the CLI from a trusted source, never from the
same sender.

## Codenames

Each delivery has a codename such as `quiet-harbor`, derived from its random
ID. It lets sender and recipient talk about a delivery without saying what is
in it, and helps the recipient notice when a file isn't the one they were told
to expect.

A codename is neither secret nor unique: there are 65,536 of them. Never use
it as a password or as a reason to trust a file.

## Cryptographic details

| Parameter | Value |
| --- | --- |
| Content encryption | AES-256-GCM with a random key per delivery |
| Key wrapping | AES-256-GCM |
| Key derivation | Argon2id v1.3, 64 MiB, 3 passes, 1 lane, 16-byte random salt |
| Key commitment | HMAC-SHA256, checked before anything is decrypted |
| Filename | Padded to 180 bytes, then encrypted with the content key |
| Streaming chunks | 4 MiB, each bound to its position in the file |

Every encrypted value is bound to the delivery's schema and ID. Readers accept
only the Argon2id settings above and reject any other before doing any work,
so a delivery can't force excessive memory or time. The exact layouts are in
[File formats](formats.md).

## Reporting a vulnerability

See the [security policy](../SECURITY.md).
