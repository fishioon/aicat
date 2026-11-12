import { reactive, shallowRef, watch } from "vue";
import type { DesktopState } from "../generated/mygo";
import { host } from "../host";
import { language, t } from "../i18n";

export type Route =
  | "overview"
  | "connect"
  | "provider"
  | "traffic"
  | "proxy"
  | "general"
  | "extensions";
export interface Location {
  route: Route;
  host: string;
  path: string;
  app: string | null;
  query: string;
  sideQuery: string;
  selected: number;
}
export const state = shallowRef<DesktopState | null>(null);
export const shell = reactive({
  route: "overview" as Route,
  host: "",
  path: "",
  app: null as string | null,
  query: "",
  sideQuery: "",
  selected: 0,
  extensionQuery: "",
  extension: "traffic" as "traffic" | "codex",
  sidebarHidden: false,
  historyAt: -1,
  history: [] as Location[],
});
// The model vendors aicat can hold a key for. Mounts and defaults mirror
// internal/provider; env is what an agent sets to reach the mount.
export const providerKinds = {
  anthropic: {
    name: "Anthropic",
    url: "https://api.anthropic.com",
    env: "ANTHROPIC_BASE_URL",
    mount: "/anthropic",
    placeholder: "sk-ant-…",
  },
  openai: {
    name: "OpenAI",
    url: "https://api.openai.com/v1",
    env: "OPENAI_BASE_URL",
    mount: "/openai/v1",
    placeholder: "sk-…",
  },
  compat: {
    name: "兼容 OpenAI 的服务",
    url: "",
    env: "OPENAI_BASE_URL",
    mount: "/compat/v1",
    placeholder: "API Key",
  },
} as const;
export type ProviderKind = keyof typeof providerKinds;
export const providerChoice = shallowRef<ProviderKind>("anthropic");
export const busy = shallowRef(false);
export const notice = shallowRef("");
export const noticeKind = shallowRef<"bad" | "">("");
let noticeTimer: ReturnType<typeof setTimeout>;
function show(text: string, kind: "bad" | "", ms: number) {
  notice.value = text;
  noticeKind.value = kind;
  clearTimeout(noticeTimer);
  noticeTimer = setTimeout(() => {
    notice.value = "";
  }, ms);
}
export function problem(error: unknown) {
  show(error instanceof Error ? error.message : String(error), "bad", 5000);
}
// say shows a short confirmation; text is already translated.
export function say(text: string) {
  show(text, "", 2600);
}
function snapshot(): Location {
  const { route, host, path, app, query, sideQuery, selected } = shell;
  return { route, host, path, app, query, sideQuery, selected };
}
export function navigate(
  route: Route,
  filter: Partial<Location> = {},
  record = true,
) {
  if (!state.value) return;
  if (route === "traffic" && !state.value.trafficEnabled) route = "extensions";
  if (record) {
    if (shell.historyAt >= 0) shell.history[shell.historyAt] = snapshot();
    shell.history.splice(shell.historyAt + 1);
    shell.history.push({
      route,
      host: "",
      path: "",
      app: null,
      query: shell.query,
      sideQuery: shell.sideQuery,
      selected: shell.selected,
      ...filter,
    });
    shell.historyAt++;
  }
  Object.assign(shell, record ? shell.history[shell.historyAt] : filter, {
    route,
  });
}
export function travel(delta: number) {
  let next = shell.historyAt + delta;
  while (
    next >= 0 &&
    next < shell.history.length &&
    shell.history[next].route === "traffic" &&
    !state.value?.trafficEnabled
  )
    next += delta;
  if (next < 0 || next >= shell.history.length) return;
  shell.history[shell.historyAt] = snapshot();
  shell.historyAt = next;
  navigate(shell.history[next].route, shell.history[next], false);
}
let refreshTask: Promise<void> | undefined;
let dirty = false;
export async function refresh() {
  dirty = true;
  if (refreshTask) return refreshTask;
  refreshTask = (async () => {
    while (dirty) {
      dirty = false;
      const next = await host.state();
      state.value = next;
      language(
        next.preferences.language === "system"
          ? next.language
          : next.preferences.language,
      );
      if (!next.trafficEnabled && shell.route === "traffic")
        navigate("extensions");
    }
  })();
  try {
    await refreshTask;
  } finally {
    refreshTask = undefined;
  }
}
export async function run(operation: () => Promise<unknown>): Promise<boolean> {
  if (busy.value) return false;
  busy.value = true;
  try {
    await operation();
    await refresh();
    return true;
  } catch (error) {
    await refresh().catch(() => {});
    problem(error);
    return false;
  } finally {
    busy.value = false;
  }
}
const dark = matchMedia("(prefers-color-scheme: dark)");
export const theme = shallowRef<"light" | "dark">("light");
function applyTheme() {
  const preference = state.value?.preferences.theme ?? "system";
  theme.value =
    preference === "dark" || (preference === "system" && dark.matches)
      ? "dark"
      : "light";
  document.documentElement.dataset.theme = theme.value;
}
watch(state, applyTheme);
dark.addEventListener("change", applyTheme);
watch(busy, (value) => document.body.classList.toggle("busy", value));
export async function startDesktop() {
  const off = host.onStateChanged(() => {
    void refresh().catch(problem);
  });
  window.addEventListener("pagehide", off, { once: true });
  await refresh();
  navigate("overview");
}

export interface MenuEntry {
  label: string;
  id?: string;
  disabled?: boolean;
  run?: () => unknown;
}
export const menu = shallowRef<{
  x: number;
  y: number;
  entries: MenuEntry[];
} | null>(null);
export function popup(
  anchor: Element | { x: number; y: number },
  entries: MenuEntry[],
) {
  const point =
    anchor instanceof Element
      ? (({ left, bottom }) => ({ x: left, y: bottom + 4 }))(
          anchor.getBoundingClientRect(),
        )
      : anchor;
  menu.value = {
    x: Math.min(point.x, innerWidth - 190),
    y: Math.min(point.y, innerHeight - entries.length * 30 - 16),
    entries,
  };
}
export interface DialogAction {
  label: string;
  run: () => unknown;
}
export const modal = shallowRef<{
  title: string;
  message?: string;
  input?: string;
  inputId?: string;
  placeholder?: string;
  actions: DialogAction[];
} | null>(null);
// proxyAddress is the address clients use to reach the gateway; a wildcard
// listener is reached through loopback.
export function proxyAddress(addr = state.value?.gateway.addr ?? "") {
  return addr.replace(/^(0\.0\.0\.0|\[::\])(?=:)/, "127.0.0.1");
}
// 返回覆盖该域名的解密条目：精确条目，或 *.example.com 这类通配条目。
// 通配条目匹配任意层级的子域名，不含 example.com 本身。
export function decryptionRule(domain: string) {
  const name = domain.toLowerCase().replace(/\.$/, "");
  return state.value?.domains.find((entry) =>
    entry.startsWith("*.")
      ? name.endsWith(entry.slice(1)) && name.length > entry.length - 1
      : entry === name,
  );
}
export function closeDialog() {
  modal.value = null;
}
export function ask(title: string, message: string, action: () => unknown) {
  modal.value = {
    title,
    message: t(message),
    actions: [
      { label: "取消", run: closeDialog },
      {
        label: "确定",
        run: () => {
          closeDialog();
          return action();
        },
      },
    ],
  };
}
