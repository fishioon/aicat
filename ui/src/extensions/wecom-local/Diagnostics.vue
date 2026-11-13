<script setup lang="ts">
import { computed } from "vue";
import { host } from "../../host";
import { run, state } from "../../state/desktop";
import { t } from "../../i18n";
const channel = computed(() => state.value?.messageChannels["wecom-local"]);
function status(value: string, outgoing = false) {
  const labels: Record<string, string> = {
    pending: "开始发送",
    sent: "已发送",
    unknown: "结果不明",
  };
  return value ? t(labels[value] ?? value) : t(outgoing ? "发出" : "收到");
}
function messageType(value: string) {
  const labels: Record<string, string> = {
    text: "文本",
    image: "图片",
    voice: "语音",
    video: "视频",
    file: "文件",
    unsupported: "不支持的消息",
  };
  return t(labels[value] ?? value);
}
function preview(value: string) {
  const text = value.replace(/\s+/g, " ").trim();
  return text.length > 80 ? text.slice(0, 80) + "…" : text;
}
</script>
<template>
  <div id="wecom-local-diagnostics">
    <h2>{{ t("本次运行的消息流水") }}</h2>
    <p>{{ t("收发记录保存在本机，重启后可从日志查看。") }}</p>
    <p>
      <button
        id="wecom-local-open-logs"
        class="btn"
        @click="run(() => host.openMessageLogs('wecom-local'))"
      >
        {{ t("打开 Room 日志") }}
      </button>
    </p>
    <table
      class="feat"
      id="wecom-local-messages"
      v-if="channel?.records.length"
    >
      <tbody>
        <tr v-for="record in [...channel.records].reverse()" :key="record.id">
          <th scope="row">{{ record.event.data.roomid }}</th>
          <td>
            <span class="tag">{{
              status(record.status ?? "", record.event.data.outgoing)
            }}</span>
            <span v-if="record.event.data.media_status === 'metadata_only'">{{
              t("{type}（仅元数据）", {
                type: messageType(record.event.data.msgtype),
              })
            }}</span>
            <details v-if="record.event.data.text.content">
              <summary>{{ preview(record.event.data.text.content) }}</summary>
              <pre class="body">{{ record.event.data.text.content }}</pre>
            </details>
          </td>
        </tr>
      </tbody>
    </table>
    <p v-else>{{ t("尚无消息。") }}</p>
  </div>
</template>
