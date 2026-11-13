<script setup lang="ts">
import { computed, ref } from "vue";
import { host } from "../host";
import { busy, run, shell, state } from "../state/desktop";
import { t } from "../i18n";
const id = computed(() => shell.extension);
const channel = computed(() => state.value?.messageChannels[id.value]);
const enabled = computed(
  () => !!state.value?.extensions.find((x) => x.id === id.value)?.enabled,
);
const recipient = ref("");
const text = ref("");
const request = ref<{ id: string; room: string; text: string }>();
const result = ref("");
function taskStatus(status: string) {
  const labels: Record<string, string> = {
    pending: "待处理",
    running: "处理中",
    completed: "已完成",
    failed: "执行失败",
    unknown: "结果不明",
  };
  return t(labels[status] ?? status);
}
async function send() {
  const room = recipient.value.trim();
  if (
    !request.value ||
    request.value.room !== room ||
    request.value.text !== text.value
  ) {
    request.value = { id: crypto.randomUUID(), room, text: text.value };
  }
  const pending = request.value;
  await run(async () => {
    const receipt = await host.sendMessage(id.value, {
      specversion: "1.0",
      id: pending.id,
      source: "aicat-ui://desktop",
      type: "org.aicat.message.send.requested.v1",
      datacontenttype: "application/json",
      data: {
        roomid: pending.room,
        action: "send",
        msgtype: "text",
        text: { content: pending.text },
      },
    });
    result.value =
      receipt.status === "sent"
        ? t("平台已确认发送")
        : t("发送结果不明，请勿重新发送");
    if (receipt.status === "sent") {
      text.value = "";
      request.value = undefined;
    }
  });
}
</script>
<template>
  <div id="wecom-diagnostics">
    <h2 v-if="channel?.tasks.length">{{ t("Agent 任务") }}</h2>
    <table class="feat" id="wecom-agent-tasks" v-if="channel?.tasks.length">
      <tbody>
        <tr v-for="task in [...channel.tasks].reverse()" :key="task.id">
          <th scope="row">{{ task.room }}</th>
          <td>
            <span class="tag">{{ taskStatus(task.status) }}</span>
            {{ task.error }}
          </td>
        </tr>
      </tbody>
    </table>
    <h2>{{ t("测试发送") }}</h2>
    <p>{{ t("此操作会向指定收件人发送真实消息。") }}</p>
    <form class="form" id="wecom-send-form" @submit.prevent="send">
      <label for="wecom-recipient">{{ t("收件人 User ID") }}</label>
      <input
        id="wecom-recipient"
        class="field"
        v-model="recipient"
        autocomplete="off"
        required
      />
      <label for="wecom-text">{{ t("文本") }}</label>
      <textarea
        id="wecom-text"
        class="field"
        v-model="text"
        rows="3"
        maxlength="1000"
        required
      />
      <div class="acts">
        <button
          id="wecom-send"
          class="btn"
          :disabled="
            busy ||
            !enabled ||
            channel?.phase !== 'connected' ||
            !recipient.trim() ||
            !text.trim()
          "
        >
          {{ t("发送") }}
        </button>
      </div>
    </form>
    <p id="wecom-send-result" role="status">{{ result }}</p>
    <h2>{{ t("本次运行的消息") }}</h2>
    <p>
      {{
        t(
          "收到的消息和发送结果写入本地 Room 流水日志。可手动发送、通过 messages_send 调用，或开启 Codex 自动处理私聊。",
        )
      }}
    </p>
    <p>
      <button
        id="wecom-open-logs"
        class="btn"
        @click="run(() => host.openMessageLogs(id))"
      >
        {{ t("打开 Room 日志") }}
      </button>
    </p>
    <table class="feat" id="wecom-messages" v-if="channel?.records.length">
      <tbody>
        <tr v-for="record in [...channel.records].reverse()" :key="record.id">
          <th scope="row">{{ record.event.data.roomid }}</th>
          <td>
            {{ record.event.data.text.content }}
            <span class="tag">{{
              record.status ? record.status : t("收到")
            }}</span>
          </td>
        </tr>
      </tbody>
    </table>
    <p v-else>{{ t("尚无消息。") }}</p>
  </div>
</template>
