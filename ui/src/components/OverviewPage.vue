<script setup lang="ts">
import { computed, onMounted, onUnmounted, watch } from "vue";
import { state, navigate } from "../state/desktop";
import {
  usage as latestUsage,
  usageLoading,
  usageError,
  period,
  loadUsage,
  tokens,
  hitRate,
  count,
  seconds,
} from "../state/usage";
import { t } from "../i18n";
import Icon from "./Icon.vue";
import ProviderIcon from "./ProviderIcon.vue";
import Seg from "./Seg.vue";
import UsageChart from "./UsageChart.vue";
import BrandIntro from "./BrandIntro.vue";

const usage = computed(() =>
  latestUsage.value?.period === period.value ? latestUsage.value : null,
);
const gateway = computed(() => state.value!.gateway);

// Only node-level problems the user has to act on; request failures of
// individual apps stay on their app rows.
interface Todo {
  id: string;
  title: string;
  detail: string;
  kind: "bad" | "warn";
  action: { label: string; run: () => void };
}
const todos = computed<Todo[]>(() => {
  const s = state.value!;
  const list: Todo[] = [];
  if (s.gateway.state === "failed")
    list.push({
      id: "gateway",
      title: t("网关没有在监听"),
      detail: s.gateway.error || s.gateway.addr,
      kind: "bad",
      action: { label: "监听设置", run: () => navigate("proxy") },
    });
  if (s.certificate.pendingDeletion)
    list.push({
      id: "certificate-deletion",
      title: t("根证书删除未完成"),
      detail: t("HTTPS 已保持直通，删除完成前不会签发新证书"),
      kind: "warn",
      action: { label: "代理设置", run: () => navigate("proxy") },
    });
  else if (s.certificate.exists && s.certificate.expired)
    list.push({
      id: "certificate-expired",
      title: t("根证书已过期"),
      detail: t("解密 HTTPS 前需要重新安装证书"),
      kind: "warn",
      action: { label: "代理设置", run: () => navigate("proxy") },
    });
  if (s.proxy.recovery || s.proxy.problem)
    list.push({
      id: "system-proxy",
      title: t("系统代理需要恢复"),
      detail: s.proxy.problem || t("上次退出时没有恢复原来的设置"),
      kind: "bad",
      action: { label: "代理设置", run: () => navigate("proxy") },
    });
  // what the last model list sync said about a provider's endpoint and key
  for (const p of s.providers) {
    if (!p.models.error || p.syncing) continue;
    list.push({
      id: "provider:" + p.id,
      title: t("供应商 {name} 不可用", { name: p.name }),
      detail:
        p.models.errorKind === "auth"
          ? t("密钥无效")
          : p.models.errorKind === "network"
            ? t("连不上")
            : t("没有模型列表"),
      kind: "warn",
      action: { label: "管理", run: () => openProvider(p.id) },
    });
  }
  return list;
});
const providerHost = computed(
  () =>
    new Map(
      (state.value?.providers ?? []).map((p) => [
        p.id,
        new URL(p.baseUrl).host,
      ]),
    ),
);
function openProvider(id = "") {
  navigate("provider", { provider: id });
}
const periods = [
  ["today", "今天"],
  ["7d", "7 天"],
  ["30d", "30 天"],
] as const;
const total = computed(() => usage.value?.total);
// The four largest models get a color slot each, in fixed order; the rest
// share "other". A model keeps its slot while the period stays the same.
const series = computed(() =>
  (usage.value?.models ?? [])
    .slice(0, 4)
    .map((m, index) => ({ key: m.key, name: m.name, slot: index + 1 })),
);
const slotOf = (key: string) =>
  series.value.find((s) => s.key === key)?.slot ?? 0;
const modelTop = computed(() =>
  Math.max(1, ...(usage.value?.models ?? []).map(tokens)),
);
const empty = computed(() => !total.value?.requests);
// Calls keep arriving while the page is open; it rereads every 5 seconds.
let timer: ReturnType<typeof setInterval> | undefined;
onMounted(() => {
  void loadUsage();
  timer = setInterval(() => void loadUsage(), 5000);
});
onUnmounted(() => clearInterval(timer));
watch(period, () => void loadUsage());
</script>
<template>
  <div class="main" id="overview">
    <div class="ov">
      <BrandIntro />
      <div class="ov-head">
        <h1 id="gateway-state" :class="'ov-state ' + gateway.state">
          {{
            t(
              gateway.state === "ready"
                ? "网关运行中"
                : gateway.state === "failed"
                  ? "网关没有在监听"
                  : "网关正在启动",
            )
          }}
        </h1>
        <span class="grow"></span>
        <button id="connect-provider" class="btn" @click="openProvider()">
          <Icon name="plus" />{{ t("接入供应商") }}
        </button>
        <button id="connect-agent" class="btn" @click="navigate('connect')">
          <Icon name="plus" />{{ t("接入 Agent") }}
        </button>
      </div>
      <ul v-if="todos.length" class="ov-todos" id="overview-todos">
        <li v-for="todo in todos" :key="todo.id" :class="todo.kind">
          <div>
            <strong>{{ todo.title }}</strong>
            <span>{{ todo.detail }}</span>
          </div>
          <button class="btn" @click="todo.action.run">
            {{ t(todo.action.label) }}
          </button>
        </li>
      </ul>
      <section
        v-if="state!.providers.length"
        class="ov-providers"
        id="connected-providers"
      >
        <h2>{{ t("已接入供应商") }}</h2>
        <ul class="ov-list">
          <li v-for="p in state!.providers" :key="p.id">
            <button
              class="ov-row"
              :data-provider-id="p.id"
              @click="openProvider(p.id)"
            >
              <strong class="provider-heading"
                ><ProviderIcon :image="p.icon" />{{ p.name }}</strong
              >
              <span>{{ t("管理模型") }} →</span>
              <span class="who">
                {{ providerHost.get(p.id) }} ·
                {{
                  t("已启用 {n} / {total}", {
                    n: p.enabled.length,
                    total: p.models.models.length,
                  })
                }}
                ·
                {{
                  p.syncing
                    ? t("正在同步…")
                    : p.models.error
                      ? t("需要处理")
                      : p.models.syncedAt
                        ? t("已连通")
                        : t("尚未同步")
                }}
              </span>
            </button>
          </li>
        </ul>
      </section>
      <div class="ov-bar">
        <Seg
          v-model="period"
          :options="periods"
          label="时间范围"
          id-prefix="usage-period-"
        />
        <span class="muted">{{
          t("统计经过 aicat 供应商入口的模型调用")
        }}</span>
      </div>
      <ul v-if="usageError" class="ov-todos" id="usage-error" role="alert">
        <li class="warn">
          <div>
            <strong>{{ t("无法读取用量") }}</strong>
            <span v-if="usage">{{ t("当前显示上次成功读取的数据") }}</span>
            <details>
              <summary>{{ t("错误详情") }}</summary>
              {{ usageError }}
            </details>
          </div>
          <button
            class="btn"
            id="usage-retry"
            :disabled="usageLoading"
            @click="loadUsage"
          >
            {{ t("重试") }}
          </button>
        </li>
      </ul>
      <section
        v-else-if="!usage"
        class="ov-card ov-empty"
        id="usage-loading"
        role="status"
      >
        {{ t("正在读取用量…") }}
      </section>
      <section
        class="ov-tiles"
        :aria-label="t('用量摘要')"
        v-if="total && !empty"
      >
        <div class="tile">
          <h2>Token</h2>
          <div class="v num" id="usage-tokens">{{ count(tokens(total)) }}</div>
          <div class="s">
            {{ t("输入") }} <span class="num">{{ count(total.input) }}</span> ·
            {{ t("输出") }} <span class="num">{{ count(total.output) }}</span> ·
            {{ t("缓存") }}
            <span class="num">{{
              count(total.cacheRead + total.cacheWrite)
            }}</span>
          </div>
        </div>
        <div class="tile">
          <h2>{{ t("请求") }}</h2>
          <div class="v num" id="usage-requests">{{ total.requests }}</div>
          <div class="s">
            <b v-if="total.failures" class="bad"
              ><span class="num">{{ total.failures }}</span>
              {{ t("次失败") }}</b
            ><template v-else>{{ t("没有失败") }}</template>
          </div>
        </div>
        <div class="tile">
          <h2>{{ t("缓存命中率") }}</h2>
          <div class="v num">{{ (hitRate(total) * 100).toFixed(1) }}%</div>
          <div class="meter" aria-hidden="true">
            <i :style="{ width: hitRate(total) * 100 + '%' }"></i>
          </div>
        </div>
        <div class="tile">
          <h2>{{ t("响应") }}</h2>
          <div class="v num">
            {{ seconds(total.avgMs) }}<small>{{ t("平均耗时") }}</small>
          </div>
          <div class="s">
            {{ t("首字") }} <span class="num">{{ seconds(total.ttftMs) }}</span>
            <template v-if="total.speed">
              · {{ t("输出") }}
              <span class="num">{{ Math.round(total.speed) }}</span>
              token/s</template
            >
          </div>
        </div>
      </section>
      <section v-if="usage && !empty" class="ov-card ov-usage">
        <div class="ov-chart">
          <h2>{{ t("Token 用量") }}</h2>
          <UsageChart
            :buckets="usage.buckets"
            :series="series"
            :hourly="usage.period === 'today'"
          />
        </div>
        <div class="ov-models">
          <h2>{{ t("按模型") }}</h2>
          <ul>
            <li v-for="m in usage.models.slice(0, 6)" :key="m.key">
              <div class="head">
                <i :class="'s' + slotOf(m.key)"></i>
                <strong>{{ m.name }}</strong>
                <b class="num">{{ count(tokens(m)) }}</b>
              </div>
              <div class="share">
                <i
                  :class="'s' + slotOf(m.key)"
                  :style="{ width: (tokens(m) / modelTop) * 100 + '%' }"
                ></i>
              </div>
              <span class="who"
                ><span class="num">{{ m.requests }}</span> {{ t("次请求") }} ·
                {{ t("命中率") }}
                <span class="num">{{ Math.round(hitRate(m) * 100) }}%</span>
                <template v-if="m.failures">
                  ·
                  <b class="bad"
                    ><span class="num">{{ m.failures }}</span>
                    {{ t("次失败") }}</b
                  ></template
                ></span
              >
            </li>
          </ul>
        </div>
      </section>
      <section v-else-if="usage" class="ov-card ov-empty">
        <strong>{{
          state!.providers.length
            ? t("这段时间还没有模型调用")
            : t("还没有接入供应商")
        }}</strong>
        <span>{{
          state!.providers.length
            ? t(
                "让 Agent 使用 aicat 的模型入口后，这里会显示 Token 与请求统计。",
              )
            : t(
                "接入供应商并让 Agent 使用 aicat 的模型入口后，这里会显示用量。",
              )
        }}</span>
        <button
          class="btn primary"
          @click="
            state!.providers.length ? navigate('connect') : openProvider()
          "
        >
          {{ state!.providers.length ? t("接入 Agent") : t("接入供应商") }}
        </button>
      </section>
      <div class="ov-cols" v-if="usage && !empty">
        <section>
          <h2>{{ t("按供应商") }}</h2>
          <ul class="ov-list" id="overview-providers">
            <li v-for="p in usage.providers" :key="p.key">
              <component
                :is="providerHost.has(p.key) ? 'button' : 'div'"
                class="ov-row provider"
                @click="providerHost.has(p.key) && openProvider(p.key)"
              >
                <strong class="provider-heading"
                  ><ProviderIcon
                    :image="
                      state!.providers.find((item) => item.id === p.key)?.icon
                    "
                  />{{ p.name || p.key || t("未关联供应商") }}</strong
                >
                <b class="num">{{ count(tokens(p)) }}</b>
                <span class="who"
                  ><template v-if="providerHost.has(p.key)"
                    >{{ providerHost.get(p.key) }} · </template
                  ><span class="num">{{ p.requests }}</span> {{ t("次请求") }}
                  <template v-if="p.failures">
                    ·
                    <b class="bad"
                      ><span class="num">{{ p.failures }}</span>
                      {{ t("次失败") }}</b
                    ></template
                  >
                  · {{ t("平均") }}
                  <span class="num">{{ seconds(p.avgMs) }}</span></span
                >
              </component>
            </li>
          </ul>
        </section>
        <section>
          <h2>{{ t("按 Agent") }}</h2>
          <ul class="ov-list" id="overview-agents">
            <li v-for="a in usage.agents" :key="a.key">
              <div class="ov-row">
                <strong>{{ a.name }}</strong>
                <b class="num">{{ count(tokens(a)) }}</b>
                <span class="who"
                  ><span class="num">{{ a.requests }}</span> {{ t("次请求") }} ·
                  {{ t("命中率") }}
                  <span class="num"
                    >{{ Math.round(hitRate(a) * 100) }}%</span
                  ></span
                >
              </div>
            </li>
          </ul>
        </section>
      </div>
    </div>
  </div>
</template>
