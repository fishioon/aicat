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
        "原型只读，不发送消息或自动执行 Agent；媒体仅保存元数据，群聊与私聊类型尚未识别。",
      )
    }}
  </p>
</template>
