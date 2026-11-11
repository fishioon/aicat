<script setup lang="ts">
import { computed } from "vue";
import { shell, navigate } from "../state/desktop";
import { t } from "../i18n";
import Icon from "./Icon.vue";
const settings = computed(
  () => shell.route === "proxy" || shell.route === "general",
);
const matches = computed(() =>
  "流量 traffic http https network".includes(
    shell.extensionQuery.toLowerCase().trim(),
  ),
);
</script>
<template>
  <div id="module-sidebar">
    <template v-if="settings">
      <button id="settings-proxy" @click="navigate('proxy')">
        {{ t("代理") }}</button
      ><button id="settings-general" @click="navigate('general')">
        {{ t("通用") }}
      </button>
    </template>
    <template v-else>
      <input
        id="extension-search"
        v-model="shell.extensionQuery"
        :placeholder="t('搜索扩展…')"
      />
      <div>
        <details open>
          <summary>{{ t("已安装") }}</summary>
          <p>{{ t("暂无已安装的社区扩展") }}</p>
        </details>
        <details open>
          <summary>{{ t("内置") }}</summary>
          <button
            v-if="matches"
            id="extension-traffic"
            class="extension-item"
            @click="navigate('extensions')"
          >
            <span class="extension-symbol"><Icon name="traffic" /></span
            ><span
              ><strong>{{ t("流量") }}</strong
              ><small>{{ t("检查 HTTP 和 HTTPS 请求与响应") }}</small
              ><small>aicat · {{ t("内置") }}</small></span
            >
          </button>
        </details>
        <details open>
          <summary>{{ t("推荐") }}</summary>
          <button
            id="extension-community"
            class="extension-item"
            @click="navigate('community')"
          >
            <span class="extension-symbol"><Icon name="extensions" /></span
            ><span
              ><strong>{{ t("社区扩展") }}</strong
              ><small>{{ t("发现更多扩展能力") }}</small
              ><small>{{ t("即将推出") }}</small></span
            >
          </button>
        </details>
      </div>
    </template>
  </div>
</template>
