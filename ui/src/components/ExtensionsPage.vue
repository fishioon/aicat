<script setup lang="ts">
import { computed, reactive, ref } from "vue";
import {
  state,
  shell,
  navigate,
  popup,
  run,
  say,
  modal,
  closeDialog,
} from "../state/desktop";
import { copy } from "../state/traffic";
import { host } from "../host";
import { t } from "../i18n";
import { extensionList, uiOf } from "../extensions";
import type { ExtensionState } from "../generated/mygo";
import Icon from "./Icon.vue";

// The page shows the extensions the backend registered; a folder under
// ui/src/extensions/ named after one adds its own detail.
const sections = reactive({ installed: true, recommended: true });
const tab = ref<"detail" | "features">("detail");
const current = computed(
  () =>
    extensionList().find((x) => x.id === shell.extension) ?? extensionList()[0],
);
const ui = computed(() => uiOf(current.value?.id ?? ""));
const enabled = computed(() => !!current.value?.enabled);
const status = computed(() => {
  const own = ui.value.status?.();
  if (own || enabled.value) return own;
  return { text: t("这个扩展已停用。") };
});
const matches = computed(() => {
  const query = shell.extensionQuery.trim().toLowerCase();
  return extensionList().filter(
    (x) =>
      !query ||
      [t(x.name), x.id, "aicat", t(x.summary)]
        .join(" ")
        .toLowerCase()
        .includes(query),
  );
});
function choose(id: string) {
  shell.extension = id;
  tab.value = "detail";
  navigate("extensions");
}
async function set(x: ExtensionState, on: boolean) {
  if (!(await run(() => host.setExtensionEnabled(x.id, on)))) return;
  const done = t(on ? "已启用 {name}" : "已停用 {name}", { name: t(x.name) });
  say(
    x.restart
      ? t("{action}，{restart}", { action: done, restart: t(x.restart) })
      : done,
  );
}
// An extension that decrypts HTTPS is accepted by its client only once the
// root certificate is trusted: install it first, then turn it on.
function toggle() {
  const x = current.value;
  if (!x) return;
  if (x.enabled) return set(x, false);
  if (!x.needsTrust || state.value?.certificate.trusted) return set(x, true);
  modal.value = {
    title: "需要先安装证书",
    message: t(
      "启用后，aicat 会解密这个扩展需要的 HTTPS 连接，需要先安装并信任本机根证书。安装后自动启用这个扩展。",
    ),
    actions: [
      { label: "取消", run: closeDialog },
      {
        label: "安装证书",
        run: async () => {
          closeDialog();
          if (await run(() => host.installCertificate())) await set(x, true);
        },
      },
    ],
  };
}
function gear(event: MouseEvent) {
  const id = current.value?.id ?? "";
  popup(event.currentTarget as Element, [
    ...(ui.value.menu?.() ?? []),
    {
      label: "复制扩展标识符",
      run: () => copy("extension-id", id, t("扩展标识符")),
    },
  ]);
}
const tabs = [
  ["detail", "详情"],
  ["features", "功能"],
] as const;
function tabKey(event: KeyboardEvent) {
  if (event.key !== "ArrowRight" && event.key !== "ArrowLeft") return;
  event.preventDefault();
  tab.value = tab.value === "detail" ? "features" : "detail";
  (event.currentTarget as HTMLElement).parentElement
    ?.querySelector<HTMLElement>(`[data-tab="${tab.value}"]`)
    ?.focus();
}
</script>
<template>
  <div class="split wide" v-if="state">
    <aside class="side" id="module-sidebar" :aria-label="t('扩展列表')">
      <label class="search"
        ><span class="sr">{{ t("在扩展中搜索") }}</span
        ><Icon name="search" /><input
          id="extension-search"
          v-model="shell.extensionQuery"
          class="field"
          :placeholder="t('在扩展中搜索')"
          autocomplete="off"
          spellcheck="false"
      /></label>
      <section>
        <button
          class="xsec"
          id="extensions-installed"
          :aria-expanded="sections.installed"
          @click="sections.installed = !sections.installed"
        >
          <Icon name="chev" /><span class="label">{{ t("已安装") }}</span
          ><span class="badge num">{{ matches.length }}</span>
        </button>
        <template v-if="sections.installed">
          <ul v-if="matches.length">
            <li v-for="x in matches" :key="x.id">
              <button
                :id="'extension-' + x.id"
                class="xitem"
                :class="{ off: !x.enabled }"
                :aria-current="current?.id === x.id ? 'true' : undefined"
                @click="choose(x.id)"
              >
                <span class="xicon"
                  ><Icon :name="x.id" :geometry="uiOf(x.id).icon" /></span
                ><span class="top"
                  ><strong
                    >{{ t(x.name) }}
                    <span class="tag">{{ t("内置") }}</span></strong
                  ><span class="st">{{
                    t(x.enabled ? "已启用" : "已停用")
                  }}</span></span
                ><span class="desc">{{ t(x.summary) }}</span>
              </button>
            </li>
          </ul>
          <p v-else class="xnote">{{ t("没有匹配的扩展。") }}</p>
        </template>
      </section>
      <section>
        <button
          class="xsec"
          id="extensions-recommended"
          :aria-expanded="sections.recommended"
          @click="sections.recommended = !sections.recommended"
        >
          <Icon name="chev" /><span class="label">{{ t("推荐") }}</span
          ><span class="badge num">0</span>
        </button>
        <p v-if="sections.recommended" class="xnote">
          {{ t("社区扩展的发现和安装尚未提供。") }}
        </p>
      </section>
    </aside>
    <section class="main">
      <div
        v-if="current"
        class="xd extension-detail"
        :data-extension="current.id"
      >
        <header class="xd-head">
          <span class="xicon extension-emblem"
            ><Icon :name="current.id" :geometry="ui.icon"
          /></span>
          <h1>
            {{ t(current.name) }}<span class="tag">{{ t("内置") }}</span>
          </h1>
          <p class="xd-meta">
            <span>aicat</span><span>{{ t("随 aicat 发布") }}</span>
          </p>
          <p class="lede">{{ t(current.lede) }}</p>
          <div class="xd-acts">
            <button
              :id="'feature-' + current.id"
              class="xbtn"
              :class="{ primary: !enabled }"
              @click="toggle"
            >
              {{ t(enabled ? "停用" : "启用") }}
            </button>
            <button
              id="extension-menu"
              class="icon-btn"
              aria-haspopup="menu"
              :aria-label="t('更多操作')"
              :title="t('更多操作')"
              @click="gear"
            >
              <Icon name="gear" />
            </button>
          </div>
          <p v-if="status" class="xd-status" :id="status.id">
            <Icon name="info" />{{ status.text }}
          </p>
        </header>
        <div class="ptabs xtabs" role="tablist" :aria-label="t('扩展信息')">
          <button
            v-for="[id, label] in tabs"
            :key="id"
            role="tab"
            :data-tab="id"
            :aria-selected="tab === id"
            :tabindex="tab === id ? 0 : -1"
            @click="tab = id"
            @keydown="tabKey"
          >
            {{ t(label, {}, "extension") }}
          </button>
        </div>
        <div class="xd-body">
          <div role="tabpanel">
            <component v-if="ui.Body" :is="ui.Body" :tab="tab" />
            <p v-else-if="tab === 'detail'">{{ t(current.lede) }}</p>
          </div>
          <aside class="xd-side" :aria-label="t('扩展信息')">
            <h2>{{ t("信息") }}</h2>
            <dl>
              <dt>{{ t("标识符") }}</dt>
              <dd>
                <code>{{ current.id }}</code>
              </dd>
              <dt>{{ t("发布者") }}</dt>
              <dd>aicat</dd>
              <dt>{{ t("来源") }}</dt>
              <dd>{{ t("内置") }}</dd>
            </dl>
            <h2>{{ t("分类") }}</h2>
            <div class="tags">
              <span v-for="c in current.categories" :key="c" class="tag">{{
                t(c)
              }}</span>
            </div>
          </aside>
        </div>
      </div>
    </section>
  </div>
</template>
