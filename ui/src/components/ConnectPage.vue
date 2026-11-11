<script setup lang="ts">
import { computed, ref } from "vue";
import { navigate, proxyAddress } from "../state/desktop";
import { copy } from "../state/traffic";
import { t } from "../i18n";
import Icon from "./Icon.vue";
import Seg from "./Seg.vue";

type Client = "claude" | "codex" | "cursor" | "other";
const client = ref<Client>("claude");
const mcp = computed(() => `http://${proxyAddress()}/mcp`);
const clients = computed(() => ({
  claude: {
    name: "Claude Code",
    where: t("在终端运行"),
    code: `claude mcp add --transport http aicat ${mcp.value} \\\n  --header "Authorization: Bearer $(cat ~/.aicat/mcp-token)"`,
  },
  codex: {
    name: "Codex",
    where: t("加入 ~/.codex/config.toml，并把令牌放进环境变量 AICAT_MCP_TOKEN"),
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
              Authorization: "Bearer " + t("<~/.aicat/mcp-token 的内容>"),
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
    code: `${t("地址")}  ${mcp.value}\n${t("请求头")}  Authorization: Bearer ${t("<~/.aicat/mcp-token 的内容>")}`,
  },
}));
const options = computed(() =>
  (Object.keys(clients.value) as Client[]).map(
    (id) => [id, clients.value[id].name] as const,
  ),
);
const current = computed(() => clients.value[client.value]);
</script>
<template>
  <div class="main" id="connect">
    <div class="ov">
      <button class="btn back" id="connect-back" @click="navigate('overview')">
        <Icon name="back" />{{ t("返回概览") }}
      </button>
      <div class="ov-head">
        <div>
          <h1>{{ t("接入 Agent") }}</h1>
          <p>
            {{ t("aicat 的 MCP 和网关共用同一个监听地址，只接受本机请求。") }}
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
            <p>
              {{
                t(
                  "令牌保存在 ~/.aicat/mcp-token，只用于本机访问，不要提交到代码仓库。",
                )
              }}
            </p>
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
    </div>
  </div>
</template>
