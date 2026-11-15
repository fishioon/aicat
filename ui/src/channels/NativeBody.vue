<script setup lang="ts">
import { computed } from "vue";
import { shell, state } from "../state/desktop";
import { t } from "../i18n";
const extension = computed(() =>
  state.value?.extensions.find((x) => x.id === shell.extension),
);
</script>
<template>
  <p v-if="extension?.enabled && extension.needsTrust">
    {{ t("启用后，请重新建立平台连接或重新加载页面。") }}
  </p>
</template>
