<script setup lang="ts">
import { computed } from "vue";
import { state, navigate, proxyAddress } from "../state/desktop";
import { providerId } from "../state/providers";
import { records, appKey, failed, running, copy } from "../state/traffic";
import { status } from "../state/format";
import { t } from "../i18n";
import Icon from "./Icon.vue";

const gateway = computed(() => state.value!.gateway);
const trafficOn = computed(() => !!state.value?.trafficEnabled);
const base = computed(() => "http://" + proxyAddress());

// Statistics cover the bounded traffic records currently kept in memory.
const stats = computed(() => {
  const rows = [...records.value.values()];
  const http = rows.filter((row) => !row.tunnel);
  const done = http.filter((row) => !running(row));
  const bad = http.filter(failed);
  const apps = new Map<
    string,
    {
      key: string;
      name: string;
      requests: number;
      failures: number;
      last: number;
    }
  >();
  const hosts = new Set<string>();
  for (const row of rows) {
    hosts.add(new URL(row.url).host);
    const key = appKey(row);
    if (!apps.has(key))
      apps.set(key, {
        key,
        name: row.clientApp?.name ?? t("未知应用"),
        requests: 0,
        failures: 0,
        last: 0,
      });
    const app = apps.get(key)!;
    if (!row.tunnel) app.requests++;
    if (failed(row)) app.failures++;
    app.last = Math.max(app.last, Date.parse(row.startedAt) || 0);
  }
  return {
    total: http.length,
    running: http.length - done.length,
    failures: bad.length,
    success: done.length
      ? Math.floor(((done.length - bad.length) / done.length) * 1000) / 10
      : 100,
    hosts: hosts.size,
    tunnels: rows.length - http.length,
    apps: [...apps.values()].sort((a, b) => b.last - a.last),
  };
});

function ago(time: number) {
  const seconds = Math.max(0, (Date.now() - time) / 1000);
  if (seconds < 60) return t("刚刚");
  if (seconds < 3600) return t("{n} 分钟前", { n: Math.floor(seconds / 60) });
  if (seconds < 86400)
    return t("{n} 小时前", { n: Math.floor(seconds / 3600) });
  return t("{n} 天前", { n: Math.floor(seconds / 86400) });
}

interface Todo {
  id: string;
  title: string;
  detail: string;
  kind: "bad" | "warn";
  action?: { label: string; run: () => void };
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
  if (trafficOn.value) {
    const groups = new Map<
      string,
      {
        app: string | null;
        name: string;
        host: string;
        count: number;
        text: string;
      }
    >();
    for (const row of records.value.values()) {
      if (!failed(row) || row.tunnel) continue;
      const host = new URL(row.url).host;
      const key = appKey(row) + " " + host;
      const group = groups.get(key) ?? {
        app: appKey(row),
        name: row.clientApp?.name ?? t("未知应用"),
        host,
        count: 0,
        text: "",
      };
      group.count++;
      group.text = row.statusCode
        ? status(row).text + (row.error ? " · " + row.error : "")
        : row.error || status(row).text;
      groups.set(key, group);
    }
    for (const group of [...groups.values()]
      .sort((a, b) => b.count - a.count)
      .slice(0, 4))
      list.push({
        id: "failed:" + group.app + group.host,
        title: t("{app} 调用 {host} 失败", {
          app: group.name,
          host: group.host,
        }),
        detail:
          t("{n} 次", { n: group.count }) + " · " + t("最近：") + group.text,
        kind: "bad",
        action: {
          label: "查看请求",
          run: () =>
            navigate("traffic", {
              app: group.app,
              host: group.host,
              query: "",
            }),
        },
      });
  }
  return list;
});
const providers = computed(() =>
  (state.value?.providers ?? []).map((p) => ({
    id: p.id,
    name: p.name,
    host: new URL(p.baseUrl).host,
    // what the last model list sync said about the endpoint and key
    problem: !p.models.error
      ? ""
      : p.models.errorKind === "auth"
        ? t("密钥无效")
        : p.models.errorKind === "network"
          ? t("连不上")
          : "",
  })),
);
function openProvider(id = "") {
  providerId.value = id;
  navigate("provider");
}
const endpoints = computed(() => [
  ["MCP", base.value + "/mcp", t("MCP 地址")],
  ["HTTP API", base.value + "/api/traffic", t("API 地址")],
  [t("HTTP 代理"), base.value, t("代理地址")],
]);
</script>
<template>
  <div class="main" id="overview">
    <div class="ov">
      <div class="ov-head">
        <div>
          <h1 id="gateway-state">
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
          <p>
            {{ t("aicat 在后台监听") }}
            <span class="num">{{ gateway.addr }}</span
            >{{
              trafficOn
                ? t("。统计来自当前保留的流量记录。")
                : t("。流量扩展已停用，这里不统计请求。")
            }}
          </p>
        </div>
        <span class="grow"></span>
        <button id="connect-provider" class="btn" @click="openProvider()">
          <Icon name="plus" />{{ t("接入供应商") }}
        </button>
        <button id="connect-agent" class="btn" @click="navigate('connect')">
          <Icon name="plus" />{{ t("接入 Agent") }}
        </button>
      </div>
      <section v-if="trafficOn" class="card tiles" :aria-label="t('请求摘要')">
        <div class="tile">
          <h2>{{ t("请求") }}</h2>
          <div class="v num" id="stat-requests">{{ stats.total }}</div>
          <div class="s">
            {{ t("进行中") }} <span class="num">{{ stats.running }}</span>
          </div>
        </div>
        <div class="tile">
          <h2>{{ t("失败") }}</h2>
          <div class="v num">{{ stats.failures }}</div>
          <div class="s">
            {{ t("成功") }}
            <span class="num">{{ stats.success.toFixed(1) }}%</span>
          </div>
        </div>
        <div class="tile">
          <h2>{{ t("应用", {}, "column") }}</h2>
          <div class="v num">{{ stats.apps.length }}</div>
          <div class="s">{{ t("直接连接代理的本机进程") }}</div>
        </div>
        <div class="tile">
          <h2>{{ t("域名") }}</h2>
          <div class="v num">{{ stats.hosts }}</div>
          <div class="s">
            {{ t("未解密隧道") }} <span class="num">{{ stats.tunnels }}</span>
          </div>
        </div>
      </section>
      <div class="ov-cols">
        <section>
          <h2>{{ t("已接入") }}</h2>
          <p class="sub">{{ t("供应商，以及经过网关的应用") }}</p>
          <ul class="conn" id="overview-apps">
            <li
              v-for="provider in providers"
              :key="'provider:' + provider.id"
              class="provider"
            >
              <div>
                <strong>{{ provider.name }}</strong
                ><span class="who"
                  >{{ t("供应商") }} · {{ provider.host }} ·
                  <span v-if="provider.problem" class="bad">{{
                    provider.problem
                  }}</span
                  ><template v-else>{{
                    t("密钥由 aicat 保管")
                  }}</template></span
                >
              </div>
              <button class="btn" @click="openProvider(provider.id)">
                {{ t("管理") }}
              </button>
            </li>
            <li v-for="app in stats.apps.slice(0, 8)" :key="app.key">
              <div>
                <strong>{{ app.name }}</strong
                ><span class="who"
                  ><span class="num">{{ app.requests }}</span> {{ t("次请求")
                  }}<template v-if="app.failures">
                    ·
                    <b class="bad"
                      ><span class="num">{{ app.failures }}</span>
                      {{ t("次失败") }}</b
                    ></template
                  >
                  · {{ ago(app.last) }}</span
                >
              </div>
              <button
                class="btn"
                @click="
                  navigate('traffic', { app: app.key, host: '', path: '' })
                "
              >
                {{ t("查看") }}
              </button>
            </li>
            <li v-if="!stats.apps.length && !providers.length">
              <div>
                <span class="empty-line">{{
                  t("还没有请求经过网关。让客户端使用下面的代理地址。")
                }}</span>
              </div>
            </li>
          </ul>
        </section>
        <div>
          <section>
            <h2>{{ t("需要处理") }}</h2>
            <p class="sub">{{ t("连接异常和等待你确认的事项") }}</p>
            <ul class="conn" id="overview-todos">
              <li v-for="todo in todos" :key="todo.id">
                <div>
                  <strong>{{ todo.title }}</strong
                  ><span class="who" :class="todo.kind">{{ todo.detail }}</span>
                </div>
                <button v-if="todo.action" class="btn" @click="todo.action.run">
                  {{ t(todo.action.label) }}
                </button>
              </li>
              <li v-if="!todos.length">
                <div>
                  <span class="empty-line">{{ t("没有需要处理的事项") }}</span>
                </div>
              </li>
            </ul>
          </section>
          <section style="margin-top: 24px">
            <h2>{{ t("接入地址") }}</h2>
            <p class="sub">{{ t("MCP 和 HTTP API 只接受本机请求") }}</p>
            <div class="endpoints">
              <div v-for="[label, value, what] in endpoints" :key="value">
                <label>{{ label }}</label>
                <div class="copyline">
                  <code>{{ value }}</code
                  ><button
                    class="icon-btn"
                    :aria-label="t('复制') + what"
                    :title="t('复制') + what"
                    @click="copy('endpoint:' + value, value, ' ' + what)"
                  >
                    <Icon name="copy" />
                  </button>
                </div>
              </div>
            </div>
          </section>
        </div>
      </div>
    </div>
  </div>
</template>
