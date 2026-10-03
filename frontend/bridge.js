(() => {
  const MODULES = [
    { id: "cpu" },
    { id: "mem" },
    { id: "net" },
    { id: "disk" },
  ];
  const SMOOTH_KEYS = new Set(["cpu", "mem", "disk", "down", "up", "diskRead", "diskWrite"]);

  let api = null;
  let runtime = null;
  let settings = null;
  let skins = [];
  let skinDir = "";
  let configDir = "";
  let latest = null;
  let announced = false;
  let lastW = 0;
  let lastH = 0;
  const history = { down: [], up: [] };
  const shownHist = { down: [], up: [] };
  let saveTimer = 0;
  const pending = {};
  let i18n = {};
  let locales = [];
  let localeDir = "";
  const shownNum = {};
  let painting = false;

  let trayOn = false;

  function t(key) {
    const v = i18n[key];
    return v == null || v === "" ? key : v;
  }

  const $ = (sel, root = document) => root.querySelector(sel);
  const $$ = (sel, root = document) => [...root.querySelectorAll(sel)];

  window.NetPulsar = {
    get latest() { return latest; },
    get settings() { return settings; },
    history,
  };

  function waitApi(timeoutMs) {
    return new Promise((resolve, reject) => {
      const start = Date.now();
      const tick = () => {
        const app = window.go && window.go.main && window.go.main.App;
        if (app && window.runtime) {
          resolve({ app, runtime: window.runtime });
          return;
        }
        if (Date.now() - start > timeoutMs) {
          reject(new Error("runtime is not ready"));
          return;
        }
        setTimeout(tick, 30);
      };
      tick();
    });
  }

  function esc(s) {
    return String(s ?? "").replace(/[&<>"']/g, (c) => ({
      "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;",
    }[c]));
  }

  function plain(n) {
    n = Number(n) || 0;
    if (n >= 10 || n < 0.05) return String(Math.round(n));
    return n.toFixed(1);
  }

  function pct(n) {
    return plain(n) + "%";
  }

  function scaled(n, units, suffix) {
    n = Number(n) || 0;
    let i = 0;
    while (n >= 1024 && i < units.length - 1) {
      n /= 1024;
      i++;
    }
    const text = n >= 100 || i === 0 ? n.toFixed(0) : n.toFixed(1);
    return text + " " + units[i] + (suffix || "");
  }

  function rate(n) { return scaled(n, ["B", "KB", "MB", "GB"], "/s"); }
  function bytes(n) { return scaled(n, ["B", "KB", "MB", "GB", "TB"], ""); }

  function uptime(sec) {
    sec = Math.max(0, Number(sec) || 0);
    const d = Math.floor(sec / 86400);
    const h = Math.floor((sec % 86400) / 3600);
    const m = Math.floor((sec % 3600) / 60);
    if (d > 0) return d + t("unit.day") + " " + h + t("unit.hour");
    if (h > 0) return h + t("unit.hour") + " " + m + t("unit.minute");
    return Math.max(m, 0) + t("unit.minute");
  }

  function bar(n) {
    const p = Math.max(0, Math.min(100, Number(n) || 0));
    const filled = Math.round(p / 10);
    return "█".repeat(filled) + "░".repeat(10 - filled) + " " + Math.round(p) + "%";
  }

  function textFor(key, format, value) {
    if (format === "bar") return bar(value);
    if (format === "pct") return pct(value);
    if (format === "uptime" || key === "uptime") return uptime(value);
    if (format === "bytes" || /Used$|Total$/.test(key)) return bytes(value);
    if (format === "rate" || key === "down" || key === "up" || key === "diskRead" || key === "diskWrite") return rate(value);
    if (key === "host") return value || "";
    return plain(value);
  }

  function valueOf(snap, key) {
    return snap ? snap[key] : 0;
  }

  function eased(id, target, allow) {
    if (!allow || !settings || settings.smooth === false) {
      shownNum[id] = target;
      return target;
    }
    const cur = shownNum[id];
    if (cur == null || Number.isNaN(cur)) {
      shownNum[id] = target;
      return target;
    }
    const diff = target - cur;
    if (Math.abs(diff) < 0.05) {
      shownNum[id] = target;
      return target;
    }
    const next = cur + diff * 0.18;
    shownNum[id] = next;
    return next;
  }

  function paintNumbers() {
    painting = false;
    if (!latest) return;
    let moving = false;
    const smooth = !settings || settings.smooth !== false;
    $$("[data-field]").forEach((el) => {
      const key = el.dataset.field;
      const raw = valueOf(latest, key);
      if (key === "host") {
        el.textContent = raw || "";
        return;
      }
      el.textContent = textFor(key, el.dataset.format || "", Number(raw) || 0);
    });
    $$("[data-ring], [data-bar]").forEach((el) => {
      const key = el.dataset.ring || el.dataset.bar;
      const target = Math.max(0, Math.min(100, Number(valueOf(latest, key)) || 0));
      const v = eased(key + ":meter", target, SMOOTH_KEYS.has(key));
      if (smooth && SMOOTH_KEYS.has(key) && Math.abs(v - target) > 0.05) moving = true;
      el.style.setProperty("--p", v.toFixed(1));
      if (el.dataset.bar) el.style.width = v + "%";
    });
    const cores = latest.cores || [];
    $$("[data-cores]").forEach((host) => {
      [...host.children].forEach((el, i) => {
        const target = Math.max(0, Math.min(100, Number(cores[i]) || 0));
        const v = eased("core:" + i, target, true);
        if (smooth && Math.abs(v - target) > 0.05) moving = true;
        el.style.setProperty("--p", v.toFixed(1));
      });
    });
    if (easeSeries("down") || easeSeries("up")) moving = true;
    drawSparks();
    if (moving) {
      painting = true;
      requestAnimationFrame(paintNumbers);
    }
  }

  function kickPaint() {
    if (painting) return;
    painting = true;
    requestAnimationFrame(paintNumbers);
  }

  function apply(snap) {
    if (!snap) return;
    latest = snap;
    window.NetPulsar.latest = snap;
    renderCores(snap.cores || []);
    pushHistory(snap);
    renderProcs();
    kickPaint();
    document.dispatchEvent(new CustomEvent("netpulsar:metrics", { detail: snap }));
  }

  function renderCores(list) {
    $$("[data-cores]").forEach((host) => {
      if (host.childElementCount !== list.length) {
        host.innerHTML = list.map(() => "<i></i>").join("");
      }
    });
  }

  function pushHistory(snap) {
    const smooth = !settings || settings.smooth !== false;
    ["down", "up"].forEach((key) => {
      const v = Number(snap[key]) || 0;
      history[key].push(v);
      const shown = shownHist[key];
      const seed = shown.length ? shown[shown.length - 1] : v;
      shown.push(smooth ? seed : v);
      if (history[key].length > 36) {
        history[key].shift();
        shown.shift();
      }
    });
  }

  function easeSeries(key) {
    const target = history[key];
    const shown = shownHist[key];
    const smooth = !settings || settings.smooth !== false;
    if (!smooth) {
      shown.splice(0, shown.length, ...target);
      return false;
    }
    let moving = false;
    const n = Math.min(target.length, shown.length);
    for (let i = 0; i < n; i++) {
      const diff = target[i] - shown[i];
      const tol = Math.max(80, Math.abs(target[i]) * 0.02);
      if (Math.abs(diff) <= tol) {
        shown[i] = target[i];
      } else {
        shown[i] += diff * 0.22;
        moving = true;
      }
    }
    return moving;
  }

  function drawSparks() {
    $$("canvas[data-spark]").forEach((canvas) => {
      const down = shownHist.down;
      const up = shownHist.up;
      const dpr = window.devicePixelRatio || 1;
      const w = canvas.clientWidth || 120;
      const h = canvas.clientHeight || 32;
      const pw = Math.max(1, Math.floor(w * dpr));
      const ph = Math.max(1, Math.floor(h * dpr));
      if (canvas.width !== pw || canvas.height !== ph) {
        canvas.width = pw;
        canvas.height = ph;
      }
      const ctx = canvas.getContext("2d");
      ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
      ctx.clearRect(0, 0, w, h);
      stroke(ctx, down, w, h, "rgba(126, 196, 255, .95)", true);
      stroke(ctx, up, w, h, "rgba(196, 160, 255, .8)", false);
    });
  }

  function stroke(ctx, series, w, h, color, fill) {
    if (!series.length) return;
    const max = Math.max(...series, 1);
    ctx.beginPath();
    series.forEach((v, i) => {
      const x = series.length === 1 ? w : (i / (series.length - 1)) * w;
      const y = h - (v / max) * (h - 2) - 1;
      if (i === 0) ctx.moveTo(x, y);
      else ctx.lineTo(x, y);
    });
    ctx.strokeStyle = color;
    ctx.lineWidth = 1.4;
    ctx.stroke();
    if (!fill) return;
    ctx.lineTo(w, h);
    ctx.lineTo(0, h);
    ctx.closePath();
    ctx.fillStyle = "rgba(126, 196, 255, .16)";
    ctx.fill();
  }

  function renderProcs() {
    const sort = (settings && settings.processSort) || "cpu";
    const limit = (settings && settings.processLimit) || 8;
    $$("[data-sort]").forEach((btn) => btn.classList.toggle("is-on", btn.dataset.sort === sort));
    const metric = (p) => {
      if (sort === "net") return (Number(p.down) || 0) + (Number(p.up) || 0);
      if (sort === "mem") return Number(p.mem) || 0;
      return Number(p.cpu) || 0;
    };
    const rows = [...((latest && latest.procs) || [])].sort((a, b) => metric(b) - metric(a) || (b.mem || 0) - (a.mem || 0)).slice(0, limit);
    const max = rows.reduce((m, p) => Math.max(m, metric(p)), 0);
    $$("[data-procs]").forEach((host) => {
      if (!rows.length) {
        host.innerHTML = '<div class="proc-empty">' + esc(t("procs.sampling")) + "</div>";
        return;
      }
      host.innerHTML = rows.map((p) => {
        const v = metric(p);
        const width = max > 0 ? Math.min(100, (v / max) * 100) : 0;
        const label = sort === "net" ? rate(v) : pct(v);
        const action = (settings && settings.procAction) || "off";
        const cls = action === "off" ? "proc" : "proc is-action";
        return `<div class="${cls}" data-pid="${p.pid}" title="${esc(p.name)}"><span class="proc-name">${esc(p.name)}</span><span class="proc-track"><span class="proc-fill" style="width:${width.toFixed(1)}%"></span></span><span class="proc-val">${label}</span></div>`;
      }).join("");
    });
  }

  function applyChrome() {
    if (!settings) return;
    orderSorts();
    const skin = $("#skin");
    if (skin) skin.style.opacity = String(settings.opacity ?? 1);
    const enabled = new Set(settings.modules || []);
    $$("[data-module]").forEach((el) => {
      const id = el.dataset.module;
      el.hidden = !enabled.has(id);
      const idx = (settings.modules || []).indexOf(id);
      el.style.order = idx < 0 ? "99" : String(idx);
    });
    $$("[data-action='pin']").forEach((btn) => btn.classList.toggle("is-on", !!settings.alwaysOnTop));
    $$("[data-host]").forEach((el) => { el.hidden = settings.showHost === false; });
    applyScale();
    applyGlow();
    renderProcs();
    kickPaint();
  }

  function uiScale() {
    let n = Number(settings && settings.uiScale);
    if (!n) n = 1;
    if (n < 0.8) n = 0.8;
    if (n > 1.5) n = 1.5;
    return n;
  }

  function applyScale() {
    const app = $("#app");
    if (!app) return;
    const next = String(uiScale());
    if (app.style.zoom === next) return;
    app.style.zoom = next;
    measure();
  }

  function applyGlow() {
    document.documentElement.classList.toggle("no-glow", !!(settings && settings.glow === false));
  }

  function moduleRows() {
    const on = new Set(settings.modules || []);
    const rows = (settings.modules || []).map((id) => ({ id, on: true }));
    MODULES.forEach((m) => {
      if (!on.has(m.id)) rows.push({ id: m.id, on: false });
    });
    return rows;
  }

  function moduleName(id) {
    return t("module." + id);
  }

  const RANK_IDS = ["cpu", "mem", "net"];

  function rankOrder() {
    const out = [];
    const want = (settings && settings.rankOrder) || [];
    want.forEach((id) => {
      if (RANK_IDS.includes(id) && !out.includes(id)) out.push(id);
    });
    RANK_IDS.forEach((id) => {
      if (!out.includes(id)) out.push(id);
    });
    return out;
  }

  function orderSorts() {
    const order = rankOrder();
    $$(".sorts").forEach((host) => {
      const found = {};
      host.querySelectorAll("[data-sort]").forEach((btn) => { found[btn.dataset.sort] = btn; });
      order.forEach((id) => {
        if (found[id]) host.appendChild(found[id]);
      });
    });
  }

  function rankName(id) {
    return t("sort." + id);
  }

  function applyI18n(root) {
    $$("[data-i18n]", root).forEach((el) => {
      if (el.children.length) return;
      el.textContent = t(el.dataset.i18n);
    });
    $$("[data-i18n-title]", root).forEach((el) => {
      el.title = t(el.dataset.i18nTitle);
    });
  }

  function renderSettings() {
    const root = $("#settings");
    const prev = $(".set-body", root);
    const scroll = prev ? prev.scrollTop : 0;
    const rows = moduleRows();
    const action = settings.procAction || "off";
    root.innerHTML = `
      <div class="set-head" style="--wails-draggable:drag">
        <b>${esc(t("settings.title"))}</b>
        <button type="button" data-action="close-settings" title="${esc(t("action.close"))}">×</button>
      </div>
      <div class="set-body">
      <div class="set-block">
        <div class="set-label"><span>${esc(t("settings.language"))}</span></div>
        <div class="skin-grid">
          ${locales.map((l) => `
            <button type="button" class="skin-card ${l.id === settings.locale ? "on" : ""}" data-locale="${esc(l.id)}">
              <div><b>${esc(l.name || l.id)}</b><small>${esc(l.id)}</small></div>
            </button>`).join("")}
        </div>
      </div>
      <div class="set-block">
        <div class="set-label"><span>${esc(t("settings.skin"))}</span></div>
        <div class="skin-grid">
          ${skins.map((s) => `
            <button type="button" class="skin-card ${s.id === settings.skin ? "on" : ""}" data-skin="${esc(s.id)}" style="--accent:${esc(s.accent || "#8eb6ff")}">
              <i></i>
              <div><b>${esc(s.name || s.id)}</b><small>${esc(s.description || "")}</small></div>
            </button>`).join("")}
        </div>
      </div>
      <div class="set-block">
        <div class="set-label"><span>${esc(t("settings.modules"))}</span><span>${esc(t("settings.modulesHint"))}</span></div>
        <div id="mod-list">
          ${rows.map((row, i) => `
            <div class="mod-row">
              <label><input type="checkbox" data-mod="${row.id}" ${row.on ? "checked" : ""} /> ${esc(moduleName(row.id))}</label>
              <div class="ops">
                <button type="button" data-move="${i}" data-dir="-1" ${i === 0 ? "disabled" : ""}>↑</button>
                <button type="button" data-move="${i}" data-dir="1" ${i === rows.length - 1 ? "disabled" : ""}>↓</button>
              </div>
            </div>`).join("")}
        </div>
      </div>
      <div class="set-block">
        <div class="set-label"><span>${esc(t("settings.rankOrder"))}</span><span>${esc(t("settings.rankOrderHint"))}</span></div>
        <div id="rank-list">
          ${rankOrder().map((id, i, arr) => `
            <div class="mod-row">
              <span>${esc(rankName(id))}</span>
              <div class="ops">
                <button type="button" data-rank="${i}" data-dir="-1" ${i === 0 ? "disabled" : ""}>↑</button>
                <button type="button" data-rank="${i}" data-dir="1" ${i === arr.length - 1 ? "disabled" : ""}>↓</button>
              </div>
            </div>`).join("")}
        </div>
      </div>
      <div class="set-block">
        <div class="set-label"><span>${esc(t("settings.refresh"))}</span><span id="lab-refresh"></span></div>
        <input id="refresh" type="range" min="250" max="3000" step="50" value="${settings.refreshMs}" />
      </div>
      <div class="set-block">
        <div class="set-label"><span>${esc(t("settings.opacity"))}</span><span id="lab-opacity"></span></div>
        <input id="opacity" type="range" min="35" max="100" step="1" value="${Math.round(settings.opacity * 100)}" />
      </div>
      <div class="set-block">
        <div class="switch-row"><span>${esc(t("settings.alwaysOnTop"))}<small>${esc(t("settings.alwaysOnTopHint"))}</small></span><button type="button" class="switch ${settings.alwaysOnTop ? "on" : ""}" data-toggle="alwaysOnTop"><i></i></button></div>
        <div class="switch-row"><span>${esc(t("settings.launchAtLogin"))}</span><button type="button" class="switch ${settings.launchAtLogin ? "on" : ""}" data-toggle="launchAtLogin"><i></i></button></div>
        <div class="switch-row"><span>${esc(t("settings.showHost"))}</span><button type="button" class="switch ${settings.showHost !== false ? "on" : ""}" data-toggle="showHost"><i></i></button></div>
        <div class="switch-row"><span>${esc(t("settings.smooth"))}<small>${esc(t("settings.smoothHint"))}</small></span><button type="button" class="switch ${settings.smooth !== false ? "on" : ""}" data-toggle="smooth"><i></i></button></div>
        <div class="switch-row"><span>${esc(t("settings.edgeHide"))}</span><button type="button" class="switch ${settings.edgeHide ? "on" : ""}" data-toggle="edgeHide"><i></i></button></div>
        <div class="switch-row"><span>${esc(t("settings.autoHide"))}</span><button type="button" class="switch ${settings.autoHide ? "on" : ""}" data-toggle="autoHide"><i></i></button></div>
      </div>
      <div class="set-block">
        <div class="set-label"><span>${esc(t("settings.peek"))}</span><span id="lab-peek"></span></div>
        <input id="peek" type="range" min="8" max="64" step="1" value="${settings.peekPx}" />
      </div>
      <div class="set-block">
        <div class="set-label"><span>${esc(t("settings.snap"))}</span><span id="lab-snap"></span></div>
        <input id="snap" type="range" min="8" max="96" step="1" value="${settings.snapPx}" />
      </div>
      <div class="set-block">
        <div class="set-label"><span>${esc(t("settings.hideWait"))}</span><span id="lab-hide"></span></div>
        <input id="hide" type="range" min="2" max="60" step="1" value="${settings.autoHideSec}" />
      </div>
      <div class="set-block">
        <div class="set-label"><span>${esc(t("settings.limit"))}</span><span id="lab-limit"></span></div>
        <input id="limit" type="range" min="3" max="16" step="1" value="${settings.processLimit}" />
      </div>
      <div class="set-block">
        <div class="set-label"><span>${esc(t("settings.advanced"))}</span></div>
        <div class="set-label"><span>${esc(t("settings.scale"))}</span><span id="lab-scale"></span></div>
        <p class="hint">${esc(t("settings.scaleHint"))}</p>
        <input id="scale" type="range" min="80" max="150" step="10" value="${Math.round(uiScale() * 100)}" />
        <div class="switch-row"><span>${esc(t("settings.glow"))}</span><button type="button" class="switch ${settings.glow !== false ? "on" : ""}" data-toggle="glow"><i></i></button></div>
        <div class="set-label" style="margin-top:8px"><span>${esc(t("settings.procClick"))}</span></div>
        <div class="seg">
          <button type="button" data-proc-action="off" class="${action === "off" ? "on" : ""}">${esc(t("settings.procOff"))}</button>
          <button type="button" data-proc-action="folder" class="${action === "folder" ? "on" : ""}">${esc(t("settings.procFolder"))}</button>
          <button type="button" data-proc-action="copy" class="${action === "copy" ? "on" : ""}">${esc(t("settings.procCopy"))}</button>
        </div>
      </div>
      <div class="set-actions">
        <button type="button" class="ghost" id="open-skins">${esc(t("settings.openSkins"))}</button>
        <button type="button" class="ghost" id="open-locales">${esc(t("settings.openLocales"))}</button>
        <button type="button" class="ghost" id="reload-skins">${esc(t("settings.reload"))}</button>
        <button type="button" class="ghost" id="restore-skin">${esc(t("settings.restore"))}</button>
        <button type="button" class="ghost" id="reset-place">${esc(t("settings.reset"))}</button>
      </div>
      <div class="set-path">${esc(skinDir)}<br>${esc(localeDir)}</div>
      <div class="set-block" style="margin-top:10px">
        <details>
          <summary>${esc(t("settings.helpSkin"))}</summary>
          <p>${esc(t("settings.helpSkinBody"))}</p>
        </details>
        <details>
          <summary>${esc(t("settings.helpLang"))}</summary>
          <p>${esc(t("settings.helpLangBody"))}</p>
        </details>
      </div>
      </div>`;
    paintLabels();
    bindSettings(root);
    const body = $(".set-body", root);
    if (body) body.scrollTop = scroll;
  }

  function paintLabels() {
    const set = (id, text) => { const el = $("#" + id); if (el) el.textContent = text; };
    set("lab-refresh", (settings.refreshMs / 1000).toFixed(1) + " " + t("unit.second"));
    set("lab-opacity", Math.round(settings.opacity * 100) + "%");
    set("lab-peek", settings.peekPx + " px");
    set("lab-snap", settings.snapPx + " px");
    set("lab-hide", settings.autoHideSec + " " + t("unit.second"));
    set("lab-limit", String(settings.processLimit));
    set("lab-scale", Math.round(uiScale() * 100) + "%");
  }

  function queue(partial) {
    Object.assign(pending, partial);
    Object.assign(settings, partial);
    applyChrome();
    paintLabels();
    clearTimeout(saveTimer);
    saveTimer = setTimeout(flush, 80);
  }

  async function flush() {
    const next = { ...settings, ...pending };
    for (const k of Object.keys(pending)) delete pending[k];
    try {
      settings = await api.SaveSettings(next);
      applyChrome();
      paintLabels();
    } catch (err) {
      console.error(err);
    }
  }

  function bindSettings(root) {
    root.querySelectorAll("[data-locale]").forEach((btn) => {
      btn.addEventListener("click", async () => {
        if (btn.dataset.locale === settings.locale) return;
        clearTimeout(saveTimer);
        settings = await api.SaveSettings({ ...settings, locale: btn.dataset.locale });
        location.reload();
      });
    });
    root.querySelectorAll("[data-skin]").forEach((btn) => {
      btn.addEventListener("click", async () => {
        clearTimeout(saveTimer);
        settings = await api.SaveSettings({ ...settings, skin: btn.dataset.skin });
        location.reload();
      });
    });
    root.querySelectorAll("[data-mod]").forEach((box) => {
      box.addEventListener("change", () => {
        const visual = moduleRows().map((row) => row.id === box.dataset.mod ? { id: row.id, on: box.checked } : row);
        const enabled = visual.filter((r) => r.on).map((r) => r.id);
        if (!enabled.length) {
          box.checked = true;
          return;
        }
        queue({ modules: enabled });
        renderSettings();
      });
    });
    root.querySelectorAll("[data-move]").forEach((btn) => {
      btn.addEventListener("click", () => {
        const rows = moduleRows();
        const i = Number(btn.dataset.move);
        const j = i + Number(btn.dataset.dir);
        if (j < 0 || j >= rows.length) return;
        [rows[i], rows[j]] = [rows[j], rows[i]];
        queue({ modules: rows.filter((r) => r.on).map((r) => r.id) });
        renderSettings();
      });
    });
    root.querySelectorAll("[data-rank]").forEach((btn) => {
      btn.addEventListener("click", () => {
        const rows = rankOrder();
        const i = Number(btn.dataset.rank);
        const j = i + Number(btn.dataset.dir);
        if (j < 0 || j >= rows.length) return;
        [rows[i], rows[j]] = [rows[j], rows[i]];
        queue({ rankOrder: rows });
        renderSettings();
      });
    });
    bindRange("refresh", "refreshMs", (v) => v);
    bindRange("opacity", "opacity", (v) => v / 100);
    bindRange("peek", "peekPx", (v) => v);
    bindRange("snap", "snapPx", (v) => v);
    bindRange("hide", "autoHideSec", (v) => v);
    bindRange("limit", "processLimit", (v) => v);
    bindRange("scale", "uiScale", (v) => v / 100);
    root.querySelectorAll("[data-proc-action]").forEach((btn) => {
      btn.addEventListener("click", () => {
        queue({ procAction: btn.dataset.procAction });
        root.querySelectorAll("[data-proc-action]").forEach((other) => {
          other.classList.toggle("on", other.dataset.procAction === settings.procAction);
        });
      });
    });
    root.querySelectorAll("[data-toggle]").forEach((btn) => {
      btn.addEventListener("click", () => {
        const key = btn.dataset.toggle;
        queue({ [key]: !settings[key] });
        btn.classList.toggle("on", !!settings[key]);
      });
    });
    $("#open-skins").addEventListener("click", () => api.OpenSkinsDir());
    $("#open-locales").addEventListener("click", () => api.OpenLocalesDir());
    $("#reload-skins").addEventListener("click", async () => {
      const boot = await api.Bootstrap();
      skins = boot.skins || [];
      skinDir = boot.skinDir || skinDir;
      i18n = boot.strings || i18n;
      locales = boot.locales || locales;
      localeDir = boot.localeDir || localeDir;
      applyI18n(document);
      renderSettings();
    });
    $("#restore-skin").addEventListener("click", async () => {
      try {
        await api.RestoreSkin(settings.skin);
        location.reload();
      } catch (err) {
        const path = $(".set-path");
        if (path) path.textContent = t("settings.restoreOnly");
      }
    });
    $("#reset-place").addEventListener("click", async () => {
      settings = await api.ResetPlacement();
      applyChrome();
      renderSettings();
    });
  }

  function bindRange(id, key, map) {
    const el = $("#" + id);
    if (!el) return;
    el.addEventListener("input", () => queue({ [key]: map(Number(el.value)) }));
  }

  async function openSettings() {
    const boot = await api.Bootstrap();
    settings = boot.settings;
    i18n = boot.strings || i18n;
    locales = boot.locales || locales;
    localeDir = boot.localeDir || localeDir;
    skins = boot.skins || skins;
    skinDir = boot.skinDir || skinDir;
    configDir = boot.configDir || configDir;
    renderSettings();
    $("#skin").hidden = true;
    $("#settings").hidden = false;
    await api.SetInteracting(true);
    measure();
  }

  async function closeSettings() {
    $("#settings").hidden = true;
    $("#skin").hidden = false;
    await api.SetInteracting(false);
    measure();
  }

  let measureQueued = false;
  function measure() {
    if (measureQueued) return;
    measureQueued = true;
    requestAnimationFrame(() => {
      measureQueued = false;
      const rect = $("#app").getBoundingClientRect();
      const w = Math.ceil(rect.width);
      const h = Math.ceil(rect.height);
      if (w < 40 || h < 40) return;
      if (Math.abs(w - lastW) < 2 && Math.abs(h - lastH) < 2) {
        if (!announced) {
          announced = true;
          api.Ready();
        }
        return;
      }
      lastW = w;
      lastH = h;
      api.Resize(w, h).then(() => {
        if (!announced) {
          announced = true;
          api.Ready();
        }
      });
    });
  }

  document.addEventListener("click", (e) => {
    const action = e.target.closest("[data-action]");
    if (action) {
      const name = action.dataset.action;
      if (name === "settings") {
        if ($("#settings").hidden) openSettings();
        else closeSettings();
      }
      if (name === "close-settings") closeSettings();
      if (name === "close") api.Quit();
      if (name === "pin") queue({ alwaysOnTop: !settings.alwaysOnTop });
      return;
    }
    const proc = e.target.closest(".proc[data-pid]");
    if (proc && settings && settings.procAction && settings.procAction !== "off") {
      onProcess(Number(proc.dataset.pid));
      return;
    }
    const sort = e.target.closest("[data-sort]");
    if (sort && settings) {
      queue({ processSort: sort.dataset.sort });
    }
  });

  let toastTimer = 0;
  function toast(text) {
    const el = $("#toast");
    if (!el) return;
    el.textContent = text;
    el.hidden = false;
    clearTimeout(toastTimer);
    toastTimer = setTimeout(() => { el.hidden = true; }, 1400);
  }

  async function onProcess(pid) {
    const rows = (latest && latest.procs) || [];
    const row = rows.find((p) => Number(p.pid) === pid);
    if (!row) return;
    if (settings.procAction === "copy") {
      let path = "";
      try { path = await api.ProcessPath(pid); } catch (err) { path = ""; }
      const lines = [
        row.name || "",
        "PID " + pid,
        "CPU " + pct(row.cpu),
        t("module.mem") + " " + pct(row.mem),
        "↓ " + rate(row.down) + "   ↑ " + rate(row.up),
      ];
      if (path) lines.push(path);
      try {
        await api.CopyText(lines.join("\n"));
        toast(t("proc.copied"));
      } catch (err) {
        toast(t("proc.noPath"));
      }
      return;
    }
    if (settings.procAction === "folder") {
      try {
        await api.OpenProcessFolder(pid);
      } catch (err) {
        toast(t("proc.noPath"));
      }
    }
  }

  document.addEventListener("keydown", (e) => {
    if (e.key === "Escape" && !$("#settings").hidden) closeSettings();
  });

  function applyTray() {
    if (!trayOn) return;
    $$("[data-action='close']").forEach((el) => { el.hidden = true; });
  }

  function ensureNetSort() {
    $$(".sorts").forEach((host) => {
      if (host.querySelector("[data-sort='net']")) return;
      const btn = document.createElement("button");
      btn.type = "button";
      btn.dataset.sort = "net";
      btn.textContent = t("sort.net");
      host.appendChild(btn);
    });
  }

  async function boot() {
    try {
      ({ app: api, runtime } = await waitApi(8000));
      const bootData = await api.Bootstrap();
      settings = bootData.settings;
      i18n = bootData.strings || {};
      locales = bootData.locales || [];
      localeDir = bootData.localeDir || "";
      skins = bootData.skins || [];
      skinDir = bootData.skinDir || "";
      configDir = bootData.configDir || "";
      const layout = await fetch("/skin/layout.html", { cache: "no-store" });
      if (!layout.ok) throw new Error("skin is missing layout.html");
      $("#skin").innerHTML = await layout.text();
      ensureNetSort();
      applyI18n($("#skin"));
      applyChrome();
      trayOn = !!bootData.tray;
      applyTray();
      runtime.EventsOn("open-settings", () => {
        api.Reveal();
        if ($("#settings").hidden) openSettings();
      });
      const script = await fetch("/skin/main.js", { cache: "no-store" });
      if (script.ok) {
        const el = document.createElement("script");
        el.textContent = await script.text();
        document.body.appendChild(el);
      }
      runtime.EventsOn("metrics", (snap) => apply(snap));
      apply(await api.GetSnapshot());
      const observer = new ResizeObserver(() => measure());
      observer.observe($("#app"));
      requestAnimationFrame(() => requestAnimationFrame(measure));
    } catch (err) {
      const box = document.createElement("div");
      box.id = "boot-error";
      box.textContent = t("boot.fail") + (err && err.message ? err.message : err);
      document.body.appendChild(box);
    }
  }

  boot();
})();
