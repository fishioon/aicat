<script setup lang="ts">
import { computed, reactive, watch } from "vue";
import { host } from "../../host";
import { busy, run, say, state } from "../../state/desktop";
import { t } from "../../i18n";
const channel = computed(() => state.value?.messageChannels["wecom-local"]);
const enabled = computed(
  () => !!state.value?.extensions.find((x) => x.id === "wecom-local")?.enabled,
);
const local = reactive({ accountId: "", dataDir: "", keyFile: "" });
watch(
  () => channel.value?.local,
  (value) =>
    Object.assign(local, value ?? { accountId: "", dataDir: "", keyFile: "" }),
  { immediate: true },
);
async function save() {
  if (
    await run(() =>
      host.configureMessageChannel("wecom-local", {
        accountId: local.accountId,
        secret: "",
        local: { ...local },
      }),
    )
  )
    say(t("本地数据库验证通过，可以启用"));
}
</script>
<template>
  <div id="wecom-local-settings">
    <h2>{{ t("本地数据库") }}</h2>
    <p>
      {{
        t(
          "填写含 message.db 的目录和已有 keys.json 的绝对路径。密钥不复制到应用配置。",
        )
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
      <label for="wecom-local-key">{{ t("密钥文件路径") }}</label>
      <input
        id="wecom-local-key"
        class="field"
        v-model="local.keyFile"
        :disabled="enabled || busy"
        autocomplete="off"
        required
      />
      <div class="acts">
        <button id="wecom-local-save" class="btn" :disabled="enabled || busy">
          {{ t("保存并验证") }}
        </button>
      </div>
    </form>
    <p v-if="enabled">
      {{ t("修改账号前请先停用扩展。停用保留账号和消息日志。") }}
    </p>
  </div>
</template>
