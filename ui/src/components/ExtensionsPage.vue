<script setup lang="ts">
import { computed, reactive, ref } from "vue";
import {
  state,
  shell,
  navigate,
  run,
  popup,
  say,
  modal,
  closeDialog,
  proxyAddress,
} from "../state/desktop";
import { copy } from "../state/traffic";
import { host } from "../host";
import { t } from "../i18n";
import Icon from "./Icon.vue";

type ExtensionID = "traffic" | "codex";
interface Extension {
  id: ExtensionID;
  name: string;
  icon: string;
  summary: string;
  lede: string;
  categories: string[];
}
const extensions: Extension[] = [
  {
    id: "traffic",
    name: "流量",
    icon: "traffic",
    summary: "检查经过网关的 HTTP 和 HTTPS 请求与响应",
    lede: "检查经过网关的 HTTP 和 HTTPS 请求与响应。",
    categories: ["网络", "调试"],
  },
  {
    id: "codex",
    name: "Codex",
    icon: "codex",
    summary: "把已接入供应商的模型加入 Codex 的模型列表",
    lede: "把已接入供应商的模型加入 Codex 的模型列表，Codex 保留自己的 ChatGPT 登录。",
    categories: ["Agent", "模型"],
  },
];
const sections = reactive({ installed: true, recommended: true });
const tab = ref<"detail" | "features">("detail");
const enabledOf = (id: ExtensionID) =>
  id === "traffic"
    ? !!state.value?.trafficEnabled
    : !!state.value?.codexEnabled;
const current = computed(
  () => extensions.find((x) => x.id === shell.extension) ?? extensions[0],
);
const enabled = computed(() => enabledOf(current.value.id));
const matches = computed(() => {
  const query = shell.extensionQuery.trim().toLowerCase();
  return extensions.filter(
    (x) =>
      !query ||
      [t(x.name), x.id, "aicat", t(x.summary)]
        .join(" ")
        .toLowerCase()
        .includes(query),
  );
});
function choose(id: ExtensionID) {
  shell.extension = id;
  tab.value = "detail";
  navigate("extensions");
}
async function toggleTraffic() {
  const next = !enabledOf("traffic");
  if (await run(() => host.setTrafficEnabled(next)))
    say(t(next ? "已启用流量" : "已停用流量"));
}
async function enableCodex() {
  if (await run(() => host.setCodexEnabled(true))) say(t("已启用 Codex"));
}
// Codex's HTTPS to chatgpt.com is decrypted while the extension is on, which
// Codex accepts only once the root certificate is trusted: install it first.
async function toggleCodex() {
  if (enabledOf("codex")) {
    if (await run(() => host.setCodexEnabled(false))) say(t("已停用 Codex"));
    return;
  }
  if (state.value?.certificate.trusted) return enableCodex();
  modal.value = {
    title: "需要先安装证书",
    message: t(
      "启用后，aicat 会解密 Codex 到 chatgpt.com 的 HTTPS 连接，需要先安装并信任本机根证书。安装后自动启用这个扩展。",
    ),
    actions: [
      { label: "取消", run: closeDialog },
      {
        label: "安装证书",
        run: async () => {
          closeDialog();
          if (await run(() => host.installCertificate())) await enableCodex();
        },
      },
    ],
  };
}
function toggle() {
  return current.value.id === "traffic" ? toggleTraffic() : toggleCodex();
}
function gear(event: MouseEvent) {
  const id = current.value.id;
  popup(
    event.currentTarget as Element,
    id === "traffic"
      ? [
          {
            label: "打开流量",
            id: "extension-open-traffic",
            disabled: !enabled.value,
            run: () => navigate("traffic"),
          },
          {
            label: "代理设置",
            id: "extension-proxy-settings",
            run: () => navigate("proxy"),
          },
          {
            label: "复制扩展标识符",
            run: () => copy("extension-id", "traffic", t("扩展标识符")),
          },
        ]
      : [
          {
            label: "接入供应商",
            id: "extension-codex-providers",
            run: () => navigate("provider"),
          },
          {
            label: "代理设置",
            id: "extension-proxy-settings",
            run: () => navigate("proxy"),
          },
          {
            label: "复制扩展标识符",
            run: () => copy("extension-id", "codex", t("扩展标识符")),
          },
        ],
  );
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
// The providers' models Codex is offered, as the gateway lists them.
const codexModels = computed(
  () =>
    state.value?.providers
      .filter((p) => p.responses)
      .reduce(
        (n, p) =>
          n +
          p.models.models.filter(
            (m) => !m.apis?.length || m.apis.includes("responses"),
          ).length,
        0,
      ) ?? 0,
);
const baseURL = computed(
  () => `openai_base_url = "http://${proxyAddress()}/backend-api/codex"`,
);
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
                :class="{ off: !enabledOf(x.id) }"
                :aria-current="current.id === x.id ? 'true' : undefined"
                @click="choose(x.id)"
              >
                <span class="xicon"><Icon :name="x.icon" /></span
                ><span class="top"
                  ><strong
                    >{{ t(x.name) }}
                    <span class="tag">{{ t("内置") }}</span></strong
                  ><span class="st">{{
                    t(enabledOf(x.id) ? "已启用" : "已停用")
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
      <div class="xd extension-detail" :data-extension="current.id">
        <header class="xd-head">
          <span class="xicon extension-emblem"
            ><Icon :name="current.icon"
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
          <p v-if="!enabled" class="xd-status">
            <Icon name="info" />{{
              t(
                current.id === "traffic"
                  ? "这个扩展已停用，不采集新请求，导航栏里不显示“流量”。"
                  : "这个扩展已停用，Codex 的请求原样发往 ChatGPT。",
              )
            }}
          </p>
          <p
            v-else-if="current.id === 'codex'"
            class="xd-status"
            id="codex-status"
          >
            <Icon name="info" />{{
              t(
                "向 Codex 提供 {n} 个供应商模型。已在运行的 Codex 需要重新启动。",
                {
                  n: codexModels,
                },
              )
            }}
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
            <template v-if="current.id === 'traffic'">
              <template v-if="tab === 'detail'">
                <h2>{{ t("这个扩展做什么") }}</h2>
                <p>
                  {{
                    t(
                      "记录经过 aicat 代理的 HTTP 和 HTTPS 请求。可以按域名、应用和路径筛选，查看请求与响应的 Header、Body 和 SSE 事件。",
                    )
                  }}
                </p>
                <p>
                  {{
                    t("HTTPS 只解密“解密域名”里列出的域名，其余保持加密隧道。")
                  }}
                </p>
                <h2>{{ t("停用之后") }}</h2>
                <p>
                  {{
                    t(
                      "停止采集新请求，并从导航栏移除“流量”。代理继续转发，已有记录按容量上限保留。重新启用后，“流量”回到导航栏原来的位置。",
                    )
                  }}
                </p>
              </template>
              <table v-else class="feat">
                <tbody>
                  <tr>
                    <th scope="row">{{ t("导航栏") }}</th>
                    <td>
                      {{ t("“流量”入口，可以在“更多”里固定或取消固定") }}
                    </td>
                  </tr>
                  <tr>
                    <th scope="row">{{ t("托盘菜单") }}</th>
                    <td>{{ t("暂停采集、继续采集") }}</td>
                  </tr>
                  <tr>
                    <th scope="row">{{ t("设置") }}</th>
                    <td>
                      {{ t("监听地址、根证书、系统代理、解密域名，都在") }}
                      <button class="link" @click="navigate('proxy')">
                        {{ t("代理设置") }}
                      </button>
                      {{ t("里") }}
                    </td>
                  </tr>
                </tbody>
              </table>
            </template>
            <template v-else>
              <template v-if="tab === 'detail'">
                <h2>{{ t("这个扩展做什么") }}</h2>
                <p>
                  {{
                    t(
                      "Codex 读取模型列表时，aicat 在 ChatGPT 自带的模型之后加入供应商的模型，名称写作“供应商/模型”，例如 openai/gpt-5。Codex 保留自己的 ChatGPT 登录。",
                    )
                  }}
                </p>
                <p>
                  {{
                    t(
                      "选择供应商的模型时，请求带上 aicat 保管的密钥发给供应商，不带 ChatGPT 的登录凭据；选择 ChatGPT 自带的模型时，请求原样发往 ChatGPT。",
                    )
                  }}
                </p>
                <p>
                  {{
                    t(
                      "只提供支持 Responses 接口的供应商，即 OpenAI 和兼容 OpenAI 的服务。模型名称、上下文窗口和推理强度先取供应商返回的信息，缺少的部分参考 models.dev。",
                    )
                  }}
                </p>
                <h2>{{ t("让 Codex 经过 aicat") }}</h2>
                <p>
                  {{
                    t(
                      "开启代理设置里的系统代理后，aicat 解密 Codex 到 chatgpt.com 的连接，Codex 不用改配置。也可以不开系统代理，在 ~/.codex/config.toml 里加入下面这一行，让 Codex 直接连接 aicat：",
                    )
                  }}
                </p>
                <div class="snippet">
                  <header>
                    <span>~/.codex/config.toml</span
                    ><button
                      id="codex-copy"
                      class="btn"
                      @click="copy('codex-snippet', baseURL, t('配置'))"
                    >
                      <Icon name="copy" />{{ t("复制") }}
                    </button>
                  </header>
                  <pre id="codex-snippet">{{ baseURL }}</pre>
                </div>
                <h2>{{ t("限制") }}</h2>
                <p>
                  {{
                    t(
                      "供应商模型的对话暂不支持压缩，接近上下文上限时请新开对话。启用期间 Codex 改用 HTTP 流式请求，不使用 WebSocket。",
                    )
                  }}
                </p>
                <h2>{{ t("停用之后") }}</h2>
                <p>
                  {{
                    t(
                      "不再解密 Codex 的连接，模型列表恢复为 ChatGPT 自带的模型。Codex 已缓存的列表可能仍有供应商模型，选择会出错，重新打开 Codex 即可。",
                    )
                  }}
                </p>
              </template>
              <table v-else class="feat">
                <tbody>
                  <tr>
                    <th scope="row">{{ t("模型列表") }}</th>
                    <td>
                      {{ t("供应商与模型列表在") }}
                      <button class="link" @click="navigate('provider')">
                        {{ t("接入供应商") }}
                      </button>
                      {{ t("里，保存后自动同步") }}
                    </td>
                  </tr>
                  <tr>
                    <th scope="row">{{ t("接入") }}</th>
                    <td>
                      {{
                        t(
                          "经系统代理解密 chatgpt.com，或在 Codex 中把 openai_base_url 指向 aicat",
                        )
                      }}
                    </td>
                  </tr>
                  <tr>
                    <th scope="row">{{ t("设置") }}</th>
                    <td>
                      {{ t("根证书与系统代理在") }}
                      <button class="link" @click="navigate('proxy')">
                        {{ t("代理设置") }}
                      </button>
                      {{ t("里") }}
                    </td>
                  </tr>
                </tbody>
              </table>
            </template>
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
