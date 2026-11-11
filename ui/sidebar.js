import { shell, navigate, command, popup } from "./shell.js";
import { t } from "./i18n.js";
const open = new Set(["apps", "pins", "domains"]);
export function appKey(row) {
  const a = row.clientApp || {};
  return a.bundleId
    ? "bundle:" + a.bundleId.toLowerCase()
    : a.name
      ? "process:" + a.name.toLowerCase()
      : "";
}
const arrow = (expanded) =>
  `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5"><path d="${expanded ? "m6 9 6 6 6-6" : "m9 6 6 6-6 6"}"/></svg>`;
export function renderSidebar(records) {
  const root = document.querySelector("#domains"),
    scroll = root.scrollTop,
    query = document.querySelector("#domain-search").value.toLowerCase();
  root.replaceChildren();
  const hosts = new Map(),
    apps = new Map();
  for (const r of records.values()) {
    const u = new URL(r.url);
    if (!hosts.has(u.host)) hosts.set(u.host, []);
    hosts.get(u.host).push(r);
    const key = appKey(r);
    if (!apps.has(key))
      apps.set(key, {
        name: r.clientApp?.name || t("未知应用"),
        requests: 0,
        tunnels: 0,
      });
    apps.get(key)[r.tunnel ? "tunnels" : "requests"]++;
  }
  function pin(anchor, domain) {
    popup(anchor, [
      {
        label: shell.pins.includes(domain) ? "取消固定" : "固定",
        run: () =>
          command("pins", {
            values: shell.pins.includes(domain)
              ? shell.pins.filter((h) => h !== domain)
              : [...shell.pins, domain],
          }),
      },
    ]);
  }
  function row(parent, label, count, filter, key, hasChildren, renderChildren) {
    const element = document.createElement("div");
    element.className = "ac-tree-row";
    element.dataset.selected = String(
      filter.app !== undefined
        ? shell.app === filter.app
        : shell.app === null &&
            shell.host === filter.host &&
            shell.path === (filter.path || ""),
    );
    const toggle = document.createElement("button");
    toggle.className = hasChildren ? "ac-tree-toggle" : "ac-tree-spacer";
    toggle.innerHTML = hasChildren ? arrow(open.has(key)) : "";
    toggle.setAttribute("aria-label", t("展开或收起") + " " + label);
    toggle.setAttribute("aria-expanded", String(open.has(key)));
    const content = document.createElement("div");
    content.className = "ac-tree-children";
    toggle.onclick = () => {
      if (!hasChildren) return;
      if (open.has(key)) {
        open.delete(key);
        if (
          shell.host === filter.host &&
          shell.path &&
          shell.path !== (filter.path || "") &&
          shell.path.startsWith(filter.path || "/")
        )
          navigate("traffic", { host: filter.host, path: filter.path || "" });
      } else open.add(key);
      renderSidebar(records);
    };
    const button = document.createElement("button");
    button.className = "ac-side-item";
    const name = document.createElement("span");
    name.textContent = label;
    const amount = document.createElement("em");
    amount.className = "ac-count";
    amount.textContent = count;
    button.append(name, amount);
    button.title = (filter.host || "") + (filter.path || "") || label;
    if (filter.host) {
      button.dataset.host = filter.host;
      button.dataset.path = filter.path || "";
      element.oncontextmenu = (e) => {
        e.preventDefault();
        pin({ x: e.clientX, y: e.clientY }, filter.host);
      };
    }
    if (filter.app !== undefined) button.dataset.app = filter.app;
    button.onclick = () =>
      navigate("traffic", {
        query:
          filter.app !== undefined
            ? document.querySelector("#search").value
            : "",
        ...filter,
      });
    element.append(toggle, button);
    parent.append(element, content);
    if (open.has(key) && renderChildren) renderChildren(content);
  }
  function group(title, key, count, render) {
    const parent = document.createElement("section");
    parent.className = "sidebar-group";
    const header = document.createElement("div");
    header.className = "ac-domain-parent";
    const toggle = document.createElement("button");
    toggle.className = "ac-domain-toggle";
    toggle.innerHTML = arrow(open.has(key));
    const label = document.createElement("button");
    label.className = "ac-pin-group-title";
    const name = document.createElement("span");
    name.textContent = t(title);
    const amount = document.createElement("em");
    amount.className = "ac-count";
    amount.textContent = count;
    label.append(name, amount);
    const collapse = () => {
      if (open.has(key)) {
        open.delete(key);
        if (
          (key === "apps" && shell.app !== null) ||
          (key === "domains" && shell.host)
        )
          navigate("traffic");
      } else open.add(key);
      renderSidebar(records);
    };
    toggle.onclick = collapse;
    label.onclick = key === "domains" ? () => navigate("traffic") : collapse;
    header.append(toggle, label);
    parent.append(header);
    root.append(parent);
    if (open.has(key)) render(parent);
  }
  group("应用", "apps", apps.size, (parent) => {
    for (const [key, app] of [...apps].sort((a, b) =>
      a[1].name.localeCompare(b[1].name),
    ))
      if (app.name.toLowerCase().includes(query))
        row(
          parent,
          app.name,
          app.requests +
            (app.tunnels ? " · " + app.tunnels + " " + t("隧道") : ""),
          { app: key },
          "app:" + key,
          false,
        );
  });
  function tree(parent, domain, rows, source) {
    const node = { count: rows.length, children: new Map() };
    for (const r of rows) {
      let current = node;
      let path = "";
      for (const part of new URL(r.url).pathname.split("/").filter(Boolean)) {
        path += "/" + part;
        if (!current.children.has(part))
          current.children.set(part, { path, count: 0, children: new Map() });
        current = current.children.get(part);
        current.count++;
      }
    }
    function children(parent, children) {
      for (const [segment, n] of [...children].sort())
        row(
          parent,
          segment,
          n.count,
          { host: domain, path: n.path },
          source + domain + n.path,
          n.children.size > 0,
          (p) => childrenRender(p, n),
        );
    }
    const childrenRender = (p, n) => children(p, n.children);
    row(
      parent,
      domain,
      rows.length,
      { host: domain },
      source + domain,
      node.children.size > 0,
      (p) => children(p, node.children),
    );
  }
  group("固定", "pins", shell.pins.length, (p) => {
    for (const h of shell.pins)
      if (h.toLowerCase().includes(query))
        tree(p, h, hosts.get(h) || [], "pins:");
  });
  group("域名", "domains", hosts.size, (p) => {
    for (const [h, rows] of [...hosts].sort())
      if (
        h.toLowerCase().includes(query) ||
        rows.some((r) => new URL(r.url).pathname.toLowerCase().includes(query))
      )
        tree(p, h, rows, "domains:");
  });
  root.scrollTop = scroll;
}
