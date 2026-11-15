<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { host } from "../host";
import { busy, run, say, shell, state, navigate } from "../state/desktop";
import { t } from "../i18n";

const id = computed(() => shell.extension);
const channel = computed(() => state.value?.messageChannels[id.value]);
const enabled = computed(
  () => !!state.value?.extensions.find((x) => x.id === id.value)?.enabled,
);
const accountId = ref("");
const secret = ref("");
const requiresSecret = computed(
  () =>
    !channel.value?.configured ||
    accountId.value.trim() !== channel.value?.accountId,
);
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
          "在企业微信创建使用长连接的智能机器人，将 Bot ID 和 Secret 填入下方。保存并通过鉴权验证后，在本页启用扩展。",
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
        :placeholder="!requiresSecret ? t('留空保留已保存的 Secret') : ''"
        :required="requiresSecret"
      />
      <div class="acts wide">
        <button
          id="wecom-save"
          class="btn"
          :disabled="
            enabled ||
            busy ||
            !accountId.trim() ||
            (requiresSecret && !secret.trim())
          "
        >
          {{ t("保存并验证账号") }}
        </button>
      </div>
    </form>
    <p v-if="enabled">
      {{ t("修改账号前请先停用扩展。停用保留账号和消息日志。") }}
    </p>
    <p>{{ t("消息处理、model 和 Room prompt 在对应会话中设置。") }}</p>
    <button class="btn" type="button" @click="navigate('messages')">
      {{ t("打开消息") }}
    </button>
  </div>
</template>
