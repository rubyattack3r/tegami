// Shared by the Studio and the CLI delivery page. All input checks run before
// allocating Argon2's work area. No delivery controls the KDF's resource use.
const envelopeCrypto = (() => {
  "use strict";
  const text = new TextEncoder();
  const memory = 65536; // KiB
  const passes = 3;
  const lanes = 1;
  const maxNameBytes = 180;
  const chunkSize = 4194304;
  // Plaintext limits of HTML deliveries and .tegami files.
  const limits = {html: 524288000, blob: 3221225472};
  // SHA-256 of argon2.wasm, inserted when the page is assembled. This script is
  // pinned by the page's CSP hash, so the module it runs is pinned too.
  const wasmSHA256 = "__TEGAMI_ARGON2_SHA256__";
  const decode = (value) => {
    if (typeof value !== "string" || !/^[A-Za-z0-9+/]*$/.test(value) || value.length % 4 === 1) throw new Error("encoding");
    const binary = atob(value);
    const bytes = new Uint8Array(binary.length);
    for (let index = 0; index < binary.length; index++) bytes[index] = binary.charCodeAt(index);
    return bytes;
  };
  const bytesOf = (value, length) => {
    const bytes = decode(value);
    if (bytes.length !== length) throw new Error("length");
    return bytes;
  };
  const validSlot = (slot) => !!slot && slot.type === "password" && slot.memory === memory && slot.passes === passes && slot.lanes === lanes;
  const validName = (name) => typeof name === "string" && name !== "" && name !== "." && name !== ".." && !/[\/\\\0\r\n]/.test(name) && text.encode(name).length <= maxNameBytes && (!name.isWellFormed || name.isWellFormed());
  const sealed = (value, length) => {
    if (!value) throw new Error("sealed");
    bytesOf(value.nonce, 12);
    return bytesOf(value.ciphertext, length);
  };
  // Validates the header of an HTML delivery ("html") or .tegami file ("blob").
  const validate = (data, kind) => {
    const slot = data && data.unlock;
    if (!data || !Object.hasOwn(limits, kind) || data.schema !== "tegami/v1" || typeof data.id !== "string" || !/^[A-Za-z0-9_-]{22}$/.test(data.id) || !Number.isSafeInteger(data.size) || data.size < 1 || data.size > limits[kind] || !validSlot(slot)) throw new Error("metadata");
    bytesOf(slot.salt, 16);
    bytesOf(slot.nonce, 12);
    bytesOf(slot.wrapped_key, 48);
    bytesOf(data.commit, 32);
    sealed(data.name, maxNameBytes + 16);
    if (data.chunk_size !== chunkSize) throw new Error("chunk size");
    return slot;
  };
  let module;
  const compile = async () => {
    const bytes = Uint8Array.from(atob(document.getElementById("tegami-argon2").textContent), (character) => character.charCodeAt(0));
    const digest = Array.from(new Uint8Array(await crypto.subtle.digest("SHA-256", bytes)), (byte) => byte.toString(16).padStart(2, "0")).join("");
    if (digest !== wasmSHA256) throw new Error("argon2 module");
    return WebAssembly.compile(bytes);
  };
  const derive = async (material, salt) => {
    if (material.length < 12 || material.length > 4096 || salt.length !== 16) throw new Error("key input");
    if (!module) module = compile();
    const instance = new WebAssembly.Instance(await module);
    const heap = Number(instance.exports.__heap_base.value);
    const work = Math.ceil(heap / 8) * 8;
    const output = work + memory * 1024;
    const pass = output + 32;
    const saltAt = pass + material.length;
    const end = saltAt + salt.length;
    const area = instance.exports.memory;
    if (end > area.buffer.byteLength) area.grow(Math.ceil((end - area.buffer.byteLength) / 65536));
    const bytes = new Uint8Array(area.buffer);
    try {
      bytes.set(material, pass);
      bytes.set(salt, saltAt);
      // Let the page display its progress message before synchronous WASM work.
      await new Promise((resolve) => setTimeout(resolve, 0));
      instance.exports.argon2id(output, 32, work, memory, passes, lanes, pass, material.length, saltAt, salt.length, 0, 0, 0, 0);
      return await crypto.subtle.importKey("raw", bytes.subarray(output, output + 32), "AES-GCM", false, ["encrypt", "decrypt"]);
    } finally {
      bytes.fill(0);
    }
  };
  const commitMeta = (id) => text.encode("tegami/v1\0" + id + "\0commit");
  const commit = async (raw, id) => {
    const key = await crypto.subtle.importKey("raw", raw, {name: "HMAC", hash: "SHA-256"}, false, ["sign"]);
    return new Uint8Array(await crypto.subtle.sign("HMAC", key, commitMeta(id)));
  };
  // Unwraps the content key and accepts it only if the delivery commits to it.
  const unwrap = async (data, kind, material) => {
    const slot = validate(data, kind);
    const wrapping = await derive(material, bytesOf(slot.salt, 16));
    const raw = new Uint8Array(await crypto.subtle.decrypt({name: "AES-GCM", iv: bytesOf(slot.nonce, 12), additionalData: text.encode("tegami/v1\0" + data.id + "\0password")}, wrapping, bytesOf(slot.wrapped_key, 48)));
    try {
      if (raw.length !== 32) throw new Error("content key");
      const key = await crypto.subtle.importKey("raw", raw, {name: "HMAC", hash: "SHA-256"}, false, ["verify"]);
      if (!await crypto.subtle.verify("HMAC", key, bytesOf(data.commit, 32), commitMeta(data.id))) throw new Error("commitment");
      return await crypto.subtle.importKey("raw", raw, "AES-GCM", false, ["decrypt"]);
    } finally {
      raw.fill(0);
    }
  };
  const padName = (name) => {
    if (!validName(name)) throw new Error("name");
    const padded = new Uint8Array(maxNameBytes);
    padded.set(text.encode(name));
    return padded;
  };
  const openName = async (key, data) => {
    const raw = new Uint8Array(await crypto.subtle.decrypt({name: "AES-GCM", iv: bytesOf(data.name.nonce, 12), additionalData: text.encode("tegami/v1\0" + data.id + "\0name")}, key, decode(data.name.ciphertext)));
    try {
      if (raw.length !== maxNameBytes) throw new Error("padding");
      let end = raw.length;
      while (end && raw[end - 1] === 0) end--;
      const name = new TextDecoder("utf-8", {fatal: true, ignoreBOM: true}).decode(raw.subarray(0, end));
      if (!validName(name)) throw new Error("name");
      return name;
    } finally {
      raw.fill(0);
    }
  };
  // An HTML delivery carries the records of a .tegami file, each in an inert
  // element right after the package element: the nonce and ciphertext in
  // Base64 without padding.
  const recordStart = (index) => '\n<script id="tegami-r' + index + '" type="application/octet-stream">';
  const recordCount = (data) => Math.ceil(data.size / chunkSize);
  const plainLength = (data, index) => Math.min(chunkSize, data.size - index * chunkSize);
  const encodedLength = (data, index) => Math.ceil((12 + plainLength(data, index) + 16) * 4 / 3);
  const openRecord = async (key, data, index, encoded) => {
    const expected = plainLength(data, index);
    if (typeof encoded !== "string" || encoded.length !== encodedLength(data, index)) throw new Error("record length");
    const record = decode(encoded);
    if (record.length !== 12 + expected + 16) throw new Error("record length");
    const plain = new Uint8Array(await crypto.subtle.decrypt({name: "AES-GCM", iv: record.subarray(0, 12), additionalData: text.encode("tegami/v1\0" + data.id + "\0" + data.size + "\0" + index + "\0" + expected)}, key, record.subarray(12)));
    if (plain.length !== expected) throw new Error("record length");
    return plain;
  };
  // Decrypts every record into a Blob, so no output exists unless all of them
  // open. source.read(index) returns a record's Base64 text, and
  // source.more(count) reports whether another record follows the last.
  const openRecords = async (key, data, source, progress) => {
    const count = recordCount(data);
    let output = new Blob([]);
    for (let index = 0; index < count; index++) {
      const plain = await openRecord(key, data, index, await source.read(index));
      output = new Blob([output, plain]);
      plain.fill(0);
      progress((index + 1) / count);
    }
    if (await source.more(count)) throw new Error("trailing record");
    return output;
  };
  // This page's own records, read in the same positions as the CLI reads
  // them: each directly after the package element or the previous record,
  // separated by one line break.
  const pageRecords = () => {
    let previous = document.getElementById("tegami-package");
    const next = (index) => {
      const separator = previous && previous.nextSibling;
      const element = separator && separator.nodeType === Node.TEXT_NODE && separator.data === "\n" ? separator.nextSibling : null;
      return element && element.id === "tegami-r" + index && element.localName === "script" && element.getAttribute("type") === "application/octet-stream" ? element : null;
    };
    return {
      read: (index) => {
        const element = next(index);
        if (!element) throw new Error("record");
        previous = element;
        return element.textContent;
      },
      more: (count) => next(count) !== null,
    };
  };
  return {memory, passes, lanes, chunkSize, limits, validate, derive, commit, unwrap, padName, openName, recordStart, encodedLength, openRecords, pageRecords};
})();
