import { host } from "./client.js";
import { language, t, translate } from "./i18n.js";
import { icon } from "./icons.js";
const $ = (s) => document.querySelector(s);
export const shell = {
  state: null,
  route: "traffic",
  host: "",
  path: "",
  app: null,
  query: "",
  sideQuery: "",
  extensionQuery: "",
  pins: [],
  selected: 0,
};
let history = [],
  historyAt = -1,
  busy = false,
  noticeTimer;
export function problem(message) {
  $("#notice").textContent = message;
  $("#notice").hidden = false;
  clearTimeout(noticeTimer);
  noticeTimer = setTimeout(() => ($("#notice").hidden = true), 5000);
}
export async function command(action, input = {}) {
  if (busy) return false;
  busy = true;
  document.body.classList.add("busy");
  try {
    await host.command(action, input);
    await refresh(true);
    return true;
  } catch (error) {
    try {
      await refresh(true);
    } catch {}
    problem(error.message);
    return false;
  } finally {
    busy = false;
    document.body.classList.remove("busy");
  }
}
export function popup(anchor, entries) {
  const menu = $("#popup");
  menu.replaceChildren();
  menu.setAttribute("role", "menu");
  for (const item of entries) {
    const row = document.createElement("div");
    row.className = "menu-row";
    const button = document.createElement("button");
    button.textContent = t(item.label);
    button.disabled = item.disabled || false;
    button.dataset.action = item.id || "";
    button.setAttribute("role", "menuitem");
    button.onclick = () => {
      menu.hidden = true;
      item.run();
    };
    row.append(button);
    if (item.pin) {
      const pin = document.createElement("button");
      pin.innerHTML = icon(item.pinned ? "pin-off" : "pin");
      pin.title = t(item.pinned ? "取消固定" : "固定到侧边栏");
      pin.dataset.action = "pin-navigation";
      pin.onclick = async () => {
        await item.pin();
        menu.hidden = true;
      };
      row.append(pin);
    }
    menu.append(row);
  }
  menu.hidden = false;
  const box = anchor.getBoundingClientRect
    ? anchor.getBoundingClientRect()
    : { right: anchor.x, top: anchor.y };
  menu.style.left = Math.min(box.right + 8, innerWidth - 260) + "px";
  menu.style.top =
    Math.min(box.top, innerHeight - menu.offsetHeight - 10) + "px";
}
document.addEventListener("pointerdown", (e) => {
  if (!e.target.closest("#popup")) $("#popup").hidden = true;
});
export function dialog(title, contents, actions) {
  const d = $("#dialog");
  d.replaceChildren();
  const h = document.createElement("h2");
  h.textContent = t(title);
  d.append(h, contents);
  const footer = document.createElement("footer");
  for (const [label, run] of actions) {
    const b = document.createElement("button");
    b.textContent = t(label);
    b.onclick = run;
    footer.append(b);
  }
  d.append(footer);
  d.showModal();
}
const text = (s) => {
  const p = document.createElement("p");
  p.textContent = s;
  return p;
};
function ask(title, message, action) {
  dialog(title, text(t(message)), [
    ["取消", () => $("#dialog").close()],
    [
      "确定",
      async () => {
        $("#dialog").close();
        await action();
      },
    ],
  ]);
}
function snapshot() {
  return {
    route: shell.route,
    host: shell.host,
    path: shell.path,
    app: shell.app,
    query: $("#search").value,
    sideQuery: $("#domain-search").value,
    selected: shell.selected,
  };
}
export function navigate(route, filter = {}, record = true) {
  if (!shell.state) return;
  if (route === "traffic" && !shell.state.trafficEnabled) route = "extensions";
  if (record) {
    if (historyAt >= 0) history[historyAt] = snapshot();
    history = history.slice(0, historyAt + 1);
    history.push({
      route,
      host: "",
      path: "",
      app: null,
      query: "",
      sideQuery: $("#domain-search").value,
      selected: shell.selected,
      ...filter,
    });
    historyAt++;
  }
  const next = record ? history[historyAt] : filter;
  Object.assign(shell, next, { route });
  $("#search").value = shell.query || "";
  $("#domain-search").value = shell.sideQuery || "";
  render();
  document.dispatchEvent(
    new CustomEvent("desktop:navigate", { detail: shell.selected }),
  );
}
function travel(delta) {
  let next = historyAt + delta;
  while (
    next >= 0 &&
    next < history.length &&
    history[next].route === "traffic" &&
    !shell.state.trafficEnabled
  )
    next += delta;
  if (next < 0 || next >= history.length) return;
  history[historyAt] = snapshot();
  historyAt = next;
  navigate(history[next].route, history[next], false);
}
function renderNavigation() {
  const nav = $("#navigation");
  nav.replaceChildren();
  const add = (id, label, run) => {
    const b = document.createElement("button");
    b.id = "nav-" + id;
    b.className = "ac-nav";
    b.title = t(label);
    b.setAttribute("aria-label", t(label));
    b.innerHTML = icon(id);
    b.classList.toggle(
      "active",
      shell.route === id ||
        (id === "extensions" && shell.route === "community"),
    );
    b.onclick = run;
    nav.append(b);
    return b;
  };
  if (
    shell.state.trafficEnabled &&
    (shell.state.navigationPins || []).includes("traffic")
  )
    add("traffic", "流量", () => navigate("traffic"));
  add("extensions", "扩展", () => navigate("extensions"));
  add("more", "更多", (e) =>
    popup(
      e.currentTarget,
      shell.state.trafficEnabled
        ? [
            {
              label: "流量",
              run: () => navigate("traffic"),
              pinned: shell.state.navigationPins.includes("traffic"),
              pin: () =>
                command("navigation", {
                  values: shell.state.navigationPins.includes("traffic")
                    ? []
                    : ["traffic"],
                }),
            },
          ]
        : [{ label: "暂无可用模块", disabled: true }],
    ),
  );
  const account = document.createElement("button");
  account.className = "avatar";
  account.id = "account-menu";
  account.textContent = "AC";
  account.title = t("设置");
  account.onclick = () =>
    popup(account, [{ label: "设置", run: () => navigate("proxy") }]);
  nav.append(account);
}
function render() {
  if (!shell.state) return;
  const traffic = shell.route === "traffic";
  $("#traffic-page").hidden = !traffic;
  $("#module-page").hidden = traffic;
  $("#traffic-sidebar").hidden = !traffic;
  $("#module-sidebar").hidden = traffic;
  $("#breadcrumb").textContent =
    `${t(traffic ? "流量" : ["proxy", "general"].includes(shell.route) ? "设置" : "扩展")}　›　${t(traffic ? shell.host + shell.path || "全部域名" : shell.route === "proxy" ? "代理" : shell.route === "general" ? "通用" : shell.route === "community" ? "社区扩展" : "流量")}`;
  $("#history-back").disabled = historyAt <= 0;
  $("#history-forward").disabled = historyAt >= history.length - 1;
  $("#recording").disabled = !shell.state.trafficEnabled;
  $("#recording").hidden = !traffic;
  $("#export").hidden = !traffic;
  $("#recording-label").textContent = t(
    shell.state.trafficEnabled
      ? shell.state.recording
        ? "正在录制"
        : "继续采集"
      : "流量功能已关闭",
  );
  renderNavigation();
  if (!traffic) renderModule();
  translate();
}
function button(id, label, run) {
  const b = document.createElement("button");
  b.id = id;
  b.textContent = t(label);
  b.onclick = run;
  return b;
}
function card(parent, title) {
  const c = document.createElement("section");
  c.className = "card";
  const h = document.createElement("h3");
  h.textContent = t(title);
  c.append(h);
  parent.append(c);
  return c;
}
function renderModule() {
  const side = $("#module-sidebar"),
    page = $("#module-page");
  side.replaceChildren();
  page.replaceChildren();
  const s = shell.state;
  if (["proxy", "general"].includes(shell.route)) {
    side.append(
      button("settings-proxy", "代理", () => navigate("proxy")),
      button("settings-general", "通用", () => navigate("general")),
    );
    const title = document.createElement("h1");
    title.textContent = t(shell.route === "proxy" ? "代理" : "通用");
    page.append(title);
    const tabs = document.createElement("nav");
    tabs.className = "settings-tabs";
    tabs.append(
      button("settings-tab-proxy", "代理", () => navigate("proxy")),
      button("settings-tab-general", "通用", () => navigate("general")),
    );
    page.append(tabs);
    if (shell.route === "general") {
      const c = card(page, "语言");
      c.classList.add("general-row");
      const description = text(t("选择界面语言，保存后立即生效。"));
      description.className = "ac-rule-desc";
      c.append(description);
      const select = document.createElement("select");
      select.id = "preference-language";
      for (const [id, label] of [
        ["system", t("跟随系统")],
        ["zh-Hans", "简体中文"],
        ["en", "English"],
      ])
        select.add(new Option(label, id));
      select.value = s.preferences.language;
      select.onchange = () =>
        command("preferences", { ...s.preferences, language: select.value });
      c.append(select);
      const theme = card(page, "主题");
      theme.classList.add("general-row");
      const themeDescription = text(t("选择应用外观，保存后立即生效。"));
      themeDescription.className = "ac-rule-desc";
      theme.append(themeDescription);
      const themeChoices = document.createElement("div");
      themeChoices.className = "ac-appearance";
      theme.append(themeChoices);
      for (const [id, label] of [
        ["system", "跟随系统"],
        ["light", "浅色"],
        ["dark", "深色"],
      ])
        themeChoices.append(
          button("preference-theme-" + id, label, () =>
            command("preferences", { ...s.preferences, theme: id }),
          ),
        );
      for (const button of themeChoices.children)
        button.setAttribute(
          "aria-pressed",
          String(button.id === "preference-theme-" + s.preferences.theme),
        );
      return;
    }
    const proxy = card(page, "本地代理");
    const addr = document.createElement("p");
    addr.id = "proxy-address";
    addr.textContent = s.gateway.addr || s.gateway.Addr;
    proxy.append(addr);
    const listener = document.createElement("select");
    listener.id = "listener-address";
    const address = addr.textContent;
    const match = address.match(/^(?:\[([^\]]+)\]|([^:]+)):(\d+)$/);
    const bind = match?.[1] || match?.[2] || "127.0.0.1";
    for (const ip of [...new Set([...(s.addresses || []), bind])])
      listener.add(new Option(ip, ip));
    listener.value = bind;
    const port = document.createElement("input");
    port.id = "listener-port";
    port.value = match?.[3] || "";
    port.inputMode = "numeric";
    port.setAttribute("aria-label", t("端口"));
    const apply = button("listener-apply", "应用", () =>
      command("listener", {
        address:
          (listener.value.includes(":")
            ? `[${listener.value}]`
            : listener.value) +
          ":" +
          port.value,
      }),
    );
    const validation = document.createElement("p");
    validation.id = "listener-error";
    const validate = () => {
      apply.disabled =
        !/^\d+$/.test(port.value) ||
        Number(port.value) < 1 ||
        Number(port.value) > 65535;
      validation.textContent = apply.disabled ? t("端口范围为 1–65535") : "";
    };
    port.oninput = validate;
    validate();
    proxy.append(listener, port, apply, validation);
    const sys = card(page, "系统代理");
    sys.append(
      text(t(s.proxy.enabled ? "已启用" : "未启用")),
      button("system-proxy-toggle", s.proxy.enabled ? "关闭" : "启用", () =>
        s.proxy.enabled
          ? command("system-proxy", { enabled: false })
          : ask(
              "系统代理",
              "更改此 Mac 的 HTTP 与 HTTPS 代理，关闭时恢复原设置。",
              () => command("system-proxy", { enabled: true }),
            ),
      ),
    );
    if (s.proxy.problem) sys.append(text(s.proxy.problem));
    if (s.proxy.recovery)
      sys.append(
        button("system-proxy-restore", "恢复原设置", () =>
          command("system-proxy", { enabled: false }),
        ),
      );
    const cert = card(page, "本机证书"),
      cs = s.certificate;
    const status = text(
      t(
        cs.PendingDeletion
          ? "删除未完成"
          : !cs.Exists
            ? "未安装"
            : cs.Expired
              ? "已过期"
              : cs.Trusted
                ? "已信任"
                : "未信任",
      ),
    );
    status.id = "certificate-status";
    cert.append(status);
    if (!cs.Trusted && !cs.PendingDeletion)
      cert.append(
        button("certificate-install", "安装证书", () =>
          command("certificate-install"),
        ),
      );
    if (cs.Exists) {
      cert.append(
        button("certificate-details", "查看证书", () => {
          const p = text(
            [
              cs.Name,
              cs.Fingerprint,
              cs.NotBefore,
              cs.NotAfter,
              t("信任范围：当前 macOS 用户"),
            ].join("\n"),
          );
          p.style.whiteSpace = "pre-wrap";
          dialog("证书详情", p, [
            ["重新检测", () => command("certificate-recheck")],
            ["关闭", () => $("#dialog").close()],
          ]);
        }),
        button("certificate-export", "导出证书", () =>
          command("certificate-export"),
        ),
        button("certificate-keychain", "在钥匙串中管理", () =>
          command("certificate-keychain"),
        ),
        button("certificate-recheck", "重新检测", () =>
          command("certificate-recheck"),
        ),
        button(
          "certificate-delete",
          cs.PendingDeletion ? "重试删除…" : "删除证书…",
          () =>
            ask(
              "删除本机根证书",
              "撤销当前用户信任，并删除本机根证书与私钥。",
              () => command("certificate-delete"),
            ),
        ),
      );
    }
    if (s.certificateError) cert.append(text(s.certificateError));
    const domains = card(page, "解密域名");
    domains.append(text(t("仅解密此列表中的域名。")));
    domains.append(
      button("add-decryption-domain", "添加域名", () => {
        const input = document.createElement("input");
        input.id = "decryption-domain-input";
        input.placeholder = "example.com";
        dialog("添加解密域名", input, [
          ["取消", () => $("#dialog").close()],
          [
            "添加",
            async () => {
              if (await command("domain", { host: input.value, enabled: true }))
                $("#dialog").close();
            },
          ],
        ]);
      }),
    );
    for (const domain of s.domains) {
      const row = document.createElement("div");
      row.className = "setting-row";
      row.append(
        text(domain),
        button("remove-decryption-" + domain, "移除", () =>
          command("domain", { host: domain, enabled: false }),
        ),
      );
      domains.append(row);
    }
  } else {
    const search = document.createElement("input");
    search.id = "extension-search";
    search.placeholder = t("搜索扩展…");
    search.value = shell.extensionQuery;
    side.append(search);
    const groups = document.createElement("div");
    side.append(groups);
    const list = () => {
      groups.replaceChildren();
      for (const [title, items] of [
        ["已安装", []],
        ["内置", [["extensions", "流量"]]],
        ["推荐", [["community", "社区扩展"]]],
      ]) {
        const d = document.createElement("details");
        d.open = true;
        const h = document.createElement("summary");
        h.textContent = t(title);
        d.append(h);
        if (!items.length) d.append(text(t("暂无已安装的社区扩展")));
        for (const [id, label] of items)
          if (
            (label + " traffic http https network")
              .toLowerCase()
              .includes(search.value.toLowerCase().trim()) ||
            id === "community"
          ) {
            const item = button(
              "extension-" + (id === "extensions" ? "traffic" : id),
              label,
              () => navigate(id),
            );
            item.className = "extension-item";
            item.replaceChildren();
            const symbol = document.createElement("span");
            symbol.className = "extension-symbol";
            symbol.innerHTML = icon(
              id === "extensions" ? "traffic" : "extensions",
            );
            const info = document.createElement("span");
            const name = document.createElement("strong");
            name.textContent = t(label);
            const desc = document.createElement("small");
            desc.textContent = t(
              id === "extensions"
                ? "检查 HTTP 和 HTTPS 请求与响应"
                : "发现更多扩展能力",
            );
            const source = document.createElement("small");
            source.textContent =
              id === "extensions" ? "aicat · " + t("内置") : t("即将推出");
            info.append(name, desc, source);
            item.append(symbol, info);
            d.append(item);
          }
        groups.append(d);
      }
    };
    search.oninput = () => {
      shell.extensionQuery = search.value;
      list();
    };
    list();
    const c = card(page, shell.route === "community" ? "社区扩展" : "流量");
    c.classList.add("extension-detail");
    const emblem = document.createElement("span");
    emblem.className = "extension-emblem";
    emblem.innerHTML = icon(
      shell.route === "community" ? "extensions" : "traffic",
    );
    c.prepend(emblem);
    if (shell.route === "community") {
      c.append(text(t("即将推出")), text(t("发现更多扩展能力")));
      return;
    }
    c.append(
      text("aicat · " + t("内置扩展")),
      text(t("检查 HTTP 和 HTTPS 请求与响应")),
      button("feature-traffic", s.trafficEnabled ? "关闭" : "启用", () =>
        command("traffic", { enabled: !s.trafficEnabled }),
      ),
      button("extension-open-traffic", "打开流量", () => navigate("traffic")),
      button("extension-proxy-settings", "代理设置", () => navigate("proxy")),
    );
    $("#feature-traffic").setAttribute("role", "switch");
    $("#feature-traffic").setAttribute(
      "aria-checked",
      String(s.trafficEnabled),
    );
    $("#feature-traffic").setAttribute("aria-label", t("启用流量"));
    $("#feature-traffic").classList.add("feature-switch");
    $("#extension-open-traffic").disabled = !s.trafficEnabled;
    c.append(
      text(
        t(
          "关闭后隐藏流量入口并停止采集新请求，代理继续转发，已有记录按容量限制保留。",
        ),
      ),
    );
  }
}
async function refreshState(force = false) {
  const next = await host.state();
  const before = JSON.stringify(shell.state);
  shell.state = next;
  shell.pins = next.pinnedDomains || [];
  language(
    next.preferences.language === "system"
      ? next.language || navigator.language
      : next.preferences.language,
  );
  document.documentElement.dataset.theme =
    next.preferences.theme === "system"
      ? matchMedia("(prefers-color-scheme: dark)").matches
        ? "dark"
        : "light"
      : next.preferences.theme;
  if (!next.trafficEnabled && shell.route === "traffic") {
    navigate("extensions");
    return;
  }
  if (before !== JSON.stringify(next)) {
    if (
      !force &&
      document.activeElement?.closest("#module-page,#module-sidebar")
    ) {
      renderNavigation();
    } else render();
    document.dispatchEvent(new Event("desktop:state"));
  }
}
let refreshTask;
export async function refresh(force = false) {
  if (refreshTask) {
    await refreshTask;
    if (!force) return;
  }
  const task = refreshState(force);
  refreshTask = task;
  try {
    await task;
  } finally {
    if (refreshTask === task) refreshTask = null;
  }
}
$("#history-back").onclick = () => travel(-1);
$("#history-forward").onclick = () => travel(1);
$("#sidebar-toggle").onclick = () => {
  $("#sidebar").hidden = !$("#sidebar").hidden;
  $("#titlebar").classList.toggle("collapsed", $("#sidebar").hidden);
  document.body.classList.toggle("sidebar-hidden", $("#sidebar").hidden);
};
$("#recording").onclick = () =>
  command("recording", { enabled: !shell.state.recording });
$("#theme").onclick = () =>
  command("preferences", {
    ...shell.state.preferences,
    theme: document.documentElement.dataset.theme === "dark" ? "light" : "dark",
  });
$("#export").onclick = () =>
  document.dispatchEvent(new Event("desktop:export"));
window.desktop = { navigate, problem, refresh, state: () => shell.state };
await refresh();
navigate(shell.state.trafficEnabled ? "traffic" : "extensions");
setInterval(() => {
  if (!busy && !document.hidden)
    refresh().catch((error) => problem(error.message));
}, 1500);
window.addEventListener("focus", () =>
  command("certificate-recheck").catch((error) => problem(error.message)),
);
matchMedia("(prefers-color-scheme: dark)").addEventListener("change", () => {
  if (shell.state.preferences.theme === "system") refresh();
});

for (const [id, name] of [
  ["history-back", "back"],
  ["history-forward", "forward"],
  ["sidebar-toggle", "sidebar"],
  ["export", "export"],
  ["theme", "sun"],
])
  $("#" + id).innerHTML = icon(name);
for (const button of document.querySelectorAll("[data-side-copy]"))
  button.innerHTML = icon("copy");

document.addEventListener("keydown", (event) => {
  if (event.metaKey || event.ctrlKey) {
    if (event.key === ",") {
      event.preventDefault();
      navigate("general");
    }
    if (event.key === "[") {
      event.preventDefault();
      travel(-1);
    }
    if (event.key === "]") {
      event.preventDefault();
      travel(1);
    }
  }
  if (event.key === "Escape") $("#popup").hidden = true;
});
