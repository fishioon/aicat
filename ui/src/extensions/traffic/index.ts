import { state, navigate } from "../../state/desktop";
import { t } from "../../i18n";
import type { ExtensionUI } from "../types";

const ui: ExtensionUI = {
  menu: () => [
    {
      label: "打开流量",
      id: "extension-open-traffic",
      disabled: !state.value?.trafficEnabled,
      run: () => navigate("traffic"),
    },
    {
      label: "代理设置",
      id: "extension-proxy-settings",
      run: () => navigate("proxy"),
    },
  ],
  status: () =>
    state.value?.trafficEnabled
      ? undefined
      : { text: t("这个扩展已停用，不采集新请求，导航栏里不显示“流量”。") },
};
export default ui;
