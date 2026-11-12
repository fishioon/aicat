<script setup lang="ts">
import { navigate } from "../../state/desktop";
import { t } from "../../i18n";

defineProps<{ tab: "detail" | "features" }>();
</script>
<template>
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
      {{ t("HTTPS 只解密“解密域名”里列出的域名，其余保持加密隧道。") }}
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
