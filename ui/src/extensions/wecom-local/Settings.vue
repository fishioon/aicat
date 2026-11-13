<script setup lang="ts">
import { computed, reactive, ref, watch } from "vue";
import { host } from "../../host";
import { busy, run, say, state } from "../../state/desktop";
import { t } from "../../i18n";
const channel = computed(() => state.value?.messageChannels["wecom-local"]);
const enabled = computed(
  () => !!state.value?.extensions.find((x) => x.id === "wecom-local")?.enabled,
);
const local = reactive({ accountId: "", dataDir: "" });
const key = ref("");
function directoryIdentity(value: string) {
  const parts: string[] = [];
  for (const part of value.split("/")) {
    if (part === "..") parts.pop();
    else if (part && part !== ".") parts.push(part);
  }
  return (value.startsWith("/") ? "/" : "") + parts.join("/");
}
const requiresKey = computed(
  () =>
    !channel.value?.configured ||
    local.accountId !== channel.value?.local?.accountId ||
    directoryIdentity(local.dataDir) !==
      directoryIdentity(channel.value?.local?.dataDir ?? ""),
);
watch(
  () => JSON.stringify(channel.value?.local),
  () =>
    Object.assign(
      local,
      channel.value?.local ?? { accountId: "", dataDir: "" },
    ),
  { immediate: true },
);
async function save() {
  if (
    await run(() =>
      host.configureMessageChannel("wecom-local", {
        accountId: local.accountId,
        secret: key.value,
        local: { ...local },
      }),
    )
  ) {
    key.value = "";
    say(t("本地数据库验证通过，可以启用"));
  }
}
</script>
<template>
  <div id="wecom-local-settings">
    <h2>{{ t("本地数据库") }}</h2>
    <p>
      {{ t("填写 message.db 所在目录和 Base64 原始密钥；密钥只保存在本机。") }}
    </p>
    <p>
      {{
        t("MCP 发送需要辅助功能权限及唯一会话名称，会操作企业微信和剪贴板。")
      }}
    </p>
    <form class="form" id="wecom-local-config" @submit.prevent="save">
      <label for="wecom-local-account">{{ t("本地账号 ID") }}</label>
      <input
        id="wecom-local-account"
        class="field"
        v-model="local.accountId"
        :disabled="enabled || busy"
        autocomplete="off"
        required
      />
      <label for="wecom-local-dir">{{ t("数据库目录") }}</label>
      <input
        id="wecom-local-dir"
        class="field"
        v-model="local.dataDir"
        :disabled="enabled || busy"
        autocomplete="off"
        required
      />
      <label for="wecom-local-key">{{ t("Base64 原始密钥") }}</label>
      <input
        id="wecom-local-key"
        class="field"
        v-model="key"
        type="password"
        :placeholder="!requiresKey ? t('留空保留已保存的密钥') : ''"
        :disabled="enabled || busy"
        autocomplete="new-password"
        :required="requiresKey"
      />
      <div class="acts">
        <button
          id="wecom-local-save"
          class="btn"
          :disabled="
            enabled ||
            busy ||
            !local.accountId.trim() ||
            !local.dataDir.trim() ||
            (requiresKey && !key.trim())
          "
        >
          {{ t("保存并验证") }}
        </button>
      </div>
    </form>
    <p v-if="enabled">
      {{ t("修改前请先停用，账号和消息保留。") }}
    </p>
  </div>
</template>
