<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { host } from "../host";
import { busy, run, say, shell, state } from "../state/desktop";
import { t } from "../i18n";

const id = computed(() => shell.extension);
const channel = computed(() => state.value?.messageChannels[id.value]);
const enabled = computed(
  () => !!state.value?.extensions.find((x) => x.id === id.value)?.enabled,
);
const accountId = ref("");
const secret = ref("");
const agentEnabled = ref(false);
const agentBinary = ref("");
watch(
  [
    () => id.value,
    () => channel.value?.agent.enabled,
    () => channel.value?.agent.binary,
  ],
  () => {
    agentEnabled.value = channel.value?.agent.enabled ?? false;
    agentBinary.value = channel.value?.agent.binary ?? "";
  },
  { immediate: true },
);
async function saveAgent() {
  if (
    await run(() =>
      host.configureMessageAgent(id.value, {
        enabled: agentEnabled.value,
        binary: agentBinary.value,
      }),
    )
  )
    say(t("Codex 配置已保存"));
}
watch(
  () => channel.value?.accountId,
  (id) => {
    accountId.value = id ?? "";
  },
  { immediate: true },
);
async function save() {
  if (
    await run(() =>
      host.configureMessageChannel(id.value, {
        accountId: accountId.value,
        secret: secret.value,
      }),
    )
  ) {
    secret.value = "";
    say(t("鉴权验证通过，可以启用插件"));
  }
}
</script>
<template>
  <div id="wecom-settings">
    <h2>{{ t("配置机器人") }}</h2>
    <p>
      {{
        t(
          "在企业微信创建使用长连接的智能机器人，将 Bot ID 和 Secret 填入下方。保存并通过鉴权验证后，回到扩展详情启用。",
        )
      }}
    </p>
    <form class="form" id="wecom-config" @submit.prevent="save">
      <label for="wecom-bot-id">Bot ID</label>
      <input
        id="wecom-bot-id"
        class="field"
        v-model="accountId"
        :disabled="enabled || busy"
        autocomplete="off"
        required
      />
      <label for="wecom-secret">Secret</label>
      <input
        id="wecom-secret"
        class="field"
        v-model="secret"
        :disabled="enabled || busy"
        type="password"
        autocomplete="new-password"
        :placeholder="channel?.configured ? t('留空保留已保存的 Secret') : ''"
      />
      <div class="acts">
        <button
          id="wecom-save"
          class="btn"
          :disabled="enabled || busy || !accountId.trim()"
        >
          {{ t("保存并验证账号") }}
        </button>
      </div>
    </form>
    <p v-if="enabled">
      {{ t("修改账号前请先停用扩展。停用保留账号和消息日志。") }}
    </p>
    <h2>Codex</h2>
    <form class="form" id="wecom-agent-form" @submit.prevent="saveAgent">
      <label
        ><input
          id="wecom-agent-enabled"
          type="checkbox"
          v-model="agentEnabled"
          :disabled="enabled || busy"
        />
        {{ t("自动处理私聊消息") }}</label
      >
      <p>
        {{
          t(
            "开启后，新收到的私聊文本交给 Codex。执行中的新消息会补充当前任务，回复自动发送到此会话。修改前请先停用扩展。",
          )
        }}
      </p>
      <label for="wecom-agent-binary">{{ t("Codex 可执行文件") }}</label>
      <input
        id="wecom-agent-binary"
        class="field"
        v-model="agentBinary"
        :disabled="enabled || busy"
        :placeholder="t('留空自动查找已安装的 Codex')"
      />
      <div class="acts">
        <button id="wecom-agent-save" class="btn" :disabled="enabled || busy">
          {{ t("保存 Codex 配置") }}
        </button>
      </div>
    </form>
  </div>
</template>
