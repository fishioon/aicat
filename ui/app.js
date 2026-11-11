import { host } from "./client.js";
import { shell, command, navigate, popup, problem, dialog } from "./shell.js";
import { t } from "./i18n.js";

import { appKey, renderSidebar } from "./sidebar.js";

const $ = (selector) => document.querySelector(selector);
const viewport = $("#rows-viewport");
const rowHeight = 30;
const overscan = 6;
const records = new Map();
const views = { request: "headers", response: "headers" };
const bodyCache = new Map();
const metrics = {
  batches: 0,
  rowsRendered: 0,
  renderMs: [],
  filterMs: [],
  deliveryMs: [],
  errors: [],
  maxRowsInDOM: 0,
};
let filtered = [];
let selected = 0;
let detail = null;
let generation = 0;

let rowFrame = 0;
let connected = false;

function remember(array, value) {
  array.push(value);
  if (array.length > 600) array.shift();
}

// The existing native matcher accepts case-insensitive fuzzy terms joined by
// AND, without reordering capture order. This page preserves that membership.
function matches(url, query) {
  const text = url.toLowerCase().normalize("NFD").replace(/\p{M}/gu, "");
  return query
    .toLowerCase()
    .normalize("NFD")
    .replace(/\p{M}/gu, "")
    .trim()
    .split(/\s+/u)
    .filter(Boolean)
    .every((term) => {
      let position = 0;
      for (const char of term) {
        const found = text.indexOf(char, position);
        if (found < 0) return false;
        position = found + char.length;
      }
      return true;
    });
}

function filter() {
  const started = performance.now();
  const query = $("#search").value;
  filtered = [...records.values()]
    .sort((a, b) => a.id - b.id)
    .filter(
      (row) =>
        (!shell.host || new URL(row.url).host === shell.host) &&
        (!shell.path ||
          new URL(row.url).pathname === shell.path ||
          new URL(row.url).pathname.startsWith(
            shell.path.replace(/\/$/, "") + "/",
          )) &&
        (shell.app === null || appKey(row) === shell.app) &&
        matches(row.url, query),
    );
  $("#count").textContent = `${filtered.length} ${t("个请求")}`;
  $("#list-empty").hidden = filtered.length > 0;
  $("#list-empty").textContent = t(
    records.size ? "没有匹配的请求" : "等待流量 · 请在客户端配置代理",
  );
  $("#rows-space").style.height = `${filtered.length * rowHeight}px`;
  if (selected && !filtered.some((row) => row.id === selected))
    clearSelection();
  remember(metrics.filterMs, performance.now() - started);
  scheduleRows();
}

function scheduleRows() {
  if (rowFrame) return;
  rowFrame = requestAnimationFrame(() => {
    rowFrame = 0;
    renderRows();
  });
}

function renderRows() {
  const started = performance.now();
  const first = Math.max(
    0,
    Math.floor(viewport.scrollTop / rowHeight) - overscan,
  );
  const last = Math.min(
    filtered.length,
    Math.ceil((viewport.scrollTop + viewport.clientHeight) / rowHeight) +
      overscan,
  );
  const fragment = document.createDocumentFragment();
  for (let index = first; index < last; index++) {
    const record = filtered[index];
    const row = document.createElement("div");
    row.className = "columns traffic-row";
    row.dataset.id = String(record.id);
    row.dataset.stripe = String(index % 2 === 1);
    row.setAttribute("role", "row");
    row.setAttribute("aria-selected", String(record.id === selected));
    const size =
      record.responseBytes < 1024
        ? `${record.responseBytes} B`
        : `${(record.responseBytes / 1024).toFixed(1)} KiB`;
    const values = [
      record.id,
      record.url,
      record.method,
      record.statusCode || "—",
      `${(record.durationNs / 1e6).toFixed(0)} ms`,
      size,
    ];
    const classes = ["id", "url", "method", "status", "duration", "size"];
    values.forEach((value, column) => {
      const cell = document.createElement("span");
      cell.className = classes[column];
      cell.textContent = value;
      if (column === 1) {
        const url = new URL(record.url);
        if (
          !url.username &&
          !url.password &&
          record.url.startsWith(url.protocol + "//" + url.host)
        ) {
          cell.replaceChildren();
          for (const [className, text] of [
            ["ac-url-scheme", url.protocol + "//"],
            ["ac-host", url.host],
            [
              "ac-url-path",
              record.url.slice((url.protocol + "//" + url.host).length),
            ],
          ]) {
            const span = document.createElement("span");
            span.className = className;
            span.textContent = text;
            cell.append(span);
          }
        }
      }
      row.append(cell);
    });
    fragment.append(row);
  }
  $("#rows").style.transform = `translateY(${first * rowHeight}px)`;
  $("#rows").replaceChildren(fragment);
  metrics.rowsRendered++;
  metrics.maxRowsInDOM = Math.max(metrics.maxRowsInDOM, last - first);
  remember(metrics.renderMs, performance.now() - started);
}

function renderDomains() {
  renderSidebar(records);
}

function clearSelection() {
  selected = 0;
  shell.selected = 0;
  detail = null;
  generation++;
  bodyCache.clear();
  for (const panel of document.querySelectorAll(".inspector")) {
    panel.querySelector(".raw").replaceChildren();
    delete panel.querySelector(".raw").dataset.text;
    panel.querySelector(".headers").replaceChildren();
    panel.querySelector(".body-notice").textContent = "";
  }
  $("#detail-empty").hidden = false;
  $("#detail-content").hidden = true;
  $("#selection").textContent = t("已选择 0 项");
  scheduleRows();
}

async function select(id) {
  const version = ++generation;
  selected = id;
  shell.selected = id;
  detail = null;
  bodyCache.clear();
  $("#detail-empty").hidden = true;
  $("#detail-content").hidden = false;
  $("#detail-title").textContent = `#${id} · ${t("正在读取…")}`;
  for (const panel of document.querySelectorAll(".inspector")) {
    panel.querySelector(".headers").replaceChildren();
    panel.querySelector(".raw").textContent = "";
    panel.querySelector(".body-notice").textContent = "";
  }
  $("#selection").textContent = t("已选择 1 项");
  scheduleRows();
  try {
    const record = await host.record(id);
    if (version !== generation || !records.has(id)) return;
    detail = record;
    renderSummary(record);
    await Promise.all(
      ["request", "response"].map((side) => renderDetail(side, version)),
    );
  } catch (error) {
    if (version === generation) $("#detail-title").textContent = error.message;
  }
}

function headers(message, side) {
  const h = { ...message.headers };
  if (
    side === "request" &&
    !Object.keys(h).some((k) => k.toLowerCase() === "host")
  )
    h.Host = [new URL(detail.url).host];
  return h;
}
function headerText(message, side) {
  return Object.entries(headers(message, side))
    .flatMap(([name, values]) => values.map((value) => `${name}: ${value}`))
    .join("\n");
}

async function renderDetail(side, version = generation) {
  if (!detail) return;
  const record = detail;
  const panel = $(`[data-side="${side}"]`);
  const raw = views[side] === "raw";
  panel.querySelector("[data-side-copy]").disabled =
    !!record.tunnel && side === "response";
  panel.querySelector(".raw").hidden = !raw;
  panel.querySelector(".headers").hidden = raw;
  for (const tab of panel.querySelectorAll(".tab"))
    tab.classList.toggle("active", tab.dataset.view === views[side]);
  const message =
    side === "request" ? record.requestReceived : record.responseSent;
  if (record.tunnel && side === "response") {
    panel.querySelector(".headers").textContent = t(
      "CONNECT 加密隧道 · 内容未解密",
    );
    panel.querySelector(".raw").textContent = t(
      "CONNECT 加密隧道 · 内容未解密",
    );
    const enable = document.createElement("button");
    enable.textContent = t("启用此域名");
    enable.id = "enable-decryption";
    enable.onclick = () => {
      const hostName = new URL(record.url).hostname;
      const p = document.createElement("p");
      p.textContent = hostName;
      dialog("HTTPS 解密", p, [
        ["取消", () => $("#dialog").close()],
        [
          "安装证书",
          async () => {
            if (await command("certificate-install")) {
              await command("domain", { host: hostName, enabled: true });
              $("#dialog").close();
            }
          },
        ],
        [
          "客户端已单独信任",
          async () => {
            if (await command("domain", { host: hostName, enabled: true }))
              $("#dialog").close();
          },
        ],
      ]);
    };
    if (side === "response")
      panel.querySelector(raw ? ".raw" : ".headers").append(enable);
    return;
  }
  if (!message) {
    panel.querySelector(".raw").textContent = "响应尚未到达";
    return;
  }
  const heading =
    side === "request"
      ? `${message.method} ${record.tunnel ? new URL(record.url).host : new URL(record.url).pathname + new URL(record.url).search} ${message.proto || "HTTP"}`
      : `${message.proto || "HTTP"} ${message.statusCode}`;
  if (!raw) {
    const table = document.createElement("table");
    table.className = "ac-header-table";
    const head = table.createTHead().insertRow();
    for (const label of ["名称", "值"]) {
      const cell = document.createElement("th");
      cell.textContent = t(label);
      head.append(cell);
    }
    head.firstChild.style.width = "35%";
    const tbody = table.createTBody();
    for (const [name, values] of Object.entries(headers(message, side)))
      for (const value of values) {
        const row = tbody.insertRow();
        for (const cellValue of [name, value])
          row.insertCell().textContent = cellValue;
      }
    panel.querySelector(".headers").replaceChildren(table);
    return;
  }
  renderRaw(
    panel.querySelector(".raw"),
    `${heading}\n${headerText(message, side)}\n\n${t("正在读取正文…")}`,
  );
  try {
    const info = message.body;
    let text = "";
    let notice = "";
    if (info.id && !record.tunnel) {
      const key = `${info.id}:${info.captured}:${info.state}`;
      if (!bodyCache.has(key)) bodyCache.set(key, host.body(info.id));
      const body = await bodyCache.get(key);
      const bytes = Uint8Array.from(atob(body.data || ""), (char) =>
        char.charCodeAt(0),
      );
      try {
        if (bytes.includes(0)) throw new Error("binary body");
        text = new TextDecoder("utf-8", { fatal: true }).decode(bytes);
      } catch {
        text = [...bytes]
          .map((byte) => byte.toString(16).padStart(2, "0"))
          .join(" ");
        notice = t("二进制正文，以十六进制显示");
      }
      if (body.info.state !== "complete")
        notice = t(
          "正文状态 {state} · 保留 {captured} B / 已观察 {observed} B",
          body.info,
        );
    } else notice = record.tunnel ? "CONNECT 加密隧道" : "正文未采集";
    if (version !== generation || views[side] !== "raw") return;
    renderRaw(
      panel.querySelector(".raw"),
      `${heading}\n${headerText(message, side)}\n\n${text}`,
    );
    panel.querySelector(".body-notice").textContent = notice;
  } catch (error) {
    if (version === generation)
      panel.querySelector(".body-notice").textContent = error.message;
  }
}

$("#search").addEventListener("input", () => {
  viewport.scrollTop = 0;
  filter();
});
$("#domain-search").addEventListener("input", renderDomains);
viewport.addEventListener("scroll", scheduleRows, { passive: true });
new ResizeObserver(scheduleRows).observe(viewport);
document.addEventListener("visibilitychange", () => {
  if (!document.hidden) scheduleRows();
});
function choose(id) {
  navigate("traffic", {
    host: shell.host,
    path: shell.path,
    app: shell.app,
    query: $("#search").value,
    selected: id,
  });
}
$("#rows").addEventListener("click", (event) => {
  const row = event.target.closest("[data-id]");
  if (row) choose(Number(row.dataset.id));
});
$("#rows").addEventListener("contextmenu", async (event) => {
  const row = event.target.closest("[data-id]");
  if (!row) return;
  event.preventDefault();
  const id = Number(row.dataset.id);
  choose(id);
  const record = await host.record(id);
  const domain = new URL(record.url).host;
  popup({ x: event.clientX, y: event.clientY }, [
    { label: "复制 URL", run: () => copy(row, record.url) },
    {
      label: "复制 cURL",
      disabled:
        record.tunnel || record.requestReceived.body.state !== "complete",
      run: async () => {
        try {
          copy(row, await host.command("curl", { ids: [id] }));
        } catch (error) {
          problem(error.message);
        }
      },
    },
    {
      label: shell.pins.includes(domain) ? "取消固定" : "固定",
      run: () =>
        command("pins", {
          values: shell.pins.includes(domain)
            ? shell.pins.filter((x) => x !== domain)
            : [...shell.pins, domain],
        }),
    },
  ]);
});
$("#all-domains").addEventListener("click", () =>
  navigate("traffic", { query: $("#search").value }),
);
for (const tab of document.querySelectorAll(".tab"))
  tab.addEventListener("click", () => {
    const side = tab.closest("[data-side]").dataset.side;
    views[side] = tab.dataset.view;
    renderDetail(side);
  });
$("#clear").addEventListener("click", async () => {
  try {
    await host.clear();
    shell.host = "";
    shell.path = "";
    shell.app = null;
    $("#search").value = "";
    $("#domain-search").value = "";
    clearSelection();
    filter();
    renderDomains();
  } catch (error) {
    problem(error.message);
  }
});
async function copy(anchor, value) {
  if (await command("clipboard", { text: value })) {
    const old = anchor.querySelector(".copy-feedback");
    if (old) old.remove();
    const notice = document.createElement("span");
    notice.className = "copy-feedback";
    notice.textContent = t("已复制");
    anchor.append(notice);
    setTimeout(() => notice.remove(), 2200);
  }
}
for (const b of document.querySelectorAll("[data-side-copy]"))
  b.addEventListener("click", async () => {
    if (!detail) return;
    const side = b.dataset.sideCopy;
    if (detail.tunnel && side === "response") return;
    const panel = $(`[data-side="${side}"]`);
    if (views[side] === "raw") await renderDetail(side);
    const message =
      side === "request" ? detail.requestReceived : detail.responseSent;
    if (message)
      copy(
        b,
        views[side] === "raw"
          ? panel.querySelector(".raw").dataset.text
          : headerText(message, side),
      );
  });
document.addEventListener("desktop:state", () => {
  $("#connection").textContent = t(connected ? "代理监听中" : "正在重新连接…");
  filter();
  $("#selection").textContent = t(selected ? "已选择 1 项" : "已选择 0 项");
  if (!shell.state.trafficEnabled) clearSelection();
  renderDomains();
});
document.addEventListener("desktop:navigate", (event) => {
  const id = event.detail;
  filter();
  renderDomains();
  if (id && filtered.some((r) => r.id === id)) select(id);
  else clearSelection();
  viewport.scrollTop = 0;
});
document.addEventListener("desktop:export", () =>
  command("export", { ids: filtered.map((r) => r.id) }),
);
document.addEventListener("keydown", (event) => {
  if (event.key === "Escape") clearSelection();
  if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === "k") {
    event.preventDefault();
    $("#search").focus();
  }
});
window.addEventListener("error", (event) =>
  remember(metrics.errors, {
    message: event.message,
    file: event.filename,
    line: event.lineno,
    column: event.colno,
    stack: event.error?.stack || "",
  }),
);
window.addEventListener("unhandledrejection", (event) =>
  remember(metrics.errors, String(event.reason)),
);

window.trafficView = {
  metrics,
  state: () => ({
    total: records.size,
    visible: filtered.length,
    selected,
    ids: filtered.map((row) => row.id),
    domRows: $("#rows").childElementCount,
    focused: document.activeElement?.id,
    scrollTop: viewport.scrollTop,
    detailRevision: detail?.revision || 0,
  }),
  status: () => host.status(),
};

let controller,
  watchGeneration = 0;
function connect() {
  controller?.abort();
  controller = new AbortController();
  const version = ++watchGeneration;
  host.watch(
    (update) => {
      if (version !== watchGeneration) return;
      metrics.batches++;
      if (update.reset) records.clear();
      for (const id of update.removed) records.delete(id);
      for (const row of update.items) records.set(row.id, row);
      filter();
      renderDomains();
      if (
        selected &&
        update.items.some(
          (row) =>
            row.id === selected && row.revision > (detail?.revision || 0),
        )
      )
        select(selected);
      requestAnimationFrame(() =>
        remember(metrics.deliveryMs, Date.now() - update.sentAt),
      );
    },
    (isConnected, error) => {
      connected = isConnected;
      $("#connection").textContent = t(
        connected ? "代理监听中" : "正在重新连接…",
      );
      if (error) remember(metrics.errors, error);
    },
    controller.signal,
  );
}
window.addEventListener("pagehide", () => controller?.abort());
window.addEventListener("focus", () => connect());
document.addEventListener("visibilitychange", () => {
  if (!document.hidden) connect();
  else controller?.abort();
});
connect();

function renderSummary(record) {
  const root = $("#detail-title");
  root.replaceChildren();
  const method = document.createElement("span");
  method.className = "ac-method-pill";
  method.textContent = record.method;
  const status = document.createElement("span");
  status.className = "ac-status-pill";
  status.textContent = record.statusCode
    ? record.statusCode +
      " " +
      ({
        200: "OK",
        201: "Created",
        204: "No Content",
        400: "Bad Request",
        401: "Unauthorized",
        404: "Not Found",
        500: "Internal Server Error",
      }[record.statusCode] || "")
    : record.state;
  const url = document.createElement("span");
  url.className = "ac-detail-path";
  url.textContent = record.url;
  url.title = record.url;
  root.append(method, status, url);
}
function renderRaw(root, text) {
  root.dataset.text = text;
  const fragment = document.createDocumentFragment();
  let header = true;
  text.split("\n").forEach((line, index) => {
    const row = document.createElement("span");
    row.className = "ac-raw-line";
    const number = document.createElement("span");
    number.className = "ac-line-number";
    number.setAttribute("aria-hidden", "true");
    number.textContent = index + 1;
    const code = document.createElement("code");
    if (index === 0) {
      code.className = "ac-wire-start";
      code.textContent = line;
    } else if (header && line.includes(":")) {
      const colon = line.indexOf(":") + 1;
      const key = document.createElement("span");
      key.className = "ac-wire-key";
      key.textContent = line.slice(0, colon);
      code.append(key, document.createTextNode(line.slice(colon)));
    } else {
      code.textContent = line || " ";
      if (!line) header = false;
    }
    row.append(number, code);
    fragment.append(row);
  });
  root.replaceChildren(fragment);
}

// Split positions are presentation state for this window, never business config.
function splitter(id, axis, container, apply, initial) {
  const handle = $(id);
  let value = initial;
  const update = (n) => {
    value = Math.max(20, Math.min(80, n));
    handle.setAttribute("aria-valuenow", Math.round(value));
    handle.setAttribute("aria-valuemin", "20");
    handle.setAttribute("aria-valuemax", "80");
    apply(value);
    scheduleRows();
  };
  handle.addEventListener("pointerdown", (event) => {
    handle.setPointerCapture(event.pointerId);
    handle.classList.add("dragging");
  });
  handle.addEventListener("pointermove", (event) => {
    if (!handle.hasPointerCapture(event.pointerId)) return;
    const rect = $(container).getBoundingClientRect();
    update(
      axis === "x"
        ? ((event.clientX - rect.left) / rect.width) * 100
        : ((event.clientY - rect.top - 44) / rect.height) * 100,
    );
  });
  handle.addEventListener("pointerup", (event) => {
    handle.releasePointerCapture(event.pointerId);
    handle.classList.remove("dragging");
  });
  handle.addEventListener("keydown", (event) => {
    if (
      ["ArrowLeft", "ArrowUp", "ArrowRight", "ArrowDown"].includes(event.key)
    ) {
      event.preventDefault();
      update(value + (["ArrowLeft", "ArrowUp"].includes(event.key) ? -1 : 1));
    }
  });
  update(initial);
}
splitter(
  "#list-split",
  "y",
  "#traffic-page",
  (value) => ($(".table-area").style.height = value + "%"),
  48,
);
splitter(
  "#detail-split",
  "x",
  ".inspectors",
  (value) => ($("[data-side=request]").style.width = value + "%"),
  50,
);
