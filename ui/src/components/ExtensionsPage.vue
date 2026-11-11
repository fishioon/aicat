<script setup lang="ts">
import { computed, reactive, ref } from "vue";
import { state, shell, navigate, run, popup, say } from "../state/desktop";
import { copy } from "../state/traffic";
import { host } from "../host";
import { t } from "../i18n";
import Icon from "./Icon.vue";

const sections = reactive({ installed: true, recommended: true });
const tab = ref<"detail" | "features">("detail");
const enabled = computed(() => !!state.value?.trafficEnabled);
const matches = computed(() => {
  const query = shell.extensionQuery.trim().toLowerCase();
  return (
    !query ||
    [
      t("流量"),
      "traffic",
      "aicat",
      t("检查经过网关的 HTTP 和 HTTPS 请求与响应"),
    ]
      .join(" ")
      .toLowerCase()
      .includes(query)
  );
});
async function toggle() {
  const next = !enabled.value;
  if (await run(() => host.setTrafficEnabled(next)))
    say(t(next ? "已启用流量" : "已停用流量"));
}
function gear(event: MouseEvent) {
  popup(event.currentTarget as Element, [
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
          ><span class="badge num">{{ matches ? 1 : 0 }}</span>
        </button>
        <template v-if="sections.installed">
          <ul v-if="matches">
            <li>
              <button
                id="extension-traffic"
                class="xitem"
                :class="{ off: !enabled }"
                aria-current="true"
                @click="navigate('extensions')"
              >
                <span class="xicon"><Icon name="traffic" /></span
                ><span class="top"
                  ><strong
                    >{{ t("流量") }}
                    <span class="tag">{{ t("内置") }}</span></strong
                  ><span class="st">{{
                    t(enabled ? "已启用" : "已停用")
                  }}</span></span
                ><span class="desc">{{
                  t("检查经过网关的 HTTP 和 HTTPS 请求与响应")
                }}</span>
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
      <div class="xd extension-detail">
        <header class="xd-head">
          <span class="xicon extension-emblem"><Icon name="traffic" /></span>
          <h1>
            {{ t("流量") }}<span class="tag">{{ t("内置") }}</span>
          </h1>
          <p class="xd-meta">
            <span>aicat</span><span>{{ t("随 aicat 发布") }}</span>
          </p>
          <p class="lede">
            {{ t("检查经过网关的 HTTP 和 HTTPS 请求与响应。") }}
          </p>
          <div class="xd-acts">
            <button
              id="feature-traffic"
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
              t("这个扩展已停用，不采集新请求，导航栏里不显示“流量”。")
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
          </div>
          <aside class="xd-side" :aria-label="t('扩展信息')">
            <h2>{{ t("信息") }}</h2>
            <dl>
              <dt>{{ t("标识符") }}</dt>
              <dd><code>traffic</code></dd>
              <dt>{{ t("发布者") }}</dt>
              <dd>aicat</dd>
              <dt>{{ t("来源") }}</dt>
              <dd>{{ t("内置") }}</dd>
            </dl>
            <h2>{{ t("分类") }}</h2>
            <div class="tags">
              <span class="tag">{{ t("网络") }}</span
              ><span class="tag">{{ t("调试") }}</span>
            </div>
          </aside>
        </div>
      </div>
    </section>
  </div>
</template>
