// Data-source page helpers: query-param/header rows synced with the URL,
// template cards, the collapsible JSON tree, and response tabs.
(function () {
  // ---------- JSON tree (also used by the screen editor's field picker) ----------
  const SPECIAL = /[.*?#|@\\]/g;
  const esc = (k) => String(k).replace(SPECIAL, (c) => "\\" + c); // gjson path escaping
  const join = (p, k) => (p ? p + "." + k : k);
  const html = (s) => String(s).replace(/[&<>"]/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" }[c]));

  function leafHTML(v) {
    if (v === null) return '<span class="v t-null">null</span>';
    if (typeof v === "string") return `<span class="v t-str">"${html(v.length > 80 ? v.slice(0, 80) + "…" : v)}"</span>`;
    if (typeof v === "number") return `<span class="v t-num">${v}</span>`;
    return `<span class="v t-bool">${v}</span>`;
  }

  function build(key, v, path, depth, mode) {
    const isArr = Array.isArray(v);
    if (v !== null && typeof v === "object") {
      const keys = isArr ? v.map((_, i) => i) : Object.keys(v);
      const meta = isArr ? `[${keys.length}]` : `{${keys.length}}`;
      const d = document.createElement("details");
      if (depth < 2) d.open = true;
      const pick = path && mode === "insert" ? `<button type="button" class="path pick" data-path="${html(path)}" title="Use this ${isArr ? "list" : "object"}">⤶</button>` : "";
      d.innerHTML = `<summary>${key !== null ? `<span class="k">${html(key)}</span>` : ""}<span class="meta">${meta}</span>${pick}</summary>`;
      const box = document.createElement("div");
      box.className = "kids";
      const limit = 50;
      keys.slice(0, limit).forEach((k) => box.appendChild(build(k, v[k], join(path, isArr ? k : esc(k)), depth + 1, mode)));
      if (keys.length > limit) {
        const more = document.createElement("div");
        more.className = "more";
        more.textContent = `… ${keys.length - limit} more`;
        box.appendChild(more);
      }
      d.appendChild(box);
      return d;
    }
    const row = document.createElement(mode === "insert" ? "button" : "div");
    row.className = mode === "insert" ? "leaf path" : "leaf copyable";
    if (mode === "insert") row.type = "button";
    row.dataset.path = path;
    row.title = mode === "insert" ? "Insert {{ " + path + " }}" : "Copy {{ " + path + " }}";
    row.innerHTML = `<span class="k">${html(key)}</span>${leafHTML(v)}`;
    return row;
  }

  function initTrees(root) {
    (root || document).querySelectorAll(".json-tree:not([data-ready])").forEach((el) => {
      el.dataset.ready = "1";
      let data;
      try { data = JSON.parse(el.dataset.json); } catch { el.textContent = "Not JSON"; return; }
      el.appendChild(build(null, data, "", 0, el.dataset.mode || "copy"));
    });
  }

  function toast(msg) {
    let t = document.getElementById("page-toast");
    if (!t) {
      t = document.createElement("div");
      t.id = "page-toast";
      document.body.appendChild(t);
    }
    t.textContent = msg;
    t.classList.add("show");
    clearTimeout(t._h);
    t._h = setTimeout(() => t.classList.remove("show"), 1600);
  }

  document.addEventListener("click", (e) => {
    if (e.target.closest("summary .pick")) e.preventDefault(); // pick the list without toggling it
    const copy = e.target.closest(".leaf.copyable");
    if (copy) {
      const tpl = "{{ " + copy.dataset.path + " }}";
      navigator.clipboard?.writeText(tpl).then(() => toast("Copied " + tpl), () => toast(tpl));
      return;
    }
    const tool = e.target.closest("[data-tree]");
    if (tool) {
      const scope = tool.closest(".resp, .explorer") || document;
      scope.querySelectorAll(".json-tree details").forEach((d) => (d.open = tool.dataset.tree === "expand"));
      return;
    }
    const tab = e.target.closest(".tab[data-tab]");
    if (tab) {
      const scope = tab.closest(".resp");
      scope.querySelectorAll(".tab").forEach((t) => t.classList.toggle("on", t === tab));
      scope.querySelectorAll(".tab-pane").forEach((p) => (p.hidden = p.dataset.pane !== tab.dataset.tab));
      scope.querySelector(".tree-tools").hidden = tab.dataset.tab !== "tree";
      return;
    }
    const tog = e.target.closest(".resp-toggle");
    if (tog) {
      const resp = tog.closest(".resp");
      resp.classList.toggle("collapsed");
      try { localStorage.setItem("resp-collapsed", resp.classList.contains("collapsed") ? "1" : ""); } catch {}
    }
  });

  function restoreCollapse(root) {
    let c = "";
    try { c = localStorage.getItem("resp-collapsed") || ""; } catch {}
    (root || document).querySelectorAll(".resp").forEach((r) => r.classList.toggle("collapsed", c === "1"));
  }

  document.addEventListener("DOMContentLoaded", () => { initTrees(); restoreCollapse(); });
  document.addEventListener("htmx:afterSettle", (e) => { initTrees(e.target); restoreCollapse(e.target); });

  // ---------- Source form: params <-> URL, headers, presets ----------
  const form = document.getElementById("source-form");
  if (!form) return;
  const urlEl = form.querySelector("#url");
  const paramsEl = form.querySelector("#params");
  const headersEl = form.querySelector("#headers");
  const tpl = document.getElementById("kv-row-tpl");

  function newRow(kind, k = "", v = "", secret = false) {
    const row = tpl.content.firstElementChild.cloneNode(true);
    const [ki, vi] = row.querySelectorAll("input");
    ki.value = k;
    vi.value = v;
    if (kind === "headers") {
      row.querySelector(".kv-lock").remove();
      ki.name = "hk"; vi.name = "hv";
      ki.placeholder = "Header-Name";
    } else {
      setSecret(row, secret);
    }
    return row;
  }

  // Secret params are posted as sk/sv and kept out of the URL; normal ones live in the URL.
  function setSecret(row, on) {
    const [ki, vi] = row.querySelectorAll("input");
    row.classList.toggle("secret", on);
    row.querySelector(".kv-lock").classList.toggle("on", on);
    ki.name = on ? "sk" : "";
    vi.name = on ? "sv" : "";
    vi.type = on ? "password" : "text";
    if (on && !vi.value) vi.placeholder = "secret value";
  }

  function splitURL(u) {
    const i = u.indexOf("?");
    return i < 0 ? [u, ""] : [u.slice(0, i), u.slice(i + 1)];
  }
  const dec = (s) => { try { return decodeURIComponent(s.replace(/\+/g, " ")); } catch { return s; } };
  const enc = (s) => encodeURIComponent(s).replace(/%2C/gi, ",").replace(/%3A/gi, ":").replace(/%2F/gi, "/");

  // URL -> rows (non-secret rows are rebuilt; secret rows stay).
  function urlToRows() {
    const [, q] = splitURL(urlEl.value);
    paramsEl.querySelectorAll(".kv-row:not(.secret)").forEach((r) => r.remove());
    const first = paramsEl.querySelector(".kv-row.secret");
    q.split("&").filter(Boolean).forEach((pair) => {
      const [k, ...rest] = pair.split("=");
      paramsEl.insertBefore(newRow("params", dec(k), dec(rest.join("="))), first);
    });
  }

  // rows -> URL.
  function rowsToURL() {
    const [base] = splitURL(urlEl.value);
    const parts = [...paramsEl.querySelectorAll(".kv-row:not(.secret)")]
      .map((r) => r.querySelectorAll("input"))
      .filter(([k]) => k.value.trim())
      .map(([k, v]) => enc(k.value.trim()) + (v.value !== "" ? "=" + enc(v.value) : ""));
    urlEl.value = parts.length ? base + "?" + parts.join("&") : base;
  }

  urlEl.addEventListener("input", urlToRows);
  paramsEl.addEventListener("input", (e) => { if (!e.target.closest(".secret")) rowsToURL(); });

  form.addEventListener("click", (e) => {
    const add = e.target.closest(".kv-add");
    if (add) {
      const box = add.dataset.add === "headers" ? headersEl : paramsEl;
      const row = newRow(add.dataset.add);
      box.appendChild(row);
      row.querySelector("input").focus();
      return;
    }
    const del = e.target.closest(".kv-del");
    if (del) {
      const row = del.closest(".kv-row");
      const inParams = paramsEl.contains(row);
      row.remove();
      if (inParams) rowsToURL();
      return;
    }
    const lock = e.target.closest(".kv-lock");
    if (lock) {
      const row = lock.closest(".kv-row");
      setSecret(row, !row.classList.contains("secret"));
      if (row.classList.contains("secret")) paramsEl.appendChild(row);
      rowsToURL();
    }
  });

  // Body only matters for methods that send one.
  const method = form.querySelector("#method");
  const bodyField = form.querySelector("#body-field");
  const syncMethod = () => (bodyField.hidden = method.value === "GET");
  method.addEventListener("change", syncMethod);
  syncMethod();
  if (urlEl.value) urlToRows();

  // Template cards.
  document.querySelectorAll(".template-card").forEach((card) =>
    card.addEventListener("click", () => {
      const p = (window.PRESETS || [])[card.dataset.preset];
      if (!p) return;
      document.querySelectorAll(".template-card").forEach((c) => c.classList.toggle("on", c === card));
      const f = form.elements;
      f.namedItem("kind").value = "http";
      form.dispatchEvent(new Event("change", { bubbles: true }));
      if (!f.namedItem("name").value || form.dataset.fromPreset) f.namedItem("name").value = p.name === "Blank request" ? "" : p.name;
      form.dataset.fromPreset = "1";
      method.value = p.method || "GET";
      urlEl.value = p.url || "";
      f.namedItem("body").value = p.body || "";
      if (p.interval) f.namedItem("interval_sec").value = String(p.interval);
      paramsEl.innerHTML = "";
      headersEl.innerHTML = "";
      urlToRows();
      (p.params || []).filter((x) => x.secret).forEach((x) => paramsEl.appendChild(newRow("params", x.k, x.v, true)));
      (p.headers || []).forEach((h) => headersEl.appendChild(newRow("headers", h.k, h.v)));
      f.namedItem("auth").placeholder = p.auth_hint || "none";
      syncMethod();
      const needsValue = paramsEl.querySelector(".secret input[name=sv]");
      (needsValue || urlEl).focus();
      form.scrollIntoView({ behavior: "smooth", block: "nearest" });
    })
  );
})();
