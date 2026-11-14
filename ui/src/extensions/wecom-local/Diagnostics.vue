<script setup lang="ts">
import { computed } from "vue";
import { host } from "../../host";
import { run, state } from "../../state/desktop";
import { t } from "../../i18n";
const channel = computed(() => state.value?.messageChannels["wecom-local"]);
const reasons: Record<string, string> = {
  direct: "私聊新文本",
  mention: "群聊 @ 当前账号",
  mention_all: "群聊 @所有人",
  keyword: "命中关键词",
  all: "所有新文本（测试）",
};
</script>
<template>
  <div id="wecom-local-diagnostics">
    <h2>{{ t("本次运行的消息") }}</h2>
    <p>{{ t("Room 日志保存成功后才推进本地读取游标，重启后继续读取。") }}</p>
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
            {{ record.event.data.text.content }}
            <span class="tag">{{ record.event.data.msgtype }}</span>
          </td>
        </tr>
      </tbody>
    </table>
    <p v-else>{{ t("尚无消息。") }}</p>
    <table class="feat" id="wecom-local-tasks" v-if="channel?.tasks.length">
      <tbody>
        <tr v-for="task in [...channel.tasks].reverse()" :key="task.id">
          <th scope="row">{{ task.room }}</th>
          <td>
            {{ task.status }}<span v-if="task.error"> · {{ task.error }}</span>
            <span v-if="task.triggerReason" class="tag">{{
              t(reasons[task.triggerReason] ?? task.triggerReason)
            }}</span>
          </td>
        </tr>
      </tbody>
    </table>
  </div>
</template>
