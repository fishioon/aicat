<script setup lang="ts">
import { computed, onMounted, onUnmounted } from "vue";
import {
  state,
  shell,
  navigate,
  travel,
  popup,
  problem,
  refresh,
  run,
  menu,
} from "./state/desktop";
import { host } from "./host";
import { t } from "./i18n";
import { traffic, filtered, clearSelection } from "./state/traffic";
import Icon from "./components/Icon.vue";
import Overlays from "./components/Overlays.vue";
import TrafficSidebar from "./components/TrafficSidebar.vue";
import TrafficPage from "./components/TrafficPage.vue";
import ModuleSidebar from "./components/ModuleSidebar.vue";
import SettingsPage from "./components/SettingsPage.vue";
import ExtensionsPage from "./components/ExtensionsPage.vue";
const settings = computed(
  () => shell.route === "proxy" || shell.route === "general",
);
const breadcrumb = computed(() => {
  const section =
    shell.route === "traffic" ? "流量" : settings.value ? "设置" : "扩展";
  const page =
    shell.route === "traffic"
      ? shell.host + shell.path || "全部域名"
      : shell.route === "proxy"
        ? "代理"
        : shell.route === "general"
          ? "通用"
          : shell.route === "community"
            ? "社区扩展"
            : "流量";
  return `${t(section)}　›　${t(page)}`;
});
function more(event: MouseEvent) {
  popup(
    event.currentTarget as Element,
    state.value?.trafficEnabled
      ? [
          {
            label: "流量",
            run: () => navigate("traffic"),
            pinned: state.value.navigationPins.includes("traffic"),
            pin: () =>
              run(() =>
                host.setNavigationPins(
                  state.value!.navigationPins.includes("traffic")
                    ? []
                    : ["traffic"],
                ),
              ),
          },
        ]
      : [{ label: "暂无可用模块", disabled: true }],
  );
}
function account(event: MouseEvent) {
  popup(event.currentTarget as Element, [
    { label: "设置", run: () => navigate("proxy") },
  ]);
}
function keydown(event: KeyboardEvent) {
  if (event.key === "Escape") {
    menu.value = null;
    clearSelection();
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
    if (event.key.toLowerCase() === "k") {
      event.preventDefault();
      document.querySelector<HTMLInputElement>("#search")?.focus();
    }
  }
}
function toggleTheme() {
  if (state.value)
    void run(() =>
      host.setPreferences({
        ...state.value!.preferences,
        theme:
          document.documentElement.dataset.theme === "dark" ? "light" : "dark",
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
window.desktop = { navigate, problem, refresh, state: () => state.value };
</script>
<template>
  <header
    class="titlebar"
    id="titlebar"
    :class="{ collapsed: shell.sidebarHidden }"
  >
    <div class="window-space">
      <button
        id="history-back"
        class="icon-button"
        :aria-label="t('后退')"
        :disabled="shell.historyAt <= 0"
        @click="travel(-1)"
      >
        <Icon name="back" />
      </button>
      <button
        id="history-forward"
        class="icon-button"
        :aria-label="t('前进')"
        :disabled="shell.historyAt >= shell.history.length - 1"
        @click="travel(1)"
      >
        <Icon name="forward" />
      </button>
      <button
        id="sidebar-toggle"
        class="icon-button"
        :aria-label="t('折叠侧边栏')"
        @click="shell.sidebarHidden = !shell.sidebarHidden"
      >
        <Icon name="sidebar" />
      </button>
    </div>
    <span class="breadcrumb" id="breadcrumb">{{ breadcrumb }}</span>
    <button
      id="export"
      class="icon-button"
      :hidden="shell.route !== 'traffic'"
      :aria-label="t('导出记录')"
      @click="run(() => host.exportTraffic(filtered.map((row) => row.id)))"
    >
      <Icon name="export" />
    </button>
    <button
      id="recording"
      :hidden="shell.route !== 'traffic'"
      :disabled="!state?.trafficEnabled"
      :aria-label="t('暂停采集')"
      @click="run(() => host.setRecording(!state?.recording))"
    >
      <span class="dot"></span
      ><span id="recording-label">{{
        t(
          !state?.trafficEnabled
            ? "流量功能已关闭"
            : state.recording
              ? "正在录制"
              : "继续采集",
        )
      }}</span>
    </button>
    <button
      id="theme"
      class="icon-button"
      :aria-label="t('切换主题')"
      @click="toggleTheme"
    >
      <Icon name="sun" />
    </button>
  </header>
  <div class="shell" :class="{ 'sidebar-hidden': shell.sidebarHidden }">
    <nav class="rail ac-rail" :aria-label="t('导航')" id="navigation">
      <button
        v-if="state?.trafficEnabled && state.navigationPins.includes('traffic')"
        id="nav-traffic"
        class="ac-nav icon-button"
        :class="{ active: shell.route === 'traffic' }"
        :title="t('流量')"
        :aria-label="t('流量')"
        @click="navigate('traffic')"
      >
        <Icon name="traffic" />
      </button>
      <button
        id="nav-extensions"
        class="ac-nav icon-button"
        :class="{
          active: shell.route === 'extensions' || shell.route === 'community',
        }"
        :title="t('扩展')"
        :aria-label="t('扩展')"
        @click="navigate('extensions')"
      >
        <Icon name="extensions" />
      </button>
      <button
        id="nav-more"
        class="ac-nav icon-button"
        :title="t('更多')"
        :aria-label="t('更多')"
        @click="more"
      >
        <Icon name="more" />
      </button>
      <button
        class="avatar"
        id="account-menu"
        :title="t('设置')"
        @click="account"
      >
        AC
      </button>
    </nav>
    <aside id="sidebar" class="ac-sidebar" :hidden="shell.sidebarHidden">
      <TrafficSidebar v-show="shell.route === 'traffic'" />
      <ModuleSidebar v-show="shell.route !== 'traffic'" />
      <div class="connection">
        <span class="dot"></span
        ><span id="connection">{{
          t(traffic.connected ? "代理监听中" : "正在重新连接…")
        }}</span>
      </div>
    </aside>
    <TrafficPage v-show="shell.route === 'traffic'" />
    <main
      v-if="shell.route !== 'traffic' && state"
      class="ac-main"
      id="module-page"
    >
      <SettingsPage v-if="settings" />
      <ExtensionsPage v-else />
    </main>
  </div>
  <Overlays />
</template>
<style>
#app {
  display: contents;
}
</style>
