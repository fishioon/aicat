import { reactive, shallowRef, watch } from "vue";
import type { DesktopState } from "../generated/mygo";
import { host } from "../host";
import { language, t } from "../i18n";

export type Route =
  "traffic" | "proxy" | "general" | "extensions" | "community";
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
  route: "traffic" as Route,
  host: "",
  path: "",
  app: null as string | null,
  query: "",
  sideQuery: "",
  selected: 0,
  extensionQuery: "",
  sidebarHidden: false,
  historyAt: -1,
  history: [] as Location[],
  navigationVersion: 0,
});
export const busy = shallowRef(false);
export const notice = shallowRef("");
let noticeTimer: ReturnType<typeof setTimeout>;
export function problem(error: unknown) {
  notice.value = error instanceof Error ? error.message : String(error);
  clearTimeout(noticeTimer);
  noticeTimer = setTimeout(() => {
    notice.value = "";
  }, 5000);
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
      query: "",
      sideQuery: shell.sideQuery,
      selected: shell.selected,
      ...filter,
    });
    shell.historyAt++;
  }
  Object.assign(shell, record ? shell.history[shell.historyAt] : filter, {
    route,
  });
  shell.navigationVersion++;
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
function applyTheme() {
  const theme = state.value?.preferences.theme ?? "system";
  document.documentElement.dataset.theme =
    theme === "system" ? (dark.matches ? "dark" : "light") : theme;
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
  navigate(state.value?.trafficEnabled ? "traffic" : "extensions");
}

export interface MenuEntry {
  label: string;
  id?: string;
  disabled?: boolean;
  run?: () => unknown;
  pinned?: boolean;
  pin?: () => unknown;
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
  const box =
    anchor instanceof Element
      ? anchor.getBoundingClientRect()
      : { right: anchor.x, top: anchor.y };
  menu.value = {
    x: Math.min(box.right + 8, innerWidth - 260),
    y: Math.min(box.top, innerHeight - entries.length * 42 - 20),
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
  actions: DialogAction[];
} | null>(null);
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
