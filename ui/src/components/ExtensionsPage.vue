<script setup lang="ts">
import { state, shell, navigate, run } from "../state/desktop";
import { host } from "../host";
import { t } from "../i18n";
import Icon from "./Icon.vue";
</script>
<template>
  <section class="card extension-detail" v-if="state">
    <span class="extension-emblem"
      ><Icon :name="shell.route === 'community' ? 'extensions' : 'traffic'"
    /></span>
    <h3>{{ t(shell.route === "community" ? "社区扩展" : "流量") }}</h3>
    <template v-if="shell.route === 'community'"
      ><p>{{ t("即将推出") }}</p>
      <p>{{ t("发现更多扩展能力") }}</p></template
    >
    <template v-else>
      <p>aicat · {{ t("内置扩展") }}</p>
      <p>{{ t("检查 HTTP 和 HTTPS 请求与响应") }}</p>
      <button
        id="feature-traffic"
        class="feature-switch"
        role="switch"
        :aria-checked="state.trafficEnabled"
        :aria-label="t('启用流量')"
        @click="run(() => host.setTrafficEnabled(!state!.trafficEnabled))"
      >
        {{ t(state.trafficEnabled ? "关闭" : "启用") }}
      </button>
      <button
        id="extension-open-traffic"
        :disabled="!state.trafficEnabled"
        @click="navigate('traffic')"
      >
        {{ t("打开流量") }}
      </button>
      <button id="extension-proxy-settings" @click="navigate('proxy')">
        {{ t("代理设置") }}
      </button>
      <p>
        {{
          t(
            "关闭后隐藏流量入口并停止采集新请求，代理继续转发，已有记录按容量限制保留。",
          )
        }}
      </p>
    </template>
  </section>
</template>
