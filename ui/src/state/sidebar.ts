import { computed, reactive } from "vue";
import { records, appKey, failed } from "./traffic";
import { shell, state } from "./desktop";
import { t } from "../i18n";
import type { Summary } from "../generated/mygo";
export interface TreeNode {
  key: string;
  label: string;
  count: number;
  // Requests under this node that failed or returned 4xx/5xx.
  errors: number;
  host?: string;
  path?: string;
  app?: string;
  children: TreeNode[];
}
export const expanded = reactive(new Set(["apps", "pins", "domains"]));
interface Branch {
  path: string;
  count: number;
  errors: number;
  children: Map<string, Branch>;
}
function tree(
  domain: string,
  rows: Summary[],
  source: string,
  app?: string,
): TreeNode {
  const root: Branch = {
    path: "",
    count: rows.length,
    errors: 0,
    children: new Map(),
  };
  for (const row of rows) {
    const bad = failed(row) ? 1 : 0;
    root.errors += bad;
    let current = root,
      path = "";
    for (const part of new URL(row.url).pathname.split("/").filter(Boolean)) {
      path += "/" + part;
      if (!current.children.has(part))
        current.children.set(part, {
          path,
          count: 0,
          errors: 0,
          children: new Map(),
        });
      current = current.children.get(part)!;
      current.count++;
      current.errors += bad;
    }
  }
  const children = (branch: Branch): TreeNode[] =>
    [...branch.children]
      .sort(([a], [b]) => a.localeCompare(b))
      .map(([part, node]) => ({
        key: source + domain + node.path,
        label: part,
        count: node.count,
        errors: node.errors,
        host: domain,
        path: node.path,
        app,
        children: children(node),
      }));
  return {
    key: source + domain,
    label: domain,
    count: rows.length,
    errors: root.errors,
    host: domain,
    path: "",
    app,
    children: children(root),
  };
}
export const groups = computed(() => {
  const hosts = new Map<string, Summary[]>();
  const apps = new Map<
    string,
    {
      name: string;
      count: number;
      errors: number;
      hosts: Map<string, Summary[]>;
    }
  >();
  for (const row of records.value.values()) {
    const host = new URL(row.url).host;
    if (!hosts.has(host)) hosts.set(host, []);
    hosts.get(host)!.push(row);
    const key = appKey(row);
    if (!apps.has(key))
      apps.set(key, {
        name: row.clientApp?.name ?? t("未知应用"),
        count: 0,
        errors: 0,
        hosts: new Map(),
      });
    const app = apps.get(key)!;
    app.count++;
    if (failed(row)) app.errors++;
    if (!app.hosts.has(host)) app.hosts.set(host, []);
    app.hosts.get(host)!.push(row);
  }
  const query = shell.sideQuery.toLowerCase();
  const pins = state.value?.pinnedDomains ?? [];
  return [
    {
      title: "固定",
      key: "pins",
      count: pins.length,
      nodes: pins
        .filter((host) => host.toLowerCase().includes(query))
        .map((host) => tree(host, hosts.get(host) ?? [], "pins:")),
    },
    {
      title: "域名",
      key: "domains",
      count: hosts.size,
      nodes: [...hosts]
        .sort(([a], [b]) => a.localeCompare(b))
        .filter(
          ([host, rows]) =>
            host.toLowerCase().includes(query) ||
            rows.some((row) =>
              new URL(row.url).pathname.toLowerCase().includes(query),
            ),
        )
        .map(([host, rows]) => tree(host, rows, "domains:")),
    },
    {
      title: "应用",
      key: "apps",
      count: apps.size,
      nodes: [...apps]
        .sort(([, a], [, b]) => a.name.localeCompare(b.name))
        .filter(
          ([, app]) =>
            app.name.toLowerCase().includes(query) ||
            [...app.hosts].some(
              ([host, rows]) =>
                host.toLowerCase().includes(query) ||
                rows.some((row) =>
                  new URL(row.url).pathname.toLowerCase().includes(query),
                ),
            ),
        )
        .map(([key, app]): TreeNode => ({
          key: "app:" + key,
          app: key,
          label: app.name,
          count: app.count,
          errors: app.errors,
          children: [...app.hosts]
            .sort(([a], [b]) => a.localeCompare(b))
            .map(([host, rows]) =>
              tree(host, rows, "app:" + JSON.stringify(key) + ":", key),
            ),
        })),
    },
  ];
});
