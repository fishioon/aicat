<script setup lang="ts">
import { t } from "../i18n";
defineProps<{ tab: "detail" | "features" }>();
</script>
<template>
  <template v-if="tab === 'detail'">
    <h2>{{ t("这个扩展做什么") }}</h2>
    <p>
      {{
        t(
          "连接企业微信长连接智能机器人，接收私聊文本并保存到 Room 日志，可交给 Codex 自动处理。",
        )
      }}
    </p>
    <h2>{{ t("开始使用") }}</h2>
    <p>
      {{
        t(
          "先安装官方插件，再点击“配置”填写 Bot ID 和 Secret；鉴权验证通过后，启用扩展。可单独选择 Codex 自动处理。",
        )
      }}
    </p>
    <h2>{{ t("停用之后") }}</h2>
    <p>{{ t("断开机器人连接并停止自动处理，账号凭据和 Room 日志保留。") }}</p>
  </template>
  <table v-else class="feat">
    <tbody>
      <tr>
        <th scope="row">{{ t("接入") }}</th>
        <td>
          {{ t("官方 OpenClaw 插件，应用内置 Bun 宿主，单个机器人账号") }}
        </td>
      </tr>
      <tr>
        <th scope="row">{{ t("消息") }}</th>
        <td>{{ t("私聊文本接收、Room 日志、手动发送和 messages_send") }}</td>
      </tr>
      <tr>
        <th scope="row">{{ t("凭据") }}</th>
        <td>{{ t("Secret 仅保存到本机私有文件，不回传界面") }}</td>
      </tr>
    </tbody>
  </table>
</template>
