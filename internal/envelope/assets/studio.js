(() => {
  "use strict";

  const text = new TextEncoder();
  const chunkBytes = 4194304;
  const maxHeaderBytes = 65536;
  const maxBlobBytes = 3221225472;
  const maxHTMLBytes = 52428800;
  const maxNameBytes = 180;
  const {memory, passes, lanes} = envelopeCrypto;
  const maxPageBytes = 134217728;
  const packagePlaceholder = "__TEGAMI_" + "PACKAGE__";
  const packageMarker = 'type="application/json">';
  const magic = text.encode("TEGAMI1\0");
  const zipMagic = Uint8Array.of(0x50, 0x4b, 0x03, 0x04);
  const canStream = typeof window.showSaveFilePicker === "function";
  // Captured before any change to the page; HTML deliveries are built from it.
  const pristine = "<!doctype html>\n" + document.documentElement.outerHTML;

  const $ = (id) => document.getElementById(id);
  const decode = (value) => Uint8Array.from(atob(value.replace(/-/g, "+").replace(/_/g, "/").padEnd(value.length + ((4 - value.length % 4) % 4), "=")), (character) => character.charCodeAt(0));
  // A codename names a delivery by its public random ID without revealing what
  // is inside. codenames.json is shared with the Go code.
  const words = JSON.parse($("tegami-codenames").textContent);
  const codename = (id) => {
    const bytes = decode(id);
    return words.adjectives[bytes[0]] + "-" + words.nouns[bytes[1]];
  };
  const b64 = (bytes) => {
    let value = "";
    for (let offset = 0; offset < bytes.length; offset += 32768) value += String.fromCharCode(...bytes.subarray(offset, offset + 32768));
    return btoa(value).replace(/=+$/, "");
  };
  const b64url = (bytes) => b64(bytes).replace(/\+/g, "-").replace(/\//g, "_");
  const random = (length) => crypto.getRandomValues(new Uint8Array(length));
  const u32 = (value) => {
    const output = new Uint8Array(4);
    new DataView(output.buffer).setUint32(0, value);
    return output;
  };
  const number = (bytes) => new DataView(bytes.buffer, bytes.byteOffset, 4).getUint32(0);
  const same = (left, right) => left.length === right.length && left.every((value, index) => value === right[index]);
  const escapeHTML = (value) => value.replace(/[&<>"']/g, (character) => "&#" + character.charCodeAt(0) + ";");
  const formatSize = (size) => {
    if (size < 1024) return size === 1 ? t("size.byte") : t("size.bytes", {n: size});
    const units = ["KiB", "MiB", "GiB"];
    let value = size / 1024;
    let unit = 0;
    while (value >= 1024 && unit < units.length - 1) {
      value /= 1024;
      unit++;
    }
    return value.toFixed(1) + " " + units[unit];
  };
  const friendly = (key, values) => Object.assign(new Error(key), {friendly: true, key, values});

  // Sources are what gets encrypted: one file as is, or several files packed
  // into an uncompressed ZIP that is generated while it is read.
  const readFile = async function* (file) {
    for (let offset = 0; offset < file.size; offset += chunkBytes) {
      const bytes = new Uint8Array(await file.slice(offset, Math.min(offset + chunkBytes, file.size)).arrayBuffer());
      if (bytes.length !== Math.min(chunkBytes, file.size - offset)) throw friendly("error.fileChanged", {name: file.name});
      yield bytes;
    }
  };
  const fileSource = (file) => ({name: file.name, size: file.size, count: 1, chunks: () => readFile(file)});
  const crcTable = new Uint32Array(256).map((_, index) => {
    let value = index;
    for (let bit = 0; bit < 8; bit++) value = value & 1 ? 0xedb88320 ^ (value >>> 1) : value >>> 1;
    return value >>> 0;
  });
  const crc32 = (crc, bytes) => {
    let value = crc ^ 0xffffffff;
    for (let index = 0; index < bytes.length; index++) value = crcTable[(value ^ bytes[index]) & 255] ^ (value >>> 8);
    return (value ^ 0xffffffff) >>> 0;
  };
  const dosTime = (milliseconds) => {
    const date = new Date(Math.max(milliseconds || 0, 315532800000));
    return {
      time: (date.getHours() << 11) | (date.getMinutes() << 5) | (date.getSeconds() >> 1),
      date: ((date.getFullYear() - 1980) << 9) | ((date.getMonth() + 1) << 5) | date.getDate(),
    };
  };
  const zipSource = (name, entries) => {
    const items = entries.map(({file, path}) => ({file, path: text.encode(path), crc: 0, offset: 0, ...dosTime(file.lastModified)}));
    const size = items.reduce((total, item) => total + 30 + item.path.length + item.file.size + 16 + 46 + item.path.length, 22);
    const chunks = async function* () {
      let offset = 0;
      for (const item of items) {
        item.offset = offset;
        item.crc = 0;
        const local = new Uint8Array(30 + item.path.length);
        const view = new DataView(local.buffer);
        view.setUint32(0, 0x04034b50, true);
        view.setUint16(4, 20, true);
        view.setUint16(6, 0x0808, true); // sizes follow the data; UTF-8 names
        view.setUint16(10, item.time, true);
        view.setUint16(12, item.date, true);
        view.setUint16(26, item.path.length, true);
        local.set(item.path, 30);
        yield local;
        for await (const bytes of readFile(item.file)) {
          item.crc = crc32(item.crc, bytes);
          yield bytes;
        }
        const descriptor = new Uint8Array(16);
        const fields = new DataView(descriptor.buffer);
        fields.setUint32(0, 0x08074b50, true);
        fields.setUint32(4, item.crc, true);
        fields.setUint32(8, item.file.size, true);
        fields.setUint32(12, item.file.size, true);
        yield descriptor;
        offset += local.length + item.file.size + 16;
      }
      const directorySize = items.reduce((total, item) => total + 46 + item.path.length, 0);
      const directory = new Uint8Array(directorySize + 22);
      const view = new DataView(directory.buffer);
      let position = 0;
      for (const item of items) {
        view.setUint32(position, 0x02014b50, true);
        view.setUint16(position + 4, 20, true);
        view.setUint16(position + 6, 20, true);
        view.setUint16(position + 8, 0x0808, true);
        view.setUint16(position + 12, item.time, true);
        view.setUint16(position + 14, item.date, true);
        view.setUint32(position + 16, item.crc, true);
        view.setUint32(position + 20, item.file.size, true);
        view.setUint32(position + 24, item.file.size, true);
        view.setUint16(position + 28, item.path.length, true);
        view.setUint32(position + 42, item.offset, true);
        directory.set(item.path, position + 46);
        position += 46 + item.path.length;
      }
      view.setUint32(position, 0x06054b50, true);
      view.setUint16(position + 8, items.length, true);
      view.setUint16(position + 10, items.length, true);
      view.setUint32(position + 12, directorySize, true);
      view.setUint32(position + 16, offset, true);
      yield directory;
    };
    return {name, size, count: items.length, chunks, zip: true};
  };
  // Regroups a source into the fixed-size plaintext chunks of a blob.
  const rechunk = async function* (source) {
    let buffer = new Uint8Array(chunkBytes);
    let filled = 0;
    for await (let piece of source.chunks()) {
      while (piece.length > 0) {
        const take = Math.min(piece.length, chunkBytes - filled);
        buffer.set(piece.subarray(0, take), filled);
        filled += take;
        piece = piece.subarray(take);
        if (filled === chunkBytes) {
          yield buffer;
          buffer = new Uint8Array(chunkBytes);
          filled = 0;
        }
      }
    }
    if (filled > 0) yield buffer.subarray(0, filled);
  };
  const ignored = /^(\.DS_Store|Thumbs\.db|desktop\.ini)$/i;
  // Turns picked or dropped files into one source; entries are {file, path}.
  const makeSource = (entries, folder) => {
    entries = entries.filter((entry) => !ignored.test(entry.file.name));
    if (entries.length === 0) throw friendly("error.noFilesHere");
    if (entries.length === 1 && !folder) return fileSource(entries[0].file);
    if (entries.length > 65535) throw friendly("error.tooMany");
    const seen = new Set();
    const unique = entries.map(({file, path}) => {
      let candidate = path;
      for (let copy = 2; seen.has(candidate.toLowerCase()); copy++) candidate = path.replace(/(\.[^.\/]*)?$/, " (" + copy + ")$1");
      seen.add(candidate.toLowerCase());
      return {file, path: candidate};
    });
    return zipSource((folder || "files") + ".zip", unique);
  };
  // Turns a drop into selection items: loose files, and folders with their
  // files listed under the folder's name.
  const droppedItems = async (transfer) => {
    const roots = Array.from(transfer.items || [], (item) => item.kind === "file" && item.webkitGetAsEntry ? item.webkitGetAsEntry() : null).filter(Boolean);
    if (roots.length === 0) return Array.from(transfer.files, (file) => ({kind: "file", file}));
    const fileOf = (entry) => new Promise((resolve, reject) => entry.file(resolve, reject));
    const walk = async (entry, prefix, entries) => {
      if (entry.isFile) {
        const file = await fileOf(entry);
        entries.push({file, path: prefix + file.name});
      } else if (entry.isDirectory) {
        const reader = entry.createReader();
        const next = () => new Promise((resolve, reject) => reader.readEntries(resolve, reject));
        for (let batch = await next(); batch.length > 0; batch = await next()) {
          for (const child of batch) await walk(child, prefix + entry.name + "/", entries);
        }
      }
    };
    const items = [];
    for (const root of roots) {
      if (root.isFile) {
        items.push({kind: "file", file: await fileOf(root)});
      } else {
        const entries = [];
        await walk(root, "", entries);
        items.push({kind: "folder", name: root.name, entries});
      }
    }
    return items;
  };

  // Matches the CLI's portable filename rule, so either side can open the other's files.
  const nameProblem = (name) => {
    if (typeof name !== "string" || name === "" || name === "." || name === "..") return "error.nameMissing";
    if (/[\/\\\0\r\n]/.test(name) || (name.isWellFormed && !name.isWellFormed())) return "error.nameChars";
    if (text.encode(name).length > maxNameBytes) return "error.nameLong";
    return "";
  };
  const password = (value) => {
    const bytes = text.encode(value);
    if (bytes.length < 12 || bytes.length > 4096 || /[\r\n\0]/.test(value)) throw friendly("error.password");
    return bytes;
  };
  const unwrap = async (data, blob, material) => {
    try {
      return await envelopeCrypto.unwrap(data, blob, material);
    } catch (_) {
      throw friendly("open.wrong");
    }
  };
  const payloadMeta = (payload) => text.encode("tegami/v1\0" + payload.id + "\0" + payload.size);
  const nameMeta = (id) => text.encode("tegami/v1\0" + id + "\0name");
  const unlockMeta = (id, type) => text.encode("tegami/v1\0" + id + "\0" + type);
  const chunkMeta = (header, index, size) => text.encode("tegami/v1\0" + header.id + "\0" + header.size + "\0" + index + "\0" + size);
  // The filename is sealed with the content key, hidden until unlocked.
  const sealName = async (content, id, name) => {
    const nonce = random(12);
    const ciphertext = new Uint8Array(await crypto.subtle.encrypt({name: "AES-GCM", iv: nonce, additionalData: nameMeta(id)}, content, envelopeCrypto.padName(name)));
    return {nonce: b64(nonce), ciphertext: b64(ciphertext)};
  };
  const openName = async (content, data, blob = false) => {
    try {
      return await envelopeCrypto.openName(content, data, blob);
    } catch (_) {
      throw friendly("open.damaged");
    }
  };
  const read = async (file, offset, length) => {
    const bytes = new Uint8Array(await file.slice(offset, offset + length).arrayBuffer());
    if (bytes.length !== length) throw friendly("open.incomplete");
    return bytes;
  };
  const download = (content, name, type) => {
    const url = URL.createObjectURL(new Blob([content], {type}));
    const link = document.createElement("a");
    link.href = url;
    link.download = name;
    document.body.append(link);
    link.click();
    link.remove();
    setTimeout(() => URL.revokeObjectURL(url), 1000);
  };

  // Page state helpers.
  const show = (output, key, state, values) => {
    if (key) label(output, key, values);
    else plain(output, "");
    if (state) output.dataset.state = state;
    else delete output.dataset.state;
  };
  const meter = (bar, value) => {
    bar.hidden = value === null;
    if (value === undefined) bar.removeAttribute("value");
    else if (value !== null) bar.value = value;
  };
  let busy = false;
  window.addEventListener("beforeunload", (event) => {
    if (busy) event.preventDefault();
  });
  const explain = (error, fallback) => {
    if (error && error.friendly) return [error.key, "error", error.values];
    if (error && error.name === "AbortError") return ["cancelled", ""];
    if (error && (error.name === "SecurityError" || error.name === "NotAllowedError")) return ["error.saveBlocked", "error"];
    if (error && error.name === "QuotaExceededError") return ["error.diskFull", "error"];
    return [fallback, "error"];
  };
  const flash = (button) => {
    const use = button.querySelector("use");
    const icon = use.getAttribute("href");
    use.setAttribute("href", "#i-check");
    setTimeout(() => use.setAttribute("href", icon), 1500);
  };
  const copyText = async (value, button) => {
    try {
      await navigator.clipboard.writeText(value);
    } catch (_) {
      const area = document.createElement("textarea");
      area.value = value;
      area.setAttribute("readonly", "");
      area.className = "sprite";
      document.body.append(area);
      area.select();
      document.execCommand("copy");
      area.remove();
    }
    flash(button);
  };

  // Theme: system, light, or dark, cycled by one button and remembered in
  // this browser. Applied after the pristine copy is taken, so HTML
  // deliveries never inherit it. Its label is set once text is available.
  const themes = {system: "#i-system", light: "#i-sun", dark: "#i-moon"};
  const themeToggle = $("theme-toggle");
  let theme = "system";
  const applyTheme = (choice) => {
    theme = choice in themes ? choice : "system";
    if (theme === "system") delete document.documentElement.dataset.theme;
    else document.documentElement.dataset.theme = theme;
    themeToggle.querySelector("use").setAttribute("href", themes[theme]);
  };
  const labelTheme = () => {
    const name = t("theme.button", {theme: t({system: "theme.system", light: "theme.light", dark: "theme.dark"}[theme])});
    themeToggle.setAttribute("aria-label", name);
    themeToggle.title = name;
  };
  try {
    applyTheme(localStorage.getItem("tegami-theme"));
  } catch (_) {
    applyTheme("system");
  }
  themeToggle.addEventListener("click", () => {
    const order = Object.keys(themes);
    applyTheme(order[(order.indexOf(theme) + 1) % order.length]);
    labelTheme();
    try {
      localStorage.setItem("tegami-theme", theme);
    } catch (_) {
      // Storage can be unavailable for local files; the choice lasts for this visit.
    }
  });

  // Language: the reader's choice if they made one here, otherwise their
  // browser's. Text is looked up by key in the embedded catalogs; label()
  // remembers each element's key so switching language re-renders it.
  const catalogs = JSON.parse($("tegami-i18n").textContent);
  const storedLanguage = () => {
    try {
      return localStorage.getItem("tegami-language");
    } catch (_) {
      return null;
    }
  };
  const browserLanguage = () => (navigator.languages || [navigator.language]).some((tag) => /^ja\b/i.test(tag || "")) ? "ja" : "en";
  let language = catalogs[storedLanguage()] ? storedLanguage() : browserLanguage();
  const template = (key) => (catalogs[language] || {})[key] || catalogs.en[key] || key;
  const t = (key, values) => template(key).replace(/\{(\w+)\}/g, (match, name) => (values && name in values ? String(values[name]) : match));
  // Fills element with a sentence whose values are wrapped in tag elements.
  const fill = (element, key, values, tag, className) => {
    element.replaceChildren(...template(key).split(/(\{\w+\})/).filter(Boolean).map((part) => {
      const name = /^\{(\w+)\}$/.exec(part);
      if (!name) return document.createTextNode(part);
      const value = document.createElement(tag);
      if (className) value.className = className;
      value.textContent = values[name[1]];
      return value;
    }));
  };
  const label = (element, key, values) => {
    element.dataset.i18n = key;
    if (values) element.dataset.i18nValues = JSON.stringify(values);
    else delete element.dataset.i18nValues;
    element.textContent = t(key, values);
  };
  const plain = (element, value) => {
    delete element.dataset.i18n;
    delete element.dataset.i18nValues;
    element.textContent = value;
  };
  const translators = [];
  translators.push(() => labelTheme());
  const applyLanguage = (choice) => {
    language = choice;
    document.documentElement.lang = choice;
    for (const element of document.querySelectorAll("[data-i18n]")) element.textContent = t(element.dataset.i18n, element.dataset.i18nValues ? JSON.parse(element.dataset.i18nValues) : null);
    for (const [attribute, name] of [["aria-label", "i18nAria"], ["title", "i18nTitle"], ["placeholder", "i18nPlaceholder"]]) {
      for (const element of document.querySelectorAll("[data-i18n-" + attribute.replace("aria-label", "aria") + "]")) element.setAttribute(attribute, t(element.dataset[name]));
    }
    for (const button of document.querySelectorAll("[data-language-choice]")) button.setAttribute("aria-checked", String(button.dataset.languageChoice === choice));
    languageMenus.label();
    for (const translate of translators) translate();
  };
  for (const button of document.querySelectorAll("[data-language-choice]")) {
    button.addEventListener("click", () => {
      applyLanguage(button.dataset.languageChoice);
      try {
        localStorage.setItem("tegami-language", button.dataset.languageChoice);
      } catch (_) {
        // Storage can be unavailable for local files; the choice lasts for this visit.
      }
    });
  }

  languageMenus.setup();

  for (const button of document.querySelectorAll(".reveal")) {
    const input = $(button.dataset.target);
    button.addEventListener("click", () => {
      const visible = input.type === "password";
      input.type = visible ? "text" : "password";
      button.querySelector("use").setAttribute("href", visible ? "#i-hide" : "#i-view");
      button.setAttribute("aria-label", t(visible ? "password.hide" : "password.show"));
      button.setAttribute("aria-pressed", String(visible));
    });
  }
  translators.push(() => {
    for (const button of document.querySelectorAll(".reveal")) button.setAttribute("aria-label", t($(button.dataset.target).type === "password" ? "password.show" : "password.hide"));
  });
  for (const zone of document.querySelectorAll(".drop")) {
    zone.addEventListener("dragenter", () => { zone.dataset.drag = ""; });
    zone.addEventListener("dragover", () => { zone.dataset.drag = ""; });
    for (const type of ["dragleave", "drop"]) zone.addEventListener(type, () => { delete zone.dataset.drag; });
  }
  // A file dropped beside a drop zone would otherwise replace this page.
  for (const type of ["dragover", "drop"]) {
    window.addEventListener(type, (event) => {
      if (!(event.target instanceof HTMLInputElement && event.target.type === "file")) event.preventDefault();
    });
  }

  const tabs = Array.from(document.querySelectorAll("[role=tab]"));
  const selectTab = (tab) => {
    for (const other of tabs) {
      const selected = other === tab;
      other.setAttribute("aria-selected", String(selected));
      other.tabIndex = selected ? 0 : -1;
      $(other.getAttribute("aria-controls")).hidden = !selected;
    }
    label($("page-title"), tab.dataset.titleKey);
  };
  tabs.forEach((tab, index) => {
    tab.addEventListener("click", () => selectTab(tab));
    tab.addEventListener("keydown", (event) => {
      const step = event.key === "ArrowDown" || event.key === "ArrowRight" ? 1 : event.key === "ArrowUp" || event.key === "ArrowLeft" ? -1 : 0;
      if (!step) return;
      const next = tabs[(index + step + tabs.length) % tabs.length];
      selectTab(next);
      next.focus();
    });
  });

  const sealForm = $("seal");
  const sealFile = $("seal-file");
  const sealFolder = $("seal-folder");
  const sealPassword = $("seal-password");
  const sealButton = $("seal-button");
  const sealStatus = $("seal-status");
  const sealProgress = $("seal-progress");
  const blobChoice = $("format-blob");
  const openForm = $("open");
  const openFile = $("open-file");
  const openPassword = $("open-password");
  const openButton = $("open-button");
  const openStatus = $("open-status");
  const openProgress = $("open-progress");
  const packageText = $("tegami-package").textContent.trim();
  let embeddedPayload = null;
  let sealItems = [];
  let sealSource = null;

  if (!canStream) {
    blobChoice.disabled = true;
    label($("blob-detail"), "format.blobUnavailable");
  }

  const sourceProblem = (source, format) => {
    if (!source) return "error.noFiles";
    if (source.size < 1) return "error.empty";
    if (source.size > maxBlobBytes) return "error.tooLarge";
    const problem = nameProblem(source.name);
    if (problem) return problem;
    if (format === "html" && source.size > maxHTMLBytes) return canStream ? "error.htmlLimit" : "error.htmlLimitNoStream";
    if (format === "blob" && !canStream) return "error.blobNeedsChromium";
    return "";
  };
  const itemSize = (item) => item.kind === "folder" ? item.entries.reduce((total, entry) => total + entry.file.size, 0) : item.file.size;
  const renderSeal = () => {
    const list = $("seal-list");
    list.replaceChildren();
    sealItems.forEach((item, index) => {
      const row = $("file-row").content.firstElementChild.cloneNode(true);
      const name = item.kind === "folder" ? t(item.entries.length === 1 ? "files.folderOne" : "files.folderMany", {name: item.name, count: item.entries.length}) : item.file.name;
      row.querySelector("use").setAttribute("href", item.kind === "folder" ? "#i-folder" : "#i-file");
      row.querySelector(".file-row-name").textContent = name;
      row.querySelector(".file-row-name").title = name;
      row.querySelector(".file-row-size").textContent = formatSize(itemSize(item));
      const remove = row.querySelector("button");
      remove.setAttribute("aria-label", t("files.remove", {name}));
      remove.addEventListener("click", () => {
        sealItems.splice(index, 1);
        updateSeal();
        sealFile.focus();
      });
      list.append(row);
    });
    const empty = sealItems.length === 0;
    list.hidden = empty;
    $("seal-clear").hidden = empty;
    const zone = $("seal-drop");
    if (empty) delete zone.dataset.compact;
    else zone.dataset.compact = "";
    label($("seal-drop-title"), empty ? "drop.title" : "drop.more");
    label($("seal-drop-detail"), empty ? "drop.detail" : "drop.moreDetail");
  };
  translators.push(renderSeal);
  const renderSummary = () => {
    const summary = $("seal-summary");
    if (!sealSource) {
      label(summary, "summary.none");
      return;
    }
    const counted = sealSource.count === 1 ? t("summary.one", {size: formatSize(sealSource.size)}) : t("summary.many", {count: sealSource.count, size: formatSize(sealSource.size)});
    plain(summary, sealSource.zip ? t("summary.zip", {summary: counted, name: sealSource.name}) : counted);
  };
  translators.push(renderSummary);
  const updateSeal = () => {
    show(sealStatus, "");
    sealSource = null;
    const folder = sealItems.length === 1 && sealItems[0].kind === "folder" ? sealItems[0].name : "";
    if (sealItems.length > 0) {
      const entries = sealItems.flatMap((item) => item.kind === "folder" ? item.entries : [{file: item.file, path: item.file.name}]);
      try {
        sealSource = makeSource(entries, folder);
      } catch (error) {
        show(sealStatus, ...explain(error, "error.unreadable"));
      }
    }
    if (sealSource) {
      if (sealSource.size > maxHTMLBytes && sealSource.size <= maxBlobBytes && canStream && !blobChoice.checked) {
        blobChoice.checked = true;
        show(sealStatus, "switched");
      }
      const problem = sourceProblem(sealSource, sealForm.elements.format.value);
      if (problem) show(sealStatus, problem, "error");
    }
    renderSummary();
    renderSeal();
  };
  const sameFile = (left, right) => left.name === right.name && left.size === right.size && left.lastModified === right.lastModified;
  const addItems = (items) => {
    for (const item of items) {
      if (item.kind === "file" && sealItems.some((existing) => existing.kind === "file" && sameFile(existing.file, item.file))) continue;
      sealItems.push(item);
    }
    // Start with a strong password; the sender can still type their own.
    if (sealItems.length > 0 && !sealPassword.value) generatePassword();
    updateSeal();
  };
  const pickedItems = (input) => {
    const files = Array.from(input.files);
    input.value = "";
    if (files.length > 0 && files[0].webkitRelativePath) {
      return [{kind: "folder", name: files[0].webkitRelativePath.split("/")[0], entries: files.map((file) => ({file, path: file.webkitRelativePath}))}];
    }
    return files.map((file) => ({kind: "file", file}));
  };
  sealFile.addEventListener("change", () => addItems(pickedItems(sealFile)));
  sealFolder.addEventListener("change", () => addItems(pickedItems(sealFolder)));
  $("seal-folder-button").addEventListener("click", () => sealFolder.click());
  $("seal-clear").addEventListener("click", () => {
    sealItems = [];
    updateSeal();
  });
  $("seal-drop").addEventListener("drop", (event) => {
    event.preventDefault();
    droppedItems(event.dataTransfer).then(addItems, () => show(sealStatus, "error.unreadable", "error"));
  });
  sealForm.addEventListener("change", (event) => {
    if (event.target.name === "format" && sealSource) {
      const problem = sourceProblem(sealSource, event.target.value);
      show(sealStatus, problem, problem ? "error" : "");
    }
  });

  // The strength bar is a rough guide, not a guarantee: length counts, and
  // common words, years, repeats, and keyboard runs are flagged as easy to
  // guess, because cracking tools try those first.
  const passwordAlphabet = "abcdefghijkmnpqrstuvwxyz23456789";
  const guessable = [
    /(.)\1{2,}/,
    /0123|1234|2345|3456|4567|5678|6789|7890|9876|8765|7654|6543|5432|4321|abcd|bcde|cdef|qwer|wert|erty|asdf|sdfg|zxcv|xcvb/,
    /(19|20)\d\d/,
    /pass|letmein|welcome|admin|login|qwerty|iloveyou|monkey|dragon|secret|summer|winter|spring|autumn|tegami/,
  ];
  let generatedPassword = "";
  const assess = (value) => {
    const length = text.encode(value).length;
    if (length === 0) return [0, "strength.empty"];
    if (length < 12) return [1, "strength.short"];
    if (value === generatedPassword) return [4, "strength.generated"];
    if (new Set(value).size < 6) return [1, "strength.repetitive"];
    if (guessable.some((pattern) => pattern.test(value.toLowerCase()))) return [2, "strength.guessable"];
    return length < 16 ? [2, "strength.okay"] : length < 20 ? [3, "strength.good"] : [4, "strength.strong"];
  };
  const updateStrength = () => {
    const [level, hint] = assess(sealPassword.value);
    $("seal-strength").dataset.level = String(level);
    label($("seal-strength-label"), hint);
  };
  // 20 characters from a 32-character alphabet: 100 random bits.
  const generatePassword = () => {
    const bytes = random(20);
    let value = "";
    bytes.forEach((byte, index) => {
      if (index > 0 && index % 5 === 0) value += "-";
      value += passwordAlphabet[byte & 31];
    });
    bytes.fill(0);
    generatedPassword = value;
    sealPassword.value = value;
    if (sealPassword.type === "password") sealForm.querySelector(".reveal").click();
    updateStrength();
  };
  sealPassword.addEventListener("input", updateStrength);
  $("seal-generate").addEventListener("click", generatePassword);
  $("seal-copy").addEventListener("click", (event) => {
    if (sealPassword.value) copyText(sealPassword.value, event.currentTarget);
  });
  $("seal-result-copy").addEventListener("click", (event) => copyText(sealPassword.value, event.currentTarget));
  $("seal-again").addEventListener("click", () => {
    sealItems = [];
    sealPassword.value = "";
    generatedPassword = "";
    updateStrength();
    updateSeal();
    $("seal-result").hidden = true;
    sealForm.hidden = false;
    sealFile.focus();
  });

  const createStandalone = async (source, material) => {
    const plain = new Uint8Array(source.size);
    let filled = 0;
    for await (const piece of source.chunks()) {
      if (filled + piece.length > plain.length) throw friendly("error.changed");
      plain.set(piece, filled);
      filled += piece.length;
    }
    if (filled !== plain.length) throw friendly("error.changed");
    const nonce = random(12);
    const contentKey = random(32);
    const salt = random(16);
    const wrapNonce = random(12);
    const payload = {schema: "tegami/v1", id: b64url(random(16)), size: plain.length, name: null, payload: {nonce: b64(nonce), ciphertext: ""}, unlocks: []};
    try {
      const content = await crypto.subtle.importKey("raw", contentKey, "AES-GCM", false, ["encrypt"]);
      const ciphertext = await crypto.subtle.encrypt({name: "AES-GCM", iv: nonce, additionalData: payloadMeta(payload)}, content, plain);
      payload.name = await sealName(content, payload.id, source.name);
      payload.commit = b64(await envelopeCrypto.commit(contentKey, payload.id));
      const wrapping = await envelopeCrypto.derive(material, salt);
      const wrapped = await crypto.subtle.encrypt({name: "AES-GCM", iv: wrapNonce, additionalData: unlockMeta(payload.id, "password")}, wrapping, contentKey);
      payload.payload.ciphertext = b64(new Uint8Array(ciphertext));
      payload.unlocks = [{type: "password", salt: b64(salt), nonce: b64(wrapNonce), wrapped_key: b64(new Uint8Array(wrapped)), memory, passes, lanes}];
    } finally {
      contentKey.fill(0);
      plain.fill(0);
    }
    const marker = packageMarker + packagePlaceholder + "<\/script>";
    if (!pristine.includes(marker)) throw friendly("error.modified");
    const name = codename(payload.id);
    const encoded = JSON.stringify(payload).replace(/</g, "\\u003c");
    const html = pristine
      .replace(marker, () => packageMarker + encoded + "<\/script>")
      .replace("<title>Tegami</title>", () => "<title>" + escapeHTML(name) + "</title>")
      .replace("<body>", () => '<body data-mode="envelope">');
    download(html, name + ".html", "text/html");
    return {file: name + ".html", codename: name};
  };

  const createBlob = async (source, material) => {
    const id = b64url(random(16));
    const name = codename(id);
    const handle = await showSaveFilePicker({suggestedName: name + ".tegami", types: [{description: t("format.blob"), accept: {"application/vnd.tegami": [".tegami"]}}]});
    const contentKey = random(32);
    const salt = random(16);
    const wrapNonce = random(12);
    const header = {schema: "tegami/v1", id, size: source.size, name: null, chunk_size: chunkBytes, unlock: {type: "password", salt: b64(salt), nonce: b64(wrapNonce), wrapped_key: "", memory, passes, lanes}};
    let writer;
    try {
      show(sealStatus, "encrypting");
      meter(sealProgress, 0);
      const content = await crypto.subtle.importKey("raw", contentKey, "AES-GCM", false, ["encrypt"]);
      header.name = await sealName(content, id, source.name);
      header.commit = b64(await envelopeCrypto.commit(contentKey, id));
      const wrapping = await envelopeCrypto.derive(material, salt);
      const wrapped = await crypto.subtle.encrypt({name: "AES-GCM", iv: wrapNonce, additionalData: unlockMeta(id, "password")}, wrapping, contentKey);
      header.unlock.wrapped_key = b64(new Uint8Array(wrapped));
      const headerBytes = text.encode(JSON.stringify(header));
      if (headerBytes.length > maxHeaderBytes) throw friendly("error.headerTooLong");
      writer = await handle.createWritable();
      await writer.write(magic);
      await writer.write(u32(headerBytes.length));
      await writer.write(headerBytes);
      let done = 0;
      let index = 0;
      for await (const plain of rechunk(source)) {
        if (done + plain.length > source.size) throw friendly("error.changed");
        const nonce = random(12);
        const ciphertext = new Uint8Array(await crypto.subtle.encrypt({name: "AES-GCM", iv: nonce, additionalData: chunkMeta(header, index, plain.length)}, content, plain));
        plain.fill(0);
        await writer.write(u32(ciphertext.length));
        await writer.write(nonce);
        await writer.write(ciphertext);
        done += ciphertext.length - 16;
        index++;
        meter(sealProgress, done / source.size);
        show(sealStatus, "encryptingPercent", "", {percent: Math.floor(100 * done / source.size)});
      }
      if (done !== source.size) throw friendly("error.changed");
      await writer.close();
      writer = null;
      return {file: handle.name, codename: name};
    } finally {
      contentKey.fill(0);
      if (writer) await writer.abort();
    }
  };

  let lastResult = null;
  const renderResult = () => {
    if (!lastResult) return;
    fill($("seal-result-send"), "result.send", {file: lastResult.file}, "strong");
    fill($("seal-result-share"), "result.share", {codename: lastResult.codename}, "strong");
    plain($("seal-result-file"), t("result.file", {file: lastResult.file, size: formatSize(lastResult.size)}));
  };
  translators.push(renderResult);

  sealForm.addEventListener("submit", async (event) => {
    event.preventDefault();
    show(sealStatus, "");
    const format = sealForm.elements.format.value;
    const problem = sourceProblem(sealSource, format);
    if (problem) {
      show(sealStatus, problem, "error");
      return;
    }
    let material;
    try {
      material = password(sealPassword.value);
    } catch (error) {
      show(sealStatus, error.key, "error");
      sealPassword.focus();
      return;
    }
    busy = true;
    sealButton.disabled = true;
    try {
      let saved;
      if (format === "html") {
        show(sealStatus, "encrypting");
        meter(sealProgress);
        saved = await createStandalone(sealSource, material);
      } else {
        saved = await createBlob(sealSource, material);
      }
      show(sealStatus, "");
      lastResult = {file: saved.file, codename: saved.codename, size: sealSource.size};
      renderResult();
      $("seal-result-blob").hidden = format !== "blob";
      sealForm.hidden = true;
      $("seal-result").hidden = false;
      $("seal-result-copy").focus();
    } catch (error) {
      show(sealStatus, ...explain(error, "error.encryptFailed"));
    } finally {
      material.fill(0);
      busy = false;
      sealButton.disabled = false;
      meter(sealProgress, null);
    }
  });

  // Until a delivery is unlocked, it is known only by its codename.
  const validID = (id) => typeof id === "string" && /^[A-Za-z0-9_-]{22}$/.test(id);

  const readHeader = async (file) => {
    const invalid = friendly("open.notTegami");
    if (file.size < 12) throw invalid;
    const prefix = await read(file, 0, 12);
    if (!same(prefix.subarray(0, 8), magic)) throw invalid;
    const headerLength = number(prefix.subarray(8));
    if (headerLength < 1 || headerLength > maxHeaderBytes || file.size < 12 + headerLength) throw invalid;
    let header;
    try {
      header = JSON.parse(new TextDecoder("utf-8", {fatal: true}).decode(await read(file, 12, headerLength)));
    } catch (_) {
      throw invalid;
    }
    try {
      envelopeCrypto.validate(header, true);
    } catch (_) {
      throw invalid;
    }
    return {header, offset: 12 + headerLength};
  };

  // Treat HTML as a bounded UTF-8 string, never a DOM, iframe, or navigation.
  // Match the CLI's exact package marker and parse only the JSON following it.
  const readDelivery = async (file) => {
    const prefix = new Uint8Array(await file.slice(0, 8).arrayBuffer());
    if (same(prefix, magic)) return {blob: true, ...await readHeader(file)};
    if (same(prefix.subarray(0, 4), zipMagic)) throw friendly("open.zipFile");
    if (file.size > maxPageBytes) throw friendly("open.notTegami");
    const page = new TextDecoder("utf-8", {fatal: true}).decode(await file.arrayBuffer());
    const marker = '<script id="tegami-package" type="application/json">';
    const start = page.indexOf(marker);
    const end = page.indexOf("<\/script>", start + marker.length);
    if (start < 0 || end < 0) throw friendly("open.notTegami");
    try {
      const payload = JSON.parse(page.slice(start + marker.length, end));
      envelopeCrypto.validate(payload);
      return {blob: false, payload};
    } catch (_) {
      throw friendly("open.notTegami");
    }
  };

  openFile.addEventListener("change", async () => {
    const file = openFile.files[0];
    const zone = $("open-drop");
    show(openStatus, "");
    plain($("open-summary"), "");
    if (!file) {
      delete zone.dataset.compact;
      label($("open-file-name"), "open.drop");
      label($("open-file-detail"), "open.dropDetail");
      return;
    }
    zone.dataset.compact = "";
    plain($("open-file-name"), file.name);
    plain($("open-file-detail"), formatSize(file.size));
    try {
      const delivery = await readDelivery(file);
      if (openFile.files[0] !== file) return;
      const data = delivery.blob ? delivery.header : delivery.payload;
      plain($("open-file-name"), codename(data.id));
      label($("open-file-detail"), "open.encrypted", {size: formatSize(data.size)});
      if (delivery.blob && !canStream) show(openStatus, "error.blobNeedsChromium", "error");
    } catch (error) {
      if (openFile.files[0] === file) show(openStatus, ...explain(error, "open.notTegami"));
    }
  });

  const decryptEmbedded = async (payload, material) => {
    const damaged = friendly("open.damaged");
    try {
      envelopeCrypto.validate(payload);
    } catch (_) {
      throw damaged;
    }
    show(openStatus, "open.decrypting");
    meter(openProgress);
    const content = await unwrap(payload, false, material);
    let plaintext;
    try {
      plaintext = new Uint8Array(await crypto.subtle.decrypt({name: "AES-GCM", iv: decode(payload.payload.nonce), additionalData: payloadMeta(payload)}, content, decode(payload.payload.ciphertext)));
    } catch (_) {
      throw friendly("open.wrong");
    }
    try {
      if (plaintext.byteLength !== payload.size) throw damaged;
      const filename = await openName(content, payload);
      download(plaintext, filename, "application/octet-stream");
      return ["open.downloaded", {name: filename}];
    } finally {
      plaintext.fill(0);
    }
  };

  const decryptBlob = async (file, material) => {
    if (!canStream) throw friendly("error.blobNeedsChromium");
    const {header, offset: start} = await readHeader(file);
    show(openStatus, "open.checking");
    meter(openProgress);
    const content = await unwrap(header, true, material);
    const filename = await openName(content, header, true);
    const handle = await showSaveFilePicker({suggestedName: filename});
    let writer = await handle.createWritable();
    try {
      let offset = start;
      let remaining = header.size;
      let index = 0;
      while (remaining > 0) {
        const length = number(await read(file, offset, 4));
        offset += 4;
        if (length < 17 || length > chunkBytes + 16) throw friendly("open.damaged");
        const nonce = await read(file, offset, 12);
        offset += 12;
        const ciphertext = await read(file, offset, length);
        offset += length;
        const expected = Math.min(chunkBytes, remaining);
        let plain;
        try {
          plain = new Uint8Array(await crypto.subtle.decrypt({name: "AES-GCM", iv: nonce, additionalData: chunkMeta(header, index, expected)}, content, ciphertext));
        } catch (_) {
          throw friendly("open.damaged");
        }
        if (plain.length !== expected) throw friendly("open.damaged");
        try {
          await writer.write(plain);
          remaining -= plain.length;
        } finally {
          plain.fill(0);
        }
        index++;
        const done = (header.size - remaining) / header.size;
        meter(openProgress, done);
        show(openStatus, "open.decryptingPercent", "", {percent: Math.floor(100 * done)});
      }
      if (offset !== file.size) throw friendly("open.trailing");
      await writer.close();
      writer = null;
      return ["open.saved", {name: handle.name}];
    } finally {
      if (writer) await writer.abort();
    }
  };

  if (packageText !== packagePlaceholder) {
    document.body.dataset.mode = "envelope";
    openFile.disabled = true;
    openButton.disabled = false;
    label($("open-label"), "open.buttonDownload");
    try {
      embeddedPayload = JSON.parse(packageText);
      const name = embeddedPayload.schema === "tegami/v1" && validID(embeddedPayload.id) ? codename(embeddedPayload.id) : "";
      if (!name) throw new Error("damaged");
      $("envelope-name").textContent = name;
      const renderEnvelopeSize = () => fill($("envelope-size"), "envelope.size", {size: formatSize(embeddedPayload.size)}, "span", "mono");
      renderEnvelopeSize();
      translators.push(renderEnvelopeSize);
      document.title = name;
    } catch (_) {
      show(openStatus, "open.damaged", "error");
      openButton.disabled = true;
    }
    openPassword.focus();
  }

  openForm.addEventListener("submit", async (event) => {
    event.preventDefault();
    show(openStatus, "");
    const file = openFile.files[0];
    if (!embeddedPayload && !file) {
      show(openStatus, "open.chooseFile", "error");
      return;
    }
    let material;
    try {
      material = password(openPassword.value);
    } catch (error) {
      show(openStatus, "open.passwordShort", "error");
      openPassword.focus();
      return;
    }
    busy = true;
    openButton.disabled = true;
    try {
      const delivery = embeddedPayload ? {blob: false, payload: embeddedPayload} : await readDelivery(file);
      const [key, values] = delivery.blob ? await decryptBlob(file, material) : await decryptEmbedded(delivery.payload, material);
      show(openStatus, key, "success", values);
      openPassword.value = "";
    } catch (error) {
      show(openStatus, ...explain(error, "open.wrong"));
      if (error && error.friendly && /password/.test(error.message)) openPassword.select();
    } finally {
      material.fill(0);
      busy = false;
      openButton.disabled = false;
      meter(openProgress, null);
    }
  });

  // Translate last, once every element and translator exists.
  applyLanguage(language);
})();
