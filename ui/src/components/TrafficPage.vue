<script setup lang="ts">
import {
  computed,
  ref,
  watch,
  onMounted,
  onUnmounted,
  onUpdated,
  onBeforeUpdate,
} from "vue";
import { shell } from "../state/desktop";
import {
  records,
  filtered,
  detail,
  detailError,
  traffic,
  metrics,
  remember,
  choose,
  clearTraffic,
  rowMenu,
  copied,
  startTraffic,
} from "../state/traffic";
import { t } from "../i18n";
import Inspector from "./Inspector.vue";
import Splitter from "./Splitter.vue";
const viewport = ref<HTMLElement>();
const viewportHeight = ref(340);
const listHeight = ref(48),
  requestWidth = ref(50);
const first = computed(() =>
  Math.max(0, Math.floor(traffic.scrollTop / 30) - 6),
);
const visible = computed(() =>
  filtered.value.slice(
    first.value,
    Math.ceil((traffic.scrollTop + viewportHeight.value) / 30) + 6,
  ),
);
let started = performance.now();
function scroll() {
  traffic.scrollTop = viewport.value?.scrollTop ?? 0;
}
function resetScroll() {
  if (viewport.value) viewport.value.scrollTop = 0;
  traffic.scrollTop = 0;
}
watch(() => [shell.query, shell.navigationVersion], resetScroll);
let observer: ResizeObserver;
let stop: () => void;
onMounted(() => {
  observer = new ResizeObserver(() => {
    viewportHeight.value = viewport.value?.clientHeight ?? 0;
  });
  observer.observe(viewport.value!);
  stop = startTraffic();
});
onUpdated(() => {
  metrics.rowsRendered++;
  metrics.maxRowsInDOM = Math.max(metrics.maxRowsInDOM, visible.value.length);
  remember(metrics.renderMs, performance.now() - started);
});
onBeforeUpdate(() => {
  started = performance.now();
});
onUnmounted(() => {
  observer?.disconnect();
  stop?.();
});
const statusText: Record<number, string> = {
  200: "OK",
  201: "Created",
  204: "No Content",
  400: "Bad Request",
  401: "Unauthorized",
  404: "Not Found",
  500: "Internal Server Error",
};
function urlParts(url: string) {
  const parsed = new URL(url);
  return {
    scheme: parsed.protocol + "//",
    host: parsed.host,
    path: url.slice((parsed.protocol + "//" + parsed.host).length),
    clean: !parsed.username && !parsed.password,
  };
}
</script>
<template>
  <main id="traffic-page" class="ac-main">
    <div class="toolbar">
      <input
        id="search"
        v-model="shell.query"
        :aria-label="t('搜索 URL')"
        :placeholder="t('搜索 URL…')"
      /><kbd>⌘ K</kbd
      ><button id="clear" @click="clearTraffic">{{ t("清空") }}</button>
    </div>
    <section class="table-area" :style="{ height: listHeight + '%' }">
      <div class="columns table-head">
        <span>#</span><span>URL</span><span>{{ t("方法") }}</span
        ><span>{{ t("状态") }}</span
        ><span>{{ t("耗时") }}</span
        ><span>{{ t("大小") }}</span>
      </div>
      <div
        id="rows-viewport"
        ref="viewport"
        tabindex="0"
        role="grid"
        :aria-label="t('流量列表')"
        @scroll.passive="scroll"
      >
        <div id="rows-space" :style="{ height: filtered.length * 30 + 'px' }">
          <div id="rows" :style="{ transform: `translateY(${first * 30}px)` }">
            <div
              v-for="(row, index) in visible"
              :key="row.id"
              class="columns traffic-row"
              :data-id="row.id"
              :data-stripe="(first + index) % 2 === 1"
              role="row"
              :aria-selected="row.id === shell.selected"
              @click="choose(row.id)"
              @contextmenu="rowMenu($event, row)"
            >
              <span class="id">{{ row.id }}</span
              ><span class="url"
                ><template v-if="urlParts(row.url).clean"
                  ><span class="ac-url-scheme">{{
                    urlParts(row.url).scheme
                  }}</span
                  ><span class="ac-host">{{ urlParts(row.url).host }}</span
                  ><span class="ac-url-path">{{
                    urlParts(row.url).path
                  }}</span></template
                ><template v-else>{{ row.url }}</template
                ><span
                  v-if="copied.has('row:' + row.id)"
                  class="copy-feedback"
                  >{{ t("已复制") }}</span
                ></span
              >
              <span class="method">{{ row.method }}</span
              ><span class="status">{{ row.statusCode || "—" }}</span
              ><span class="duration"
                >{{ (row.durationNs / 1e6).toFixed(0) }} ms</span
              ><span class="size">{{
                row.responseBytes < 1024
                  ? row.responseBytes + " B"
                  : (row.responseBytes / 1024).toFixed(1) + " KiB"
              }}</span>
            </div>
          </div>
        </div>
        <div id="list-empty" :hidden="!!filtered.length">
          {{
            t(records.size ? "没有匹配的请求" : "等待流量 · 请在客户端配置代理")
          }}
        </div>
      </div>
    </section>
    <Splitter
      id="list-split"
      axis="y"
      container="#traffic-page"
      v-model="listHeight"
      :label="t('调整列表和详情高度')"
    />
    <section id="details">
      <div id="detail-empty" :hidden="!!shell.selected">
        {{ t("未选择请求") }}
      </div>
      <div id="detail-content" :hidden="!shell.selected">
        <div class="ac-detail-heading" id="detail-title">
          <template v-if="detail"
            ><span class="ac-method-pill">{{ detail.method }}</span
            ><span class="ac-status-pill">{{
              detail.statusCode
                ? detail.statusCode +
                  " " +
                  (statusText[detail.statusCode] ?? "")
                : detail.state
            }}</span
            ><span class="ac-detail-path" :title="detail.url">{{
              detail.url
            }}</span></template
          ><template v-else>{{
            detailError || `#${shell.selected} · ${t("正在读取…")}`
          }}</template>
        </div>
        <div class="inspectors ac-payloads">
          <Inspector
            side="request"
            :record="detail"
            :style="{ width: requestWidth + '%' }"
          /><Splitter
            id="detail-split"
            axis="x"
            container=".inspectors"
            v-model="requestWidth"
            :label="t('调整请求和响应宽度')"
          /><Inspector side="response" :record="detail" />
        </div>
      </div>
    </section>
    <footer>
      <span id="count">{{ filtered.length }} {{ t("个请求") }}</span
      ><span id="selection">{{
        t(shell.selected ? "已选择 1 项" : "已选择 0 项")
      }}</span>
    </footer>
  </main>
</template>
