import { computed } from "vue";
import { state, navigate } from "../../state/desktop";
import { t } from "../../i18n";
import type { ExtensionUI } from "../types";
import Body from "./Body.vue";

const enabled = () =>
  !!state.value?.extensions.find((x) => x.id === "codex")?.enabled;

// The providers' models Codex is offered, as the gateway lists them.
const models = computed(
  () =>
    state.value?.providers
      .filter((p) => p.responses)
      .reduce(
        (n, p) =>
          n +
          p.models.models.filter(
            (m) => !m.apis?.length || m.apis.includes("responses"),
          ).length,
        0,
      ) ?? 0,
);

const ui: ExtensionUI = {
  menu: () => [
    {
      label: "接入供应商",
      id: "extension-codex-providers",
      run: () => navigate("provider"),
    },
    {
      label: "代理设置",
      id: "extension-proxy-settings",
      run: () => navigate("proxy"),
    },
  ],
  status: () =>
    enabled()
      ? {
          id: "codex-status",
          text: t(
            "向 Codex 提供 {n} 个供应商模型。已在运行的 Codex 需要重新启动。",
            { n: models.value },
          ),
        }
      : {
          text: t(
            "这个扩展已停用，Codex 的请求原样发往 ChatGPT。已在运行的 Codex 需要重新启动。",
          ),
        },
  Body,
};
export default ui;
