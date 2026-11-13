<script setup lang="ts">
import { computed, ref } from "vue";
import { state, navigate, proxyAddress } from "../state/desktop";
import { copy } from "../state/traffic";
import { t } from "../i18n";
import Icon from "./Icon.vue";
import Seg from "./Seg.vue";

type Client = "claude" | "codex" | "cursor" | "other";
const client = ref<Client>("claude");
const tokenPath = computed(() => state.value!.mcpTokenPath);
const readToken = computed(
  () => "cat '" + tokenPath.value.replace(/'/g, "'\\''") + "'",
);
const tokenPlaceholder = computed(() => t("<令牌文件的内容>"));
const tokenCommand = computed(() =>
  client.value === "codex"
    ? `export AICAT_MCP_TOKEN="$(${readToken.value})"`
    : readToken.value,
);
const mcp = computed(() => `http://${proxyAddress()}/mcp`);
const clients = computed(() => ({
  claude: {
    name: "Claude Code",
    where: t("在终端运行"),
    code: `claude mcp add --transport http aicat ${mcp.value} \\\n  --header "Authorization: Bearer $(${readToken.value})"`,
  },
  codex: {
    name: "Codex",
    where: t("加入 ~/.codex/config.toml"),
    code: `[mcp_servers.aicat]\nurl = "${mcp.value}"\nbearer_token_env_var = "AICAT_MCP_TOKEN"`,
  },
  cursor: {
    name: "Cursor",
    where: t("加入 ~/.cursor/mcp.json"),
    code: JSON.stringify(
      {
        mcpServers: {
          aicat: {
            url: mcp.value,
            headers: {
              Authorization: "Bearer " + tokenPlaceholder.value,
            },
          },
        },
      },
      null,
      2,
    ),
  },
  other: {
    name: t("其他"),
    where: "Streamable HTTP",
    code: `${t("地址")}  ${mcp.value}\n${t("请求头")}  Authorization: Bearer ${tokenPlaceholder.value}`,
  },
}));
const options = computed(() =>
  (Object.keys(clients.value) as Client[]).map(
    (id) => [id, clients.value[id].name] as const,
  ),
);
const current = computed(() => clients.value[client.value]);
const base = computed(() => "http://" + proxyAddress());
const endpoints = computed(() => [
  ["MCP", base.value + "/mcp", t("MCP 地址")],
  [t("模型入口"), base.value + "/v1", t("模型入口地址")],
  [t("HTTP 代理"), base.value, t("代理地址")],
  ["HTTP API", base.value + "/api", t("API 地址")],
]);
</script>
<template>
  <div class="main" id="connect">
    <div class="ov">
      <button class="btn back" id="connect-back" @click="navigate('overview')">
        <Icon name="back" />{{ t("返回主页") }}
      </button>
      <div class="ov-head">
        <div>
          <h1>{{ t("接入 Agent") }}</h1>
          <p>
            {{ t("通过 MCP 让 Agent 查询流量和发送消息。") }}
          </p>
        </div>
      </div>
      <ol class="steps">
        <li class="step now">
          <span class="n num">1</span>
          <div>
            <h2>{{ t("把 aicat 加入 Agent") }}</h2>
            <Seg
              v-model="client"
              :options="options"
              label="Agent 客户端"
              id-prefix="client-"
            />
            <div class="snippet">
              <header>
                <span>{{ current.where }}</span
                ><button
                  id="copy-snippet"
                  class="btn"
                  @click="copy('snippet', current.code, t('配置'))"
                >
                  <Icon name="copy" />{{ t("复制配置") }}
                </button>
              </header>
              <pre id="snippet">{{ current.code }}</pre>
            </div>
            <div v-if="client !== 'claude'" class="snippet">
              <header>
                <span>{{
                  t(
                    client === "codex"
                      ? "启动 Codex 前，在同一终端运行"
                      : "在终端读取令牌，替换配置中的占位内容",
                  )
                }}</span>
                <button
                  id="copy-token-command"
                  class="btn"
                  @click="copy('token-command', tokenCommand, t('命令'))"
                >
                  <Icon name="copy" />{{ t("复制命令") }}
                </button>
              </header>
              <pre id="token-command">{{ tokenCommand }}</pre>
            </div>
          </div>
        </li>
        <li class="step">
          <span class="n num">2</span>
          <div>
            <h2>{{ t("在 Agent 里试一下") }}</h2>
            <p>
              {{
                t(
                  "重启 Agent 后，让它调用 aicat 的工具，例如“列出 aicat 最近记录的请求”。",
                )
              }}
            </p>
          </div>
        </li>
      </ol>
      <section class="connect-endpoints">
        <h2>{{ t("接入地址") }}</h2>
        <p class="sub">
          {{ t("MCP、模型入口和 HTTP API 仅供本机访问。") }}
        </p>
        <dl class="ov-endpoints" id="connect-endpoints">
          <template v-for="[label, value, what] in endpoints" :key="value">
            <dt>{{ label }}</dt>
            <dd>
              <code>{{ value }}</code
              ><button
                class="icon-btn"
                :aria-label="t('复制') + what"
                :title="t('复制') + what"
                @click="copy('endpoint:' + value, value, ' ' + what)"
              >
                <Icon name="copy" />
              </button>
            </dd>
          </template>
        </dl>
      </section>
    </div>
  </div>
</template>
