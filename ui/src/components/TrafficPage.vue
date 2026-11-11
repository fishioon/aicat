<script setup lang="ts">
import {
  computed,
  ref,
  watch,
  onMounted,
  onUnmounted,
  onUpdated,
  onBeforeUpdate,
  nextTick,
} from "vue";
import { shell, state, navigate, run, proxyAddress } from "../state/desktop";
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
  copy,
  startTraffic,
} from "../state/traffic";
import { dot, status, type, duration, size, reasons } from "../state/format";
import { host } from "../host";
import { t } from "../i18n";
import Icon from "./Icon.vue";
import Inspector from "./Inspector.vue";
import Splitter from "./Splitter.vue";
import TrafficSidebar from "./TrafficSidebar.vue";
import TunnelView from "./TunnelView.vue";

const ROW = 28;
const viewport = ref<HTMLElement>();
const viewportHeight = ref(340);
const listHeight = ref(45),
  requestWidth = ref(50);
const first = computed(() =>
  Math.max(0, Math.floor(traffic.scrollTop / ROW) - 6),
);
const visible = computed(() =>
  filtered.value.slice(
    first.value,
    Math.ceil((traffic.scrollTop + viewportHeight.value) / ROW) + 6,
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
// Selecting a request records history without changing the list's position.
watch(
  () => [shell.route, shell.query, shell.host, shell.path, shell.app],
  resetScroll,
);
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

function urlParts(url: string) {
  const parsed = new URL(url);
  const origin = parsed.protocol + "//" + parsed.host;
  if (parsed.username || parsed.password || !url.startsWith(origin))
    return { host: url, path: "" };
  return { host: parsed.host, path: url.slice(origin.length) };
}
const filteredOut = computed(
  () => !!(shell.query || shell.host || shell.app !== null),
);
function resetFilters() {
  navigate("traffic", { query: "" });
}
const curl = computed(
  () => `curl --proxy http://${proxyAddress()} https://example.com`,
);
const open = computed(() => !!shell.selected);
const summary = computed(() =>
  detail.value && detail.value.id === shell.selected ? detail.value : null,
);
const tunnelSelected = computed(() => !!summary.value?.tunnel);

// ↑ ↓ Home End move the selection; the history keeps the current entry.
async function key(event: KeyboardEvent) {
  const rows = filtered.value;
  if (!rows.length) return;
  const index = rows.findIndex((row) => row.id === shell.selected);
  const target = {
    ArrowDown: index < 0 ? 0 : Math.min(index + 1, rows.length - 1),
    ArrowUp: index < 0 ? 0 : Math.max(index - 1, 0),
    Home: 0,
    End: rows.length - 1,
  }[event.key];
  if (target === undefined) return;
  event.preventDefault();
  if (index < 0) choose(rows[target].id);
  else shell.selected = rows[target].id;
  await nextTick();
  const box = viewport.value!;
  const top = target * ROW,
    bottom = top + ROW;
  if (top < box.scrollTop) box.scrollTop = top;
  else if (bottom > box.scrollTop + box.clientHeight)
    box.scrollTop = bottom - box.clientHeight;
}
</script>
<template>
  <div class="split" :class="{ collapsed: shell.sidebarHidden }">
    <TrafficSidebar />
    <section id="traffic-page" class="main tr" :class="{ open }">
      <div class="tools toolbar">
        <label class="search"
          ><span class="sr">{{ t("搜索 URL") }}</span
          ><Icon name="search" /><input
            id="search"
            v-model="shell.query"
            class="field"
            :placeholder="t('搜索 URL')"
            autocomplete="off"
            spellcheck="false"
          /><kbd>⌘K</kbd></label
        >
        <button
          id="recording"
          class="btn"
          :disabled="!state?.trafficEnabled"
          @click="run(() => host.setRecording(!state?.recording))"
        >
          <Icon :name="state?.recording ? 'pause' : 'play'" /><span
            id="recording-label"
            >{{ t(state?.recording ? "暂停采集" : "继续采集") }}</span
          >
        </button>
        <button id="clear" class="btn" @click="clearTraffic">
          {{ t("清空") }}
        </button>
        <button
          id="export"
          class="icon-btn"
          :aria-label="t('导出为 JSON')"
          :title="t('导出为 JSON')"
          @click="run(() => host.exportTraffic(filtered.map((row) => row.id)))"
        >
          <Icon name="down" />
        </button>
      </div>
      <div class="tr-body">
        <section
          class="listwrap table-area"
          :style="open ? { height: listHeight + '%' } : {}"
        >
          <div class="cols thead table-head" aria-hidden="true">
            <span></span><span>#</span><span>URL</span
            ><span class="opt">{{ t("应用", {}, "column") }}</span
            ><span>{{ t("方法") }}</span
            ><span>{{ t("状态") }}</span
            ><span class="opt">{{ t("类型") }}</span
            ><span class="opt r">{{ t("耗时") }}</span
            ><span class="opt r">{{ t("大小") }}</span>
          </div>
          <div
            id="rows-viewport"
            ref="viewport"
            tabindex="0"
            role="grid"
            :aria-label="t('流量列表')"
            @scroll.passive="scroll"
            @keydown="key"
          >
            <div
              id="rows-space"
              :style="{ height: filtered.length * ROW + 'px' }"
            >
              <div
                id="rows"
                :style="{ transform: `translateY(${first * ROW}px)` }"
              >
                <div
                  v-for="(row, index) in visible"
                  :key="row.id"
                  class="cols traffic-row"
                  :data-id="row.id"
                  :data-stripe="(first + index) % 2 === 1"
                  role="row"
                  :aria-selected="row.id === shell.selected"
                  :title="row.url"
                  @click="choose(row.id)"
                  @contextmenu="rowMenu($event, row)"
                >
                  <span aria-hidden="true"
                    ><i v-if="dot(row) === 'run'" class="sd spin"></i
                    ><i v-else class="sd" :class="dot(row)"></i></span
                  ><span class="num dim id">{{ row.id }}</span
                  ><span class="url"
                    >{{ urlParts(row.url).host
                    }}<span class="path">{{ urlParts(row.url).path }}</span
                    ><span v-if="copied.has('row:' + row.id)" class="copied">{{
                      t("已复制")
                    }}</span></span
                  ><span class="opt">{{ row.clientApp?.name ?? "" }}</span
                  ><span class="method">{{ row.method }}</span
                  ><span class="status" :class="status(row).kind"
                    ><span :class="{ num: !!row.statusCode }">{{
                      status(row).text
                    }}</span></span
                  ><span class="opt dim">{{ type(row) }}</span
                  ><span class="opt r num duration">{{ duration(row) }}</span
                  ><span class="opt r num size">{{
                    row.tunnel || row.state === "request"
                      ? ""
                      : size(row.responseBytes)
                  }}</span>
                </div>
              </div>
            </div>
            <div id="list-empty" class="empty" v-if="!filtered.length">
              <template v-if="records.size"
                ><h2>{{ t("没有符合条件的请求") }}</h2>
                <p>
                  {{
                    t("共 {n} 个请求，都被当前的搜索或筛选排除了。", {
                      n: records.size,
                    })
                  }}
                </p>
                <button
                  v-if="filteredOut"
                  id="reset-filters"
                  class="btn"
                  @click="resetFilters"
                >
                  {{ t("清除筛选") }}
                </button></template
              >
              <template v-else-if="!state?.recording"
                ><h2>{{ t("采集已暂停") }}</h2>
                <p>{{ t("代理仍在转发请求，只是不再记录。") }}</p>
                <button
                  class="btn primary"
                  @click="run(() => host.setRecording(true))"
                >
                  <Icon name="play" />{{ t("继续采集") }}
                </button></template
              >
              <template v-else
                ><h2>{{ t("还没有请求经过网关") }}</h2>
                <p>
                  {{
                    t(
                      "让客户端使用 aicat 的代理地址，请求就会出现在这里。可以先用下面这条命令试一下。",
                    )
                  }}
                </p>
                <div class="copyline">
                  <code>{{ curl }}</code
                  ><button
                    class="icon-btn"
                    :aria-label="t('复制命令')"
                    @click="copy('curl', curl, t('命令'))"
                  >
                    <Icon name="copy" />
                  </button>
                </div>
                <button class="link" @click="navigate('connect')">
                  {{ t("接入 Agent") }}
                </button></template
              >
            </div>
          </div>
        </section>
        <template v-if="open">
          <Splitter
            id="list-split"
            axis="y"
            container="#traffic-page .tr-body"
            v-model="listHeight"
            :label="t('调整列表和详情高度')"
          />
          <section id="details" class="insp">
            <div class="sum" id="detail-title">
              <template v-if="summary"
                ><span class="m">{{ summary.method }}</span
                ><span class="st"
                  ><i v-if="dot(summary) === 'run'" class="sd spin"></i
                  ><i v-else class="sd" :class="dot(summary)"></i
                  ><span :class="status(summary).kind"
                    >{{ status(summary).text
                    }}{{
                      summary.statusCode && reasons[summary.statusCode]
                        ? " " + reasons[summary.statusCode]
                        : ""
                    }}</span
                  ></span
                ><span class="url" :title="summary.url">{{ summary.url }}</span
                ><span class="meta">{{
                  [
                    summary.clientApp?.name,
                    type(summary),
                    duration(summary),
                    summary.tunnel ? "" : size(summary.responseBytes),
                  ]
                    .filter(Boolean)
                    .join("　")
                }}</span></template
              ><span v-else class="meta">{{
                detailError || `#${shell.selected} · ${t("正在读取…")}`
              }}</span>
            </div>
            <TunnelView v-if="summary && tunnelSelected" :record="summary" />
            <div v-else class="panes inspectors">
              <Inspector
                side="request"
                :record="summary"
                :style="{ width: requestWidth + '%' }"
              /><Splitter
                id="detail-split"
                axis="x"
                container=".inspectors"
                v-model="requestWidth"
                :label="t('调整请求和响应宽度')"
              /><Inspector side="response" :record="summary" />
            </div>
          </section>
        </template>
      </div>
      <footer class="foot">
        <span v-if="!traffic.connected" id="connection" class="state warn">{{
          t("正在重新连接…")
        }}</span>
        <span
          v-else
          id="connection"
          class="state"
          :class="state?.recording ? 'ok' : 'idle'"
          >{{ t(state?.recording ? "正在采集" : "已暂停，代理继续转发") }}</span
        >
        <span id="count"
          ><span class="num">{{ records.size }}</span> {{ t("个请求")
          }}<template v-if="filtered.length !== records.size"
            >{{ t("，显示") }} <span class="num">{{ filtered.length }}</span>
            {{ t("个") }}</template
          ></span
        >
        <span class="grow"></span>
        <span id="selection">{{
          shell.selected
            ? t("Esc 取消选择")
            : filtered.length
              ? t("↑ ↓ 选择请求")
              : ""
        }}</span>
      </footer>
    </section>
  </div>
</template>
