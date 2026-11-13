import Body from "./Body.vue";
import Settings from "./Settings.vue";
import Diagnostics from "./Diagnostics.vue";
import iconImage from "../wecom/assets/wecom.png";
import { state } from "../../state/desktop";
import { t } from "../../i18n";
import type { ExtensionUI } from "../types";

const errors: Record<string, string> = {
  LOCAL_DATABASE_READ_FAILED: "无法读取本地数据库，请检查目录和密钥。",
  LOCAL_DATABASE_OPEN_FAILED: "无法读取本地数据库，请检查目录和密钥。",
  DATABASE_INTEGRITY_CHECK_FAILED: "数据库校验失败，请检查密钥和文件。",
  SOURCE_FILE_TOO_LARGE_OR_NOT_REGULAR: "数据库文件不可用或超出大小限制。",
  SOURCE_CHANGED_DURING_SNAPSHOT: "数据库正在变化，稍后自动重试。",
  DATABASE_IDENTITY_CHANGED_CURSOR_REJECTED:
    "数据库与读取进度不匹配，请恢复原数据库或更换来源。",
  DATABASE_RESET_CURSOR_REJECTED:
    "数据库与读取进度不匹配，请恢复原数据库或更换来源。",
  INVALID_CURSOR: "读取进度不可用，已暂停读取。",
  INVALID_SAVED_CURSOR: "读取进度不可用，已暂停读取。",
  INVALID_MESSAGE_OR_OVERSIZED_CONTENT:
    "消息格式不受支持或内容过大，已暂停读取。",
  INVALID_MESSAGE_TIMESTAMP: "消息格式不受支持或内容过大，已暂停读取。",
  INVALID_BASE64_KEY: "数据库密钥无效，请重新配置。",
  ABSOLUTE_LOCAL_PATH_REQUIRED: "请重新配置本地账号与数据库目录。",
  INVALID_ACCOUNT_ID: "请重新配置本地账号与数据库目录。",
  LOCAL_SQLITE_UNAVAILABLE: "数据库组件不可用，请重新安装 aicat。",
  HOST_ALREADY_RUNNING: "本地读取器已在运行，请关闭其他实例后重试。",
  INVALID_HOST_LOCK: "本地读取器状态不可用，请检查本机状态文件。",
};

export default {
  Body,
  features: false,
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
      text: channel?.error
        ? t(errors[channel.error] ?? channel.error)
        : t(
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
