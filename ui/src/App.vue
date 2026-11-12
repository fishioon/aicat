<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, ref } from "vue";
import {
  state,
  shell,
  navigate,
  travel,
  problem,
  refresh,
  run,
  menu,
  proxyAddress,
  theme,
  type Route,
} from "./state/desktop";
import { host } from "./host";
import { t } from "./i18n";
import { records, appKey, clearSelection, copy } from "./state/traffic";
import Icon from "./components/Icon.vue";
import Overlays from "./components/Overlays.vue";
import TrafficPage from "./components/TrafficPage.vue";
import SettingsPage from "./components/SettingsPage.vue";
import ExtensionsPage from "./components/ExtensionsPage.vue";
import OverviewPage from "./components/OverviewPage.vue";
import ConnectPage from "./components/ConnectPage.vue";
import ProviderPage from "./components/ProviderPage.vue";

const settings = computed(
  () => shell.route === "proxy" || shell.route === "general",
);
const overview = computed(
  () =>
    shell.route === "overview" ||
    shell.route === "connect" ||
    shell.route === "provider",
);
const trafficPinned = computed(
  () =>
    !!state.value?.trafficEnabled &&
    state.value.navigationPins.includes("traffic"),
);
function appName(key: string) {
  for (const row of records.value.values())
    if (appKey(row) === key) return row.clientApp?.name ?? t("未知应用");
  return key ? key.replace(/^\w+:/, "") : t("未知应用");
}
const crumbs = computed(() => {
  if (overview.value)
    return shell.route === "connect"
      ? [t("概览"), t("接入 Agent")]
      : shell.route === "provider"
        ? [t("概览"), t("接入供应商")]
        : [t("概览")];
  if (shell.route === "traffic") {
    const parts = [
      shell.app === null ? "" : appName(shell.app),
      shell.host,
      shell.path,
    ].filter(Boolean);
    return [t("流量"), parts.length ? parts.join(" / ") : t("全部请求")];
  }
  if (settings.value)
    return [t("设置"), t(shell.route === "proxy" ? "代理" : "通用")];
  return [t("扩展"), shell.extension === "codex" ? "Codex" : t("流量")];
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
  void copy("proxy", proxy, t("代理地址") + " " + proxy);
}

// "更多" collects modules that are enabled but not necessarily pinned.
const more = ref<{ x: number; y: number } | null>(null);
const moreButton = ref<HTMLButtonElement>();
const popRef = ref<HTMLElement>();
async function openMore() {
  if (more.value) {
    more.value = null;
    return;
  }
  menu.value = null;
  const box = moreButton.value!.getBoundingClientRect();
  more.value = { x: box.right + 8, y: box.top };
  await nextTick();
  popRef.value?.querySelector<HTMLButtonElement>("button")?.focus();
}
function closeMore(restore = false) {
  more.value = null;
  if (restore) moreButton.value?.focus();
}
function go(route: Route) {
  closeMore();
  navigate(route);
}
function pinTraffic() {
  const pinned = state.value!.navigationPins.includes("traffic");
  void run(() =>
    host.setNavigationPins(
      pinned
        ? state.value!.navigationPins.filter((id) => id !== "traffic")
        : [...state.value!.navigationPins, "traffic"],
    ),
  );
}
function outside(event: PointerEvent) {
  if (
    more.value &&
    event.target instanceof Element &&
    !event.target.closest("#more-pop, #nav-more")
  )
    closeMore();
}

function keydown(event: KeyboardEvent) {
  if (event.key === "Escape") {
    if (more.value) {
      event.preventDefault();
      closeMore(true);
      return;
    }
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
  document.addEventListener("pointerdown", outside, true);
  window.addEventListener("focus", focused);
});
onUnmounted(() => {
  document.removeEventListener("keydown", keydown);
  document.removeEventListener("pointerdown", outside, true);
  window.removeEventListener("focus", focused);
});
window.desktop = { navigate, problem, refresh, state: () => state.value };
</script>
<template>
  <header class="titlebar" id="titlebar">
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
        :title="t('概览')"
        :aria-label="t('概览')"
        :aria-current="overview ? 'page' : undefined"
        @click="navigate('overview')"
      >
        <Icon name="home" />
      </button>
      <button
        v-if="trafficPinned"
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
        :aria-current="shell.route === 'extensions' ? 'page' : undefined"
        @click="navigate('extensions')"
      >
        <Icon name="extensions" />
      </button>
      <button
        id="nav-more"
        ref="moreButton"
        :title="t('更多')"
        :aria-label="t('更多')"
        aria-haspopup="dialog"
        :aria-expanded="!!more"
        :aria-current="
          shell.route === 'traffic' && !trafficPinned ? 'page' : undefined
        "
        @click="openMore"
      >
        <Icon name="more" />
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
    <main class="page" id="module-page" v-if="state">
      <OverviewPage v-if="shell.route === 'overview'" />
      <ConnectPage v-else-if="shell.route === 'connect'" />
      <ProviderPage v-else-if="shell.route === 'provider'" />
      <TrafficPage v-show="shell.route === 'traffic'" />
      <SettingsPage v-if="settings" />
      <ExtensionsPage v-if="shell.route === 'extensions'" />
    </main>
  </div>
  <div
    v-if="more"
    id="more-pop"
    ref="popRef"
    class="pop"
    role="dialog"
    :aria-label="t('更多模块')"
    :style="{ left: more.x + 'px', top: more.y + 'px' }"
  >
    <h2>{{ t("模块") }}</h2>
    <template v-if="state?.trafficEnabled">
      <div class="pop-row">
        <button class="open" id="more-traffic" @click="go('traffic')">
          <Icon name="traffic" />{{ t("流量") }}
        </button>
        <button
          class="icon-btn"
          data-action="pin-navigation"
          :aria-pressed="trafficPinned"
          :aria-label="t(trafficPinned ? '取消固定' : '固定到导航栏')"
          :title="t(trafficPinned ? '取消固定' : '固定到导航栏')"
          @click="pinTraffic"
        >
          <Icon name="pin" />
        </button>
      </div>
      <p>{{ t("固定的显示在导航栏，不常用的取消固定后收在这里。") }}</p>
    </template>
    <p v-else>{{ t("还没有可以收在这里的模块，可以在扩展里启用。") }}</p>
  </div>
  <Overlays />
</template>
