(() => {
  "use strict";
  const $ = (id) => document.getElementById(id);
  const form = $("open-form");
  const password = $("password");
  const reveal = $("reveal");
  const button = $("open-button");
  const buttonLabel = $("open-label");
  const status = $("status");
  const packageData = JSON.parse($("tegami-package").textContent);

  // Language: the reader's choice if they made one here, otherwise their
  // browser's. The page is rendered in English and translated in place.
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
  const t = (key, values) => ((catalogs[language] || {})[key] || catalogs.en[key] || key).replace(/\{(\w+)\}/g, (match, name) => (values && name in values ? String(values[name]) : match));
  const fill = (element, key, values, className) => {
    const template = (catalogs[language] || {})[key] || catalogs.en[key];
    element.replaceChildren(...template.split(/(\{\w+\})/).filter(Boolean).map((part) => {
      const name = /^\{(\w+)\}$/.exec(part);
      if (!name) return document.createTextNode(part);
      const value = document.createElement("span");
      value.className = className;
      value.textContent = values[name[1]];
      return value;
    }));
  };
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
  let lastStatus = null;
  const show = (key, values, state) => {
    lastStatus = key ? {key, values, state} : null;
    status.textContent = key ? t(key, values) : "";
    if (state) status.dataset.state = state;
    else delete status.dataset.state;
  };
  const applyLanguage = (choice) => {
    language = choice;
    document.documentElement.lang = choice;
    for (const element of document.querySelectorAll("[data-i18n]")) element.textContent = t(element.dataset.i18n);
    for (const element of document.querySelectorAll("[data-i18n-aria]")) element.setAttribute("aria-label", t(element.dataset.i18nAria));
    for (const element of document.querySelectorAll("[data-i18n-placeholder]")) element.setAttribute("placeholder", t(element.dataset.i18nPlaceholder));
    for (const choiceButton of document.querySelectorAll("[data-language-choice]")) choiceButton.setAttribute("aria-checked", String(choiceButton.dataset.languageChoice === choice));
    languageMenus.label();
    fill($("file-size"), "envelope.size", {size: formatSize(packageData.size)}, "mono");
    reveal.setAttribute("aria-label", t(password.type === "password" ? "password.show" : "password.hide"));
    if (lastStatus) show(lastStatus.key, lastStatus.values, lastStatus.state);
  };
  for (const choiceButton of document.querySelectorAll("[data-language-choice]")) {
    choiceButton.addEventListener("click", () => {
      applyLanguage(choiceButton.dataset.languageChoice);
      try {
        localStorage.setItem("tegami-language", choiceButton.dataset.languageChoice);
      } catch (_) {
        // Storage can be unavailable for local files; the choice lasts for this visit.
      }
    });
  }
  languageMenus.setup();
  applyLanguage(language);

  const decode = (value) => {
    const base64 = value.replace(/-/g, "+").replace(/_/g, "/").padEnd(value.length + ((4 - value.length % 4) % 4), "=");
    return Uint8Array.from(atob(base64), (character) => character.charCodeAt(0));
  };
  const encoded = (value) => new TextEncoder().encode(value);
  const payloadMetadata = encoded("tegami/v1\0" + packageData.id + "\0" + packageData.size);
  reveal.addEventListener("click", () => {
    const visible = password.type === "password";
    password.type = visible ? "text" : "password";
    reveal.querySelector("use").setAttribute("href", visible ? "#i-hide" : "#i-view");
    reveal.setAttribute("aria-label", t(visible ? "password.hide" : "password.show"));
    reveal.setAttribute("aria-pressed", String(visible));
    password.focus();
  });
  form.addEventListener("submit", async (event) => {
    event.preventDefault();
    show("");
    const passwordBytes = encoded(password.value);
    if (passwordBytes.length < 12 || passwordBytes.length > 4096 || /[\r\n\0]/.test(password.value)) {
      show("open.passwordShort", null, "error");
      return;
    }
    button.disabled = true;
    buttonLabel.textContent = t("open.decrypting");
    try {
      const contentKey = await envelopeCrypto.unwrap(packageData, false, passwordBytes);
      const plaintext = await crypto.subtle.decrypt({ name: "AES-GCM", iv: decode(packageData.payload.nonce), additionalData: payloadMetadata, tagLength: 128 }, contentKey, decode(packageData.payload.ciphertext));
      if (plaintext.byteLength !== packageData.size) throw new Error("size mismatch");
      // The real filename is sealed with the file and only revealed once it opens.
      const filename = await envelopeCrypto.openName(contentKey, packageData);
      const url = URL.createObjectURL(new Blob([plaintext], { type: "application/octet-stream" }));
      const link = document.createElement("a");
      link.href = url;
      link.download = filename;
      document.body.append(link);
      link.click();
      link.remove();
      setTimeout(() => URL.revokeObjectURL(url), 1000);
      password.value = "";
      show("open.downloaded", {name: filename}, "success");
    } catch (_) {
      show("open.wrong", null, "error");
      password.select();
    } finally {
      passwordBytes.fill(0);
      button.disabled = false;
      buttonLabel.textContent = t("open.buttonDownload");
    }
  });
})();
