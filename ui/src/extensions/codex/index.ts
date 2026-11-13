import { computed } from "vue";
import { state, navigate } from "../../state/desktop";
import { t } from "../../i18n";
import type { ExtensionUI } from "../types";
import Body from "./Body.vue";
import iconImage from "./assets/codex-light.png";
import iconImageDark from "./assets/codex-dark.png";

const enabled = () =>
  !!state.value?.extensions.find((x) => x.id === "codex")?.enabled;

// The providers' models Codex is offered, as the gateway lists them.
const models = computed(
  () => state.value?.providers.reduce((n, p) => n + p.enabled.length, 0) ?? 0,
);

const ui: ExtensionUI = {
  iconImage,
  iconImageDark,
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
          text: t("提供 {n} 个供应商模型。更改后请重启 Codex。", {
            n: models.value,
          }),
        }
      : {
          text: t(
            "已停用。重启 Codex；使用手动接入时，先删除 openai_base_url 配置。",
          ),
        },
  Body,
};
export default ui;
