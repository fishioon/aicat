<script setup lang="ts">
import { computed } from "vue";
import { host } from "../../host";
import { run, state } from "../../state/desktop";
import { t } from "../../i18n";
const channel = computed(() => state.value?.messageChannels["wecom-local"]);
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
  </div>
</template>
