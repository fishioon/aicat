import { state } from "../state/desktop";
import type { ExtensionState } from "../generated/mygo";
import ChannelSettings from "../channels/Settings.vue";
import ChannelDiagnostics from "../channels/Diagnostics.vue";
import ChannelBody from "../channels/Body.vue";
import { t } from "../i18n";
import type { ExtensionUI } from "./types";

// Each folder here named after an extension's ID adds to its detail; none
// is registered by hand.
const modules = import.meta.glob<{ default: ExtensionUI }>("./*/index.ts", {
  eager: true,
});
const uis: Record<string, ExtensionUI> = Object.fromEntries(
  Object.entries(modules).map(([path, m]) => [path.split("/")[1], m.default]),
);

const channelIcons = import.meta.glob<string>("./*/assets/*.png", {
  eager: true,
  query: "?url",
  import: "default",
});
const channelUI = (id: string): ExtensionUI => ({
  iconImage: channelIcons[`./${id}/assets/${id}.png`],
  Settings: ChannelSettings,
  Diagnostics: ChannelDiagnostics,
  Body: ChannelBody,
  status: () => {
    const x = state.value?.extensions.find((x) => x.id === id);
    const channel = state.value?.messageChannels[id];
    return {
      id: id + "-status",
      text:
        channel?.error ||
        t(
          !x?.installed
            ? "请先安装插件"
            : x.enabled
              ? channel?.phase === "connected"
                ? "已连接"
                : "正在连接"
              : !channel?.validated
                ? "请配置并验证账号"
                : "配置验证通过，可以启用",
        ),
    };
  },
});
export const uiOf = (id: string): ExtensionUI =>
  uis[id] ??
  (state.value?.extensions.find((x) => x.id === id)?.package
    ? channelUI(id)
    : {});

// The extensions the backend registered, in its order.
export const extensionList = (): ExtensionState[] =>
  state.value?.extensions ?? [];

export const extensionOf = (id: string): ExtensionState | undefined =>
  extensionList().find((x) => x.id === id);
