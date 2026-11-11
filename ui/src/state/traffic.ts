import { computed, reactive, shallowRef, watch } from "vue";
import type { Summary, TrafficRecord, BodySnapshot } from "../generated/mygo";
import { host } from "../host";
import { shell, state, navigate, popup, problem, run } from "./desktop";

export const records = shallowRef(new Map<number, Summary>());
export const detail = shallowRef<TrafficRecord | null>(null);
export const detailError = shallowRef("");
export const traffic = reactive({ connected: false, scrollTop: 0 });
export const metrics = {
  batches: 0,
  rowsRendered: 0,
  renderMs: [] as number[],
  filterMs: [] as number[],
  deliveryMs: [] as number[],
  errors: [] as unknown[],
  maxRowsInDOM: 0,
};
export function remember<T>(array: T[], value: T) {
  array.push(value);
  if (array.length > 600) array.shift();
}
export function appKey(row: Summary): string {
  const app = row.clientApp;
  return app?.bundleId
    ? "bundle:" + app.bundleId.toLowerCase()
    : app?.name
      ? "process:" + app.name.toLowerCase()
      : "";
}
function matches(url: string, query: string): boolean {
  const clean = (value: string) =>
    value.toLowerCase().normalize("NFD").replace(/\p{M}/gu, "");
  const text = clean(url);
  return clean(query)
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
export const filtered = computed(() => {
  const started = performance.now();
  const result = [...records.value.values()]
    .sort((a, b) => a.id - b.id)
    .filter((row) => {
      const url = new URL(row.url);
      return (
        (!shell.host || url.host === shell.host) &&
        (!shell.path ||
          url.pathname === shell.path ||
          url.pathname.startsWith(shell.path.replace(/\/$/, "") + "/")) &&
        (shell.app === null || appKey(row) === shell.app) &&
        matches(row.url, shell.query)
      );
    });
  remember(metrics.filterMs, performance.now() - started);
  return result;
});
const bodyCache = new Map<string, Promise<BodySnapshot>>();
export function body(snapshot: {
  id: string;
  captured: number;
  state: string;
}): Promise<BodySnapshot> {
  const key = `${snapshot.id}:${snapshot.captured}:${snapshot.state}`;
  if (!bodyCache.has(key)) bodyCache.set(key, host.body(snapshot.id));
  return bodyCache.get(key)!;
}
let selectionGeneration = 0;
export function clearSelection() {
  shell.selected = 0;
  detail.value = null;
  detailError.value = "";
  selectionGeneration++;
  bodyCache.clear();
}
async function loadDetail() {
  const id = shell.selected;
  const generation = ++selectionGeneration;
  detail.value = null;
  detailError.value = "";
  bodyCache.clear();
  if (!id) return;
  try {
    const record = await host.record(id);
    if (generation === selectionGeneration && records.value.has(id))
      detail.value = record;
  } catch (error) {
    if (generation === selectionGeneration)
      detailError.value =
        error instanceof Error ? error.message : String(error);
  }
}
watch(
  () => shell.selected,
  () => {
    void loadDetail();
  },
);
watch(filtered, (rows) => {
  if (shell.selected && !rows.some((row) => row.id === shell.selected))
    clearSelection();
});
watch(
  () => state.value?.trafficEnabled,
  (enabled) => {
    if (enabled === false) clearSelection();
  },
);
export function choose(id: number) {
  navigate("traffic", {
    host: shell.host,
    path: shell.path,
    app: shell.app,
    query: shell.query,
    selected: id,
  });
}
export async function clearTraffic() {
  try {
    await host.clearTraffic();
    shell.host = "";
    shell.path = "";
    shell.app = null;
    shell.query = "";
    shell.sideQuery = "";
    clearSelection();
  } catch (error) {
    problem(error);
  }
}
export const copied = reactive(new Set<string>());
export async function copy(key: string, text: string) {
  if (await run(() => host.copyText(text))) {
    copied.add(key);
    setTimeout(() => copied.delete(key), 2200);
  }
}
export function pinDomain(domain: string) {
  const pins = state.value?.pinnedDomains ?? [];
  return run(() =>
    host.setDomainPins(
      pins.includes(domain)
        ? pins.filter((pin) => pin !== domain)
        : [...pins, domain],
    ),
  );
}
export async function rowMenu(event: MouseEvent, row: Summary) {
  event.preventDefault();
  choose(row.id);
  let record: TrafficRecord;
  try {
    record = await host.record(row.id);
  } catch (error) {
    problem(error);
    return;
  }
  if (shell.selected !== row.id || shell.route !== "traffic") return;
  const domain = new URL(row.url).host;
  popup({ x: event.clientX, y: event.clientY }, [
    { label: "复制 URL", run: () => copy("row:" + row.id, row.url) },
    {
      label: "复制 cURL",
      disabled:
        record.tunnel || record.requestReceived.body.state !== "complete",
      run: async () => {
        try {
          await copy("row:" + row.id, await host.requestCURL(row.id));
        } catch (error) {
          problem(error);
        }
      },
    },
    {
      label: state.value?.pinnedDomains.includes(domain) ? "取消固定" : "固定",
      run: () => pinDomain(domain),
    },
  ]);
}

let controller: AbortController | undefined;
let watchGeneration = 0;
let retryTimer: ReturnType<typeof setTimeout>;
let activeSubscriptions = 0;
function connect() {
  clearTimeout(retryTimer);
  controller?.abort();
  controller = new AbortController();
  const signal = controller.signal;
  const generation = ++watchGeneration;
  activeSubscriptions++;
  void host
    .watch((batch) => {
      if (generation !== watchGeneration || signal.aborted) return;
      traffic.connected = true;
      metrics.batches++;
      const next = batch.reset
        ? new Map<number, Summary>()
        : new Map(records.value);
      for (const id of batch.removed ?? []) next.delete(id);
      for (const row of batch.items ?? []) next.set(row.id, row);
      records.value = next;
      if (
        shell.selected &&
        batch.items?.some(
          (row) =>
            row.id === shell.selected &&
            row.revision > (detail.value?.revision ?? 0),
        )
      )
        void loadDetail();
      requestAnimationFrame(() =>
        remember(metrics.deliveryMs, Date.now() - batch.sentAt),
      );
    }, signal)
    .catch((error: unknown) => {
      if (!signal.aborted && generation === watchGeneration) {
        traffic.connected = false;
        problem(error);
      }
    })
    .finally(() => {
      activeSubscriptions--;
      if (!signal.aborted && generation === watchGeneration) {
        traffic.connected = false;
        retryTimer = setTimeout(connect, 500);
      }
    });
}
export function startTraffic() {
  connect();
  const visibility = () => {
    if (document.hidden) {
      clearTimeout(retryTimer);
      controller?.abort();
    } else connect();
  };
  const focus = () => {
    if (!document.hidden) connect();
  };
  document.addEventListener("visibilitychange", visibility);
  window.addEventListener("focus", focus);
  window.addEventListener(
    "pagehide",
    () => {
      clearTimeout(retryTimer);
      controller?.abort();
    },
    { once: true },
  );
  return () => {
    document.removeEventListener("visibilitychange", visibility);
    window.removeEventListener("focus", focus);
    clearTimeout(retryTimer);
    controller?.abort();
  };
}
window.addEventListener("error", (event) =>
  remember(metrics.errors, {
    message: event.message,
    file: event.filename,
    line: event.lineno,
    column: event.colno,
    stack: event.error?.stack ?? "",
  }),
);
window.addEventListener("unhandledrejection", (event) =>
  remember(metrics.errors, String(event.reason)),
);
window.trafficView = {
  metrics,
  state: () => ({
    total: records.value.size,
    visible: filtered.value.length,
    selected: shell.selected,
    ids: filtered.value.map((row) => row.id),
    domRows: document.querySelector("#rows")?.childElementCount ?? 0,
    focused: document.activeElement?.id,
    scrollTop: traffic.scrollTop,
    detailRevision: detail.value?.revision ?? 0,
  }),
  status: () => ({ activeSubscriptions, transport: "mygo-channel" }),
};
