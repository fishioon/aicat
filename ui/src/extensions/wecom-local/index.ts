import Body from "./Body.vue";
import Settings from "./Settings.vue";
import Diagnostics from "./Diagnostics.vue";
import iconImage from "../wecom/assets/wecom.png";
import { state } from "../../state/desktop";
import { t } from "../../i18n";
import type { ExtensionUI } from "../types";

export default {
  Body,
  Settings,
  Diagnostics,
  iconImage,
  status: () => {
    const channel = state.value?.messageChannels["wecom-local"];
    const enabled = state.value?.extensions.find(
      (x) => x.id === "wecom-local",
    )?.enabled;
    return {
      id: "wecom-local-status",
      text:
        channel?.error ||
        t(
          enabled
            ? channel?.phase === "connected"
              ? "正在读取本地消息"
              : "正在连接"
            : channel?.configured
              ? "已配置本地数据库"
              : "请配置本地数据库",
        ),
    };
  },
} satisfies ExtensionUI;
