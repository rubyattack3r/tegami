# Hosting a delivery on a website

You can put an HTML delivery on a website and send a link instead of the
file. If you do, **set it up to download, and ask the recipient to open the
downloaded file**, not the page on the website.

Anyone with the link can download the delivery. Without the password they see
only its codename and size, so share the password another way, as you would
for an attachment.

## Why the recipient should download it first

Some networks, especially at workplaces, check web pages before showing them.
They may change the page, or open it on a remote computer and show the
recipient only a copy. That affects a delivery opened on the website:

| | Opened on the website | Downloaded, then opened |
| --- | --- | --- |
| Opens reliably | Not always: it may not respond or unlock | Yes |
| Who handles the password and file | The network's remote computer may see both | Only the recipient's computer |
| The file the recipient gets | May be scanned, changed, or blocked | Exactly the file you made |

The file on your server is fine either way. A network can scan a downloaded
delivery, but it can't read what's inside, and decryption happens only on the
recipient's computer.

## For the sender: make the delivery download

Ask the web server to send deliveries as attachments, so browsers save them
instead of opening them. Keep deliveries in their own folder, such as
`/deliveries/`, so the rest of the site is unaffected.

**nginx**

```nginx
location /deliveries/ {
    add_header Content-Disposition "attachment";
}
```

**Apache**, in a `.htaccess` file in the folder (needs `mod_headers`):

```apache
Header set Content-Disposition "attachment"
```

**Caddy**

```caddy
header /deliveries/* Content-Disposition attachment
```

If you can't change the server's settings:

- **Use a download link** on another page of the same site:

  ```html
  <a href="/deliveries/quiet-harbor.html" download>Download quiet-harbor.html</a>
  ```

- **Put the delivery in a ZIP file** and host that instead. Networks rarely
  change files inside an archive, and some block HTML downloads but allow ZIP
  files.

## For the recipient: what to send with the link

Send the password separately, then something like this with the link:

> 1. Download the file from the link. Don't open it on the website.
> 2. Open the downloaded file, for example from your Downloads folder. If it
>    came as a ZIP file, extract it first.
> 3. Enter the password I sent you separately. The file is called
>    `quiet-harbor`.

## If it still doesn't open

If the right password doesn't work, the downloaded copy was changed on the
way. Download it again from another network, such as a home connection, or
ask the sender to send the file directly.

Don't use your browser's **Save Page As** on the website. It saves the page
as your browser received it, including any changes made on the way.
