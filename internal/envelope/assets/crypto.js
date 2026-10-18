// Shared by the Studio and the CLI delivery page. All input checks run before
// allocating Argon2's work area. No delivery controls the KDF's resource use.
const envelopeCrypto = (() => {
  "use strict";
  const text = new TextEncoder();
  const memory = 65536; // KiB
  const passes = 3;
  const lanes = 1;
  const maxNameBytes = 180;
  // SHA-256 of argon2.wasm, inserted when the page is assembled. This script is
  // pinned by the page's CSP hash, so the module it runs is pinned too.
  const wasmSHA256 = "__TEGAMI_ARGON2_SHA256__";
  const decode = (value) => {
    if (typeof value !== "string" || !/^[A-Za-z0-9+/]*$/.test(value) || value.length % 4 === 1) throw new Error("encoding");
    return Uint8Array.from(atob(value), (character) => character.charCodeAt(0));
  };
  const bytesOf = (value, length) => {
    const bytes = decode(value);
    if (bytes.length !== length) throw new Error("length");
    return bytes;
  };
  // Checks an encoded length without decoding, for payloads of up to 50 MiB.
  // Their characters are checked when they are decoded for decryption.
  const encodesLength = (value, length) => typeof value === "string" && value.length === Math.ceil(length * 4 / 3);
  const validSlot = (slot) => !!slot && slot.type === "password" && slot.memory === memory && slot.passes === passes && slot.lanes === lanes;
  const validName = (name) => typeof name === "string" && name !== "" && name !== "." && name !== ".." && !/[\/\\\0\r\n]/.test(name) && text.encode(name).length <= maxNameBytes && (!name.isWellFormed || name.isWellFormed());
  const sealed = (value, length) => {
    if (!value) throw new Error("sealed");
    bytesOf(value.nonce, 12);
    return bytesOf(value.ciphertext, length);
  };
  const validate = (data, blob = false) => {
    const slot = blob ? data && data.unlock : data && Array.isArray(data.unlocks) && data.unlocks.length === 1 && data.unlocks[0];
    if (!data || data.schema !== "tegami/v1" || typeof data.id !== "string" || !/^[A-Za-z0-9_-]{22}$/.test(data.id) || !Number.isSafeInteger(data.size) || data.size < 1 || data.size > (blob ? 3221225472 : 52428800) || !validSlot(slot)) throw new Error("metadata");
    bytesOf(slot.salt, 16);
    bytesOf(slot.nonce, 12);
    bytesOf(slot.wrapped_key, 48);
    bytesOf(data.commit, 32);
    sealed(data.name, maxNameBytes + 16);
    if (blob) {
      if (data.chunk_size !== 4194304) throw new Error("chunk size");
    } else {
      if (!data.payload) throw new Error("payload");
      bytesOf(data.payload.nonce, 12);
      if (!encodesLength(data.payload.ciphertext, data.size + 16)) throw new Error("payload length");
    }
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
  const unwrap = async (data, blob, material) => {
    const slot = validate(data, blob);
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
  return {memory, passes, lanes, validate, derive, commit, unwrap, padName, openName};
})();
