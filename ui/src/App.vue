<script setup lang="ts">
import { computed, onMounted, onUnmounted, watch } from "vue";
import {
  state,
  busy,
  shell,
  navigate,
  travel,
  problem,
  refresh,
  run,
  menu,
  proxyAddress,
  toggleSystemProxy,
  theme,
} from "./state/desktop";
import { host } from "./host";
import { t } from "./i18n";
import { extensionOf } from "./extensions";
import { records, appKey, clearSelection, copy } from "./state/traffic";
import Icon from "./components/Icon.vue";
import Overlays from "./components/Overlays.vue";
import TrafficPage from "./components/TrafficPage.vue";
import SettingsPage from "./components/SettingsPage.vue";
import ExtensionsPage from "./components/ExtensionsPage.vue";
import OverviewPage from "./components/OverviewPage.vue";
import ConnectPage from "./components/ConnectPage.vue";
import MessagesPage from "./components/MessagesPage.vue";
import ProviderPage from "./components/ProviderPage.vue";

const settings = computed(
  () => shell.route === "proxy" || shell.route === "general",
);
const hasSidebar = computed(
  () =>
    settings.value ||
    shell.route === "messages" ||
    shell.route === "traffic" ||
    shell.route === "extensions" ||
    shell.route === "extension-diagnostics",
);
const sidebarLabel = computed(() =>
  t(shell.sidebarHidden ? "展开侧边栏" : "收起侧边栏"),
);
const sidebarId = computed(() =>
  hasSidebar.value
    ? shell.route === "traffic"
      ? "traffic-sidebar"
      : "module-sidebar"
    : undefined,
);
const overview = computed(
  () =>
    shell.route === "overview" ||
    shell.route === "connect" ||
    shell.route === "provider",
);
function appName(key: string) {
  for (const row of records.value.values())
    if (appKey(row) === key) return row.clientApp?.name ?? t("未知应用");
  return key ? key.replace(/^\w+:/, "") : t("未知应用");
}
const crumbs = computed(() => {
  if (shell.route === "messages") return [t("消息")];
  if (overview.value)
    return shell.route === "connect"
      ? [t("主页"), t("接入 Agent")]
      : shell.route === "provider"
        ? [t("主页"), t("接入供应商")]
        : [t("主页")];
  if (shell.route === "traffic") {
    const parts = [
      shell.app === null ? "" : appName(shell.app),
      shell.host,
      shell.path,
    ].filter(Boolean);
    return [t("流量"), parts.length ? parts.join(" / ") : t("全部请求")];
  }
  if (shell.route === "extension-diagnostics") {
    return [
      t("扩展"),
      t(extensionOf(shell.extension)?.name ?? "扩展"),
      t("诊断"),
    ];
  }
  if (settings.value)
    return [t("设置"), t(shell.route === "proxy" ? "代理" : "通用")];
  return [t("扩展"), t(extensionOf(shell.extension)?.name ?? "流量")];
});
const breadcrumb = computed(() => crumbs.value.join(" › "));

const address = computed(() => state.value?.gateway.addr ?? "");
const gatewayDot = computed(() => {
  const status = state.value?.gateway.state;
  return status === "ready" ? "" : status === "failed" ? "bad" : "warn";
});
const addressLabel = computed(() =>
  state.value?.gateway.state === "ready"
    ? t("网关正在监听 {addr}，点击复制代理地址", { addr: address.value })
    : t("网关未就绪：{addr}", {
        addr: state.value?.gateway.error || address.value,
      }),
);
function copyProxy() {
  const proxy = "http://" + proxyAddress();
  void copy("proxy", proxy);
}

watch(
  () => shell.route,
  () => {
    menu.value = null;
  },
);

function keydown(event: KeyboardEvent) {
  if (event.defaultPrevented || document.querySelector("dialog[open]")) return;
  if (event.key === "Escape") {
    if (menu.value) {
      menu.value = null;
      return;
    }
    if (
      shell.route === "traffic" &&
      shell.selected &&
      !(event.target instanceof HTMLInputElement) &&
      !document.querySelector("dialog[open]")
    ) {
      const fromRow = !!(event.target as Element)?.closest?.("#rows-viewport");
      clearSelection();
      if (fromRow)
        document.querySelector<HTMLElement>("#rows-viewport")?.focus();
    }
  }
  if (event.metaKey || event.ctrlKey) {
    if (event.key === ",") {
      event.preventDefault();
      navigate("general");
    }
    if (event.key === "[") {
      event.preventDefault();
      travel(-1);
    }
    if (event.key === "]") {
      event.preventDefault();
      travel(1);
    }
    if (event.key.toLowerCase() === "k" && shell.route === "traffic") {
      event.preventDefault();
      const search = document.querySelector<HTMLInputElement>("#search");
      search?.focus();
      search?.select();
    }
  }
}
const dark = computed(() => theme.value === "dark");
function toggleTheme() {
  if (state.value)
    void run(() =>
      host.setPreferences({
        ...state.value!.preferences,
        theme: dark.value ? "light" : "dark",
      }),
    );
}
function focused() {
  void run(() => host.recheckCertificate());
}
onMounted(() => {
  document.addEventListener("keydown", keydown);
  window.addEventListener("focus", focused);
});
onUnmounted(() => {
  document.removeEventListener("keydown", keydown);
  window.removeEventListener("focus", focused);
});
window.desktop = {
  navigate,
  problem,
  refresh,
  toggleSystemProxy,
  state: () => state.value,
};
</script>
<template>
  <header
    class="titlebar"
    :class="{ 'sidebar-expanded': hasSidebar && !shell.sidebarHidden }"
    id="titlebar"
  >
    <div class="titlebar-controls">
      <span class="hist">
        <button
          id="history-back"
          class="icon-btn"
          :aria-label="t('后退')"
          :title="t('后退')"
          :disabled="shell.historyAt <= 0"
          @click="travel(-1)"
        >
          <Icon name="back" />
        </button>
        <button
          id="history-forward"
          class="icon-btn"
          :aria-label="t('前进')"
          :title="t('前进')"
          :disabled="shell.historyAt >= shell.history.length - 1"
          @click="travel(1)"
        >
          <Icon name="chev" />
        </button>
      </span>
      <button
        id="sidebar-toggle"
        class="icon-btn"
        :disabled="!hasSidebar"
        :aria-label="sidebarLabel"
        :title="sidebarLabel"
        :aria-expanded="hasSidebar ? !shell.sidebarHidden : undefined"
        :aria-controls="sidebarId"
        @click="shell.sidebarHidden = !shell.sidebarHidden"
      >
        <Icon name="sidebar" />
      </button>
    </div>
    <div
      class="crumb"
      id="breadcrumb"
      :aria-label="t('当前位置') + ' ' + breadcrumb"
    >
      <template v-for="(part, index) in crumbs" :key="index"
        ><Icon v-if="index" name="chev" /><span>{{ part }}</span></template
      >
    </div>
    <span class="grow"></span>
    <button
      v-if="state"
      id="gateway-address"
      class="addr"
      :aria-label="addressLabel"
      :title="addressLabel"
      @click="copyProxy"
    >
      <i class="dot" :class="gatewayDot" aria-hidden="true"></i
      ><span class="num">{{ address }}</span
      ><Icon name="copy" />
    </button>
    <button
      id="theme"
      :disabled="busy"
      class="icon-btn"
      :aria-label="t(dark ? '切换到浅色' : '切换到深色')"
      :title="t(dark ? '切换到浅色' : '切换到深色')"
      @click="toggleTheme"
    >
      <Icon :name="dark ? 'sun' : 'moon'" />
    </button>
  </header>
  <div class="shell">
    <nav class="rail" :aria-label="t('模块')" id="navigation">
      <button
        id="nav-overview"
        :title="t('主页')"
        :aria-label="t('主页')"
        :aria-current="overview ? 'page' : undefined"
        @click="navigate('overview')"
      >
        <Icon name="home" />
      </button>
      <button
        id="nav-messages"
        :title="t('消息')"
        :aria-label="t('消息')"
        :aria-current="shell.route === 'messages' ? 'page' : undefined"
        @click="navigate('messages')"
      >
        <Icon name="messages" />
      </button>
      <button
        v-if="state?.trafficEnabled"
        id="nav-traffic"
        :title="t('流量')"
        :aria-label="t('流量')"
        :aria-current="shell.route === 'traffic' ? 'page' : undefined"
        @click="navigate('traffic')"
      >
        <Icon name="traffic" />
      </button>
      <button
        id="nav-extensions"
        :title="t('扩展')"
        :aria-label="t('扩展')"
        :aria-current="
          shell.route === 'extensions' ||
          shell.route === 'extension-diagnostics'
            ? 'page'
            : undefined
        "
        @click="navigate('extensions')"
      >
        <Icon name="extensions" />
      </button>
      <span class="grow"></span>
      <button
        id="nav-settings"
        :title="t('设置')"
        :aria-label="t('设置')"
        :aria-current="settings ? 'page' : undefined"
        @click="navigate('general')"
      >
        <Icon name="settings" />
      </button>
    </nav>
    <main
      class="page"
      :class="{ 'sidebar-hidden': shell.sidebarHidden }"
      id="module-page"
      v-if="state"
    >
      <MessagesPage v-if="shell.route === 'messages'" />
      <OverviewPage v-if="shell.route === 'overview'" />
      <ConnectPage v-else-if="shell.route === 'connect'" />
      <ProviderPage v-else-if="shell.route === 'provider'" />
      <TrafficPage v-show="shell.route === 'traffic'" />
      <SettingsPage v-if="settings" />
      <ExtensionsPage
        v-if="
          shell.route === 'extensions' ||
          shell.route === 'extension-diagnostics'
        "
      />
    </main>
  </div>
  <Overlays />
</template>
