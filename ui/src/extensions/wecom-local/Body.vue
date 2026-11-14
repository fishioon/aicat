<script setup lang="ts">
import { t } from "../../i18n";
defineProps<{ tab: "detail" | "features" }>();
</script>
<template>
  <p>
    {{ t("使用已有密钥读取企业微信客户端本地数据库，按会话增量保存到 Room。") }}
  </p>
  <p>
    {{
      t(
        "支持按会话配置私聊、群聊提及与关键词触发，Agent 可通过客户端发送文本；媒体仅保存元数据。",
      )
    }}
  </p>
</template>
