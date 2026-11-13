<script setup lang="ts">
import { t } from "../../i18n";
defineProps<{ tab: "detail" | "features" }>();
</script>
<template>
  <p>{{ t("媒体仅保存元数据，群聊与私聊类型尚未识别。") }}</p>
</template>
