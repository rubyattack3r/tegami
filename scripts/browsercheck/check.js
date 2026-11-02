// No test framework or dependencies: exercise the real embedded pages in
// same-origin frames, intercept downloads and transactional save handles, and
// send browser-created deliveries to the CLI through the local test server.
(async () => {
  "use strict";
  const results = document.getElementById("results");
  const lines = [];
  const password = "correct horse battery staple";
  const message = "Browser and CLI round trip.\n";
  const text = new TextEncoder();
  const assert = (condition, reason) => { if (!condition) throw new Error(reason); };
  const wait = async (condition) => {
    const deadline = Date.now() + 20000;
    while (!condition()) {
      if (Date.now() > deadline) throw new Error("Timed out waiting for page");
      await new Promise((resolve) => setTimeout(resolve, 20));
    }
  };
  const test = async (name, run) => {
    try {
      await run();
      lines.push("PASS " + name);
    } catch (error) {
      lines.push("FAIL " + name + ": " + error.stack);
      window.failures++;
    }
    results.textContent = lines.join("\n");
  };
  window.failures = 0;
  const load = async (url) => {
    const frame = document.createElement("iframe");
    document.getElementById("pages").append(frame);
    const loaded = new Promise((resolve) => frame.addEventListener("load", resolve, {once: true}));
    frame.src = url;
    await loaded;
    const w = frame.contentWindow;
    const d = frame.contentDocument;
    const state = {downloads: [], saves: [], transactions: [], violations: []};
    d.addEventListener("securitypolicyviolation", (event) => state.violations.push(event.violatedDirective));
    w.URL.createObjectURL = (blob) => {
      state.downloads.push({blob});
      return "blob:browser-check";
    };
    w.URL.revokeObjectURL = () => {};
    w.HTMLAnchorElement.prototype.click = function () {
      state.downloads.at(-1).name = this.download;
    };
    w.showSaveFilePicker = async (options) => {
      const transaction = {parts: [], closed: false, aborted: false, name: options.suggestedName};
      state.transactions.push(transaction);
      return {
        name: options.suggestedName,
        createWritable: async () => ({
          write: async (value) => transaction.parts.push(new Uint8Array(value).slice()),
          close: async () => {
            transaction.closed = true;
            state.saves.push({name: transaction.name, blob: new Blob(transaction.parts)});
          },
          abort: async () => { transaction.aborted = true; transaction.parts = []; },
        }),
      };
    };
    const file = async (id, files) => {
      const transfer = new w.DataTransfer();
      files.forEach((item) => transfer.items.add(item));
      d.getElementById(id).files = transfer.files;
      d.getElementById(id).dispatchEvent(new w.Event("change", {bubbles: true}));
      await new Promise((resolve) => setTimeout(resolve, 30));
    };
    const open = async (source, value = password) => {
      d.getElementById("tab-open").click();
      await file("open-file", [source]);
      d.getElementById("open-password").value = value;
      d.getElementById("open").requestSubmit();
      await wait(() => !d.getElementById("open-button").disabled && !!d.getElementById("open-status").dataset.state);
      return d.getElementById("open-status").dataset.state;
    };
    const seal = async (files, format = "html") => {
      d.getElementById("tab-send").click();
      if (!d.getElementById("seal-result").hidden) d.getElementById("seal-again").click();
      await file("seal-file", files);
      d.querySelector('input[name="format"][value="' + format + '"]').checked = true;
      d.getElementById("seal-password").value = password;
      d.getElementById("seal").requestSubmit();
      await wait(() => !d.getElementById("seal-button").disabled);
      assert(!d.getElementById("seal-result").hidden, d.getElementById("seal-status").textContent);
      return format === "html" ? state.downloads.at(-1) : state.saves.at(-1);
    };
    return {frame, w, d, state, file, open, seal};
  };
  const fixture = async (path, name) => new File([await (await fetch(path)).arrayBuffer()], name);
  const packageOf = async (blob) => {
    const html = await blob.text();
    const marker = '<script id="tegami-package" type="application/json">';
    const start = html.indexOf(marker) + marker.length;
    return JSON.parse(html.slice(start, html.indexOf("<\/script>", start)));
  };
  const packageFile = (data, prefix = "") => new File([prefix, '<script id="tegami-package" type="application/json">', JSON.stringify(data), "<\/script>"], "delivery.html");
  const cliCheck = async (kind, blob) => {
    const response = await fetch("/result/" + kind, {method: "POST", body: blob});
    assert(response.ok, await response.text());
  };
  // HTML deliveries carry their records after the package element; these
  // helpers take a page apart and put it back together in another order.
  const recordStart = (index) => '\n<script id="tegami-r' + index + '" type="application/octet-stream">';
  const splitRecords = (html) => {
    let at = html.indexOf(recordStart(0));
    assert(at > 0, "page has no records");
    const before = html.slice(0, at);
    const records = [];
    for (let index = 0; html.startsWith(recordStart(index), at); index++) {
      const end = html.indexOf("<\/script>", at) + 9;
      records.push(html.slice(at, end));
      at = end;
    }
    return {before, records, after: html.slice(at)};
  };
  const renumber = (record, from, to) => record.replace(recordStart(from), recordStart(to));
  const damagedRecords = (html) => {
    const {before, records: [first, second], after} = splitRecords(html);
    assert(first && second, "page needs two records");
    const at = recordStart(1).length + 40;
    const flipped = second.slice(0, at) + (second[at] === "A" ? "B" : "A") + second.slice(at + 1);
    return {
      flipped: before + first + flipped + after,
      dropped: before + first + after,
      swapped: before + renumber(second, 1, 0) + renumber(first, 0, 1) + after,
      repeated: before + first + renumber(first, 0, 1) + after,
      extra: before + first + second + renumber(second, 1, 2) + after,
      separated: before + first + "\n" + second + after,
    };
  };
  const htmlFile = (html) => new File([html], "delivery.html");
  const pageOf = async (html) => {
    const url = URL.createObjectURL(new Blob([html], {type: "text/html"}));
    const page = await load(url);
    URL.revokeObjectURL(url);
    return page;
  };
  // Opens a CLI delivery page in a frame and submits the password.
  const unlockPage = async (html, value = password) => {
    const page = await pageOf(html);
    page.d.getElementById("password").value = value;
    page.d.getElementById("open-form").requestSubmit();
    await wait(() => page.d.getElementById("status").dataset.state);
    return page;
  };
  // Opens a Studio-made delivery in a frame and submits the password.
  const unlockStudioPage = async (html) => {
    const page = await pageOf(html);
    page.d.getElementById("open-password").value = password;
    page.d.getElementById("open").requestSubmit();
    await wait(() => page.d.getElementById("open-status").dataset.state);
    return page;
  };
  const isFootage = async (blob) => {
    const bytes = new Uint8Array(await blob.arrayBuffer());
    return bytes.length === 4194311 && bytes.every((b) => b === 42);
  };
  let studio, cli;
  let madeHTML, madeLarge;
  try {
    studio = await load("/fixtures/tegami.html");
    const cliHTML = await fixture("/fixtures/cli.html", "delivery.html");
    const cliLarge = await fixture("/fixtures/cli-large.html", "delivery.html");
    const cliBlob = await fixture("/fixtures/cli.tegami", "delivery.tegami");
    await test("RFC 9106 section 5.3 Argon2id vector", async () => {
      const wasm = Uint8Array.from(atob(studio.d.getElementById("tegami-argon2").textContent), (c) => c.charCodeAt(0));
      const {instance} = await WebAssembly.instantiate(wasm);
      assert(WebAssembly.Module.imports(await WebAssembly.compile(wasm)).length === 0, "WASM has imports");
      const work = Math.ceil(Number(instance.exports.__heap_base.value) / 8) * 8;
      const output = work + 32768;
      const pass = output + 32, salt = pass + 32, key = salt + 16, ad = key + 8;
      instance.exports.memory.grow(1);
      const bytes = new Uint8Array(instance.exports.memory.buffer);
      bytes.fill(1, pass, salt);
      bytes.fill(2, salt, key);
      bytes.fill(3, key, ad);
      bytes.fill(4, ad, ad + 12);
      instance.exports.argon2id(output, 32, work, 32, 3, 4, pass, 32, salt, 16, key, 8, ad, 12);
      const hex = Array.from(bytes.subarray(output, output + 32), (b) => b.toString(16).padStart(2, "0")).join("");
      assert(hex === "0d640df58d78766c08c037a34a8b53c9d01ef0452d75b65eb52520e96b01e659", "RFC vector: " + hex);
      bytes.fill(0);
    });
    await test("CLI HTML opens in Studio", async () => {
      assert(await studio.open(cliHTML) === "success", studio.d.getElementById("open-status").textContent);
      const saved = studio.state.downloads.at(-1);
      assert(saved.name === "報告書.txt" && await saved.blob.text() === message, "HTML output mismatch");
    });
    await test("CLI HTML with two records opens in Studio", async () => {
      assert(await studio.open(cliLarge) === "success", studio.d.getElementById("open-status").textContent);
      const saved = studio.state.downloads.at(-1);
      assert(saved.name === "footage.bin" && await isFootage(saved.blob), "HTML output mismatch");
    });
    await test("0.1.0 HTML points to its own page in Studio", async () => {
      const header = await packageOf(cliHTML);
      const old = {schema: header.schema, id: header.id, size: header.size, commit: header.commit, name: header.name, payload: header.name, unlocks: [header.unlock]};
      assert(await studio.open(packageFile(old)) === "error", "0.1.0 HTML accepted");
      assert(studio.d.getElementById("open-status").textContent.includes("older version"), studio.d.getElementById("open-status").textContent);
    });
    await test("CLI streaming delivery opens in Studio", async () => {
      assert(await studio.open(cliBlob) === "success", studio.d.getElementById("open-status").textContent);
      const saved = studio.state.saves.at(-1);
      assert(saved.name === "footage.bin" && await isFootage(saved.blob), "stream output mismatch");
    });
    await test("Damaged last chunk aborts without publishing output", async () => {
      const bytes = new Uint8Array(await cliBlob.arrayBuffer());
      bytes[bytes.length - 1] ^= 1;
      const savedBefore = studio.state.saves.length;
      assert(await studio.open(new File([bytes], "damaged.tegami")) === "error", "damaged delivery accepted");
      const transaction = studio.state.transactions.at(-1);
      assert(transaction.aborted && !transaction.closed && transaction.parts.length === 0 && studio.state.saves.length === savedBefore, "partial output published");
    });
    await test("Damaged, reordered, or extra HTML records produce no output in Studio", async () => {
      const before = studio.state.downloads.length;
      for (const [name, html] of Object.entries(damagedRecords(await cliLarge.text()))) {
        assert(await studio.open(htmlFile(html)) === "error", name + " records accepted");
      }
      assert(studio.state.downloads.length === before, "damaged records published output");
    });
    await test("Wrong password produces no output", async () => {
      const before = studio.state.downloads.length;
      assert(await studio.open(cliHTML, "an incorrect password") === "error", "wrong password accepted");
      assert(studio.state.downloads.length === before, "wrong password published output");
    });
    await test("Malicious HTML remains data", async () => {
      const malicious = '<script>parent.senderCodeRan=true;<\/script><img src="/sender-request" onerror="parent.senderCodeRan=true"><iframe src="/sender-frame"></iframe><body onload="parent.senderCodeRan=true">';
      window.senderCodeRan = false;
      const before = studio.d.querySelectorAll("img, iframe").length;
      assert(await studio.open(htmlFile(malicious + await cliLarge.text())) === "success", "HTML import failed");
      assert(!window.senderCodeRan && studio.d.querySelectorAll("img, iframe").length === before, "sender markup activated");
    });
    await test("Untrusted KDF parameters rejected before WASM work", async () => {
      const header = await packageOf(cliHTML);
      const page = await cliHTML.text();
      const compile = studio.w.WebAssembly.compile;
      let allocations = 0;
      const grow = studio.w.WebAssembly.Memory.prototype.grow;
      studio.w.WebAssembly.compile = () => { allocations++; throw new Error("unexpected compile"); };
      studio.w.WebAssembly.Memory.prototype.grow = () => { allocations++; throw new Error("unexpected allocation"); };
      try {
        for (const [key, value] of [["memory", 4194304], ["passes", 1000], ["lanes", 4], ["memory", -1], ["lanes", 0]]) {
          const modified = structuredClone(header);
          modified.unlock[key] = value;
          const html = page.replace(JSON.stringify(header), JSON.stringify(modified));
          assert(html !== page, "test header was not replaced");
          assert(await studio.open(htmlFile(html)) === "error", "HTML accepted " + key);
          const raw = new Uint8Array(await cliBlob.arrayBuffer());
          const length = new DataView(raw.buffer).getUint32(8);
          const blobHeader = JSON.parse(new TextDecoder().decode(raw.subarray(12, 12 + length)));
          blobHeader.unlock[key] = value;
          const encoded = text.encode(JSON.stringify(blobHeader));
          const prefix = raw.slice(0, 12);
          new DataView(prefix.buffer).setUint32(8, encoded.length);
          assert(await studio.open(new File([prefix, encoded, raw.subarray(12 + length)], "bad.tegami")) === "error", "blob accepted " + key);
        }
        assert(allocations === 0, "KDF ran before validation");
      } finally {
        studio.w.WebAssembly.compile = compile;
        studio.w.WebAssembly.Memory.prototype.grow = grow;
      }
    });
    await test("Commitment mismatch produces no output", async () => {
      const zero = btoa(String.fromCharCode(...new Uint8Array(32))).replace(/=+$/, "");
      const header = await packageOf(cliHTML);
      const html = (await cliHTML.text()).replace('"commit":"' + header.commit + '"', '"commit":"' + zero + '"');
      const before = studio.state.downloads.length;
      assert(await studio.open(htmlFile(html)) === "error", "HTML: bad commitment accepted");
      assert(studio.state.downloads.length === before, "commitment mismatch published output");
    });
    await test("Studio HTML opens through CLI", async () => {
      madeHTML = await studio.seal([new File([message], "報告書.txt")]);
      const data = await packageOf(madeHTML.blob);
      assert(data.schema === "tegami/v1" && data.chunk_size === 4194304 && atob(data.name.ciphertext).length === 196 && atob(data.commit).length === 32, "new format metadata");
      await cliCheck("html", madeHTML.blob);
      window.studioHTML = await madeHTML.blob.text();
    });
    await test("Studio HTML with two records opens through CLI", async () => {
      madeLarge = await studio.seal([new File([new Uint8Array(4194311).fill(42)], "footage.bin")]);
      assert(splitRecords(await madeLarge.blob.text()).records.length === 2, "expected two records");
      await cliCheck("large-html", madeLarge.blob);
    });
    await test("Studio streaming delivery opens through CLI", async () => {
      const saved = await studio.seal([new File([new Uint8Array(4194311).fill(42)], "footage.bin")], "blob");
      await cliCheck("blob", saved.blob);
    });
    await test("Studio ZIP packing opens through CLI", async () => {
      const saved = await studio.seal([new File([message], "one.txt"), new File([message], "two.txt")]);
      await cliCheck("zip", saved.blob);
    });
    await test("Filename length is hidden at both boundaries", async () => {
      for (const name of ["a", "文".repeat(60)]) {
        const saved = await studio.seal([new File([message], name)]);
        const data = await packageOf(saved.blob);
        assert(atob(data.name.ciphertext).length === 196, "variable name ciphertext length");
        assert(await studio.open(new File([saved.blob], "delivery.html")) === "success", "padded name did not open");
        assert(studio.state.downloads.at(-1).name === name, "name padding corrupted filename");
      }
    });
    await test("Studio HTML opens on its own with intact CSP and pristine markup", async () => {
      const standalone = await unlockStudioPage(madeHTML.blob);
      assert(standalone.d.body.dataset.mode === "envelope" && standalone.d.querySelector('input[name="format"][value="html"]').checked, "sender selection leaked into delivery");
      assert(standalone.d.getElementById("open-status").dataset.state === "success", standalone.d.getElementById("open-status").textContent);
      assert(await standalone.state.downloads.at(-1).blob.text() === message && standalone.state.violations.length === 0, "standalone output or CSP mismatch");
      standalone.frame.remove();
    });
    await test("Studio HTML with two records opens on its own", async () => {
      const standalone = await unlockStudioPage(madeLarge.blob);
      assert(standalone.d.getElementById("open-status").dataset.state === "success", standalone.d.getElementById("open-status").textContent);
      assert(await isFootage(standalone.state.downloads.at(-1).blob) && standalone.state.violations.length === 0, "standalone output or CSP mismatch");
      standalone.frame.remove();
    });
    await test("Studio HTML with a dropped record produces no output on its own", async () => {
      const standalone = await unlockStudioPage(damagedRecords(await madeLarge.blob.text()).dropped);
      assert(standalone.d.getElementById("open-status").dataset.state === "error" && standalone.state.downloads.length === 0, "dropped record accepted");
      standalone.frame.remove();
    });
    await test("CLI HTML opens on its own with intact CSP", async () => {
      cli = await unlockPage(await cliHTML.text());
      assert(cli.d.getElementById("status").dataset.state === "success", cli.d.getElementById("status").textContent);
      assert(await cli.state.downloads.at(-1).blob.text() === message && cli.state.violations.length === 0, "CLI page output or CSP mismatch");
    });
    await test("CLI HTML with two records opens on its own", async () => {
      const page = await unlockPage(await cliLarge.text());
      assert(page.d.getElementById("status").dataset.state === "success", page.d.getElementById("status").textContent);
      assert(page.state.downloads.at(-1).name === "footage.bin" && await isFootage(page.state.downloads.at(-1).blob) && page.state.violations.length === 0, "CLI page output or CSP mismatch");
      page.frame.remove();
    });
    await test("CLI page rejects damaged, reordered, or extra records", async () => {
      for (const [name, html] of Object.entries(damagedRecords(await cliLarge.text()))) {
        const page = await unlockPage(html);
        assert(page.d.getElementById("status").dataset.state === "error" && page.state.downloads.length === 0, name + " records accepted");
        page.frame.remove();
      }
    });
    await test("CLI page rejects untrusted parameters before allocating WASM", async () => {
      const original = await packageOf(cliHTML);
      const modified = structuredClone(original);
      modified.unlock.memory = 4194304;
      const html = (await cliHTML.text()).replace(JSON.stringify(original), JSON.stringify(modified));
      assert(html !== await cliHTML.text(), "test package was not replaced");
      const bad = await pageOf(html);
      let work = 0;
      bad.w.WebAssembly.compile = () => { work++; throw new Error("unexpected compile"); };
      bad.w.WebAssembly.Memory.prototype.grow = () => { work++; throw new Error("unexpected allocation"); };
      bad.d.getElementById("password").value = password;
      bad.d.getElementById("open-form").requestSubmit();
      await wait(() => bad.d.getElementById("status").dataset.state);
      assert(bad.d.getElementById("status").dataset.state === "error" && work === 0 && bad.state.downloads.length === 0, "unbounded KDF work or output");
      bad.frame.remove();
    });
    await test("HTML import remains available without streaming API", async () => {
      // Disable the API before parsing the page while preserving its hashed
      // executable script. The small setup script needs its own CSP hash.
      const html = await (await fetch("/fixtures/tegami.html")).text();
      const setup = "window.showSaveFilePicker=undefined;";
      const hash = btoa(String.fromCharCode(...new Uint8Array(await crypto.subtle.digest("SHA-256", text.encode(setup)))));
      const source = html.replace("script-src ", "script-src 'sha256-" + hash + "' ").replace("<head>", "<head><script>" + setup + "<\/script>");
      const noStream = await pageOf(source);
      assert(!noStream.d.getElementById("open-file").disabled && noStream.d.getElementById("format-blob").disabled, "HTML disabled with streaming API absent");
      assert(await noStream.open(cliHTML) === "success", "non-streaming HTML import failed");
      assert(await noStream.open(cliLarge) === "success" && await isFootage(noStream.state.downloads.at(-1).blob), "non-streaming large HTML import failed");
      noStream.frame.remove();
    });
    assert(studio.state.violations.length === 0, "Studio CSP violation");
  } catch (error) {
    lines.push("FAIL setup: " + error.stack);
    window.failures++;
  }
  window.done = true;
  results.textContent = lines.join("\n") + "\n\n" + (window.failures ? window.failures + " checks failed." : "All checks passed.");
  // In headless runs, browsercheck -chromium waits for this report and exits
  // with its result. A manual run has no listener, so a failure is ignored.
  fetch("/done", {method: "POST", body: JSON.stringify({failures: window.failures, report: results.textContent})}).catch(() => {});
})();
