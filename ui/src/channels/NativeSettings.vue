<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { host } from "../host";
import { shell, state, busy, run, say, navigate } from "../state/desktop";
import { t } from "../i18n";

const id = computed(() => shell.extension);
const extension = computed(() =>
  state.value?.extensions.find((x) => x.id === id.value),
);
const fields = computed(() => extension.value?.configFields ?? []);
const values = ref<Record<string, string | boolean>>({});
const error = ref("");
let generation = 0;

async function load() {
  const request = ++generation;
  const current = id.value;
  try {
    const config = await host.readExtensionConfig(current);
    if (request !== generation || current !== id.value) return;
    const settings = config.settings as Record<string, unknown> | undefined;
    const next: Record<string, string | boolean> = {};
    for (const field of fields.value) {
      const value = settings?.[field.key];
      next[field.key] =
        field.type === "secret"
          ? ""
          : field.type === "boolean"
            ? !!value
            : typeof value === "string"
              ? value
              : "";
    }
    values.value = next;
    error.value = "";
  } catch (e) {
    error.value = String(e);
  }
}

async function save() {
  const current = id.value;
  if (
    await run(() =>
      host.configureMessageChannel(current, {
        accountId: "",
        secret: "",
        values: { ...values.value },
      }),
    )
  ) {
    for (const field of fields.value)
      if (field.type === "secret") values.value[field.key] = "";
    say(t("渠道配置已保存，可以启用"));
  }
}

watch([id, () => JSON.stringify(fields.value)], () => void load(), {
  immediate: true,
});
</script>
<template>
  <section id="native-channel-settings">
    <p v-if="error" role="alert">{{ error }}</p>
    <details v-if="fields.length" :open="!extension?.automaticSetup">
      <summary v-if="extension?.automaticSetup">{{ t("高级设置") }}</summary>
      <form class="form" @submit.prevent="save">
        <template v-for="field in fields" :key="field.key">
          <label :for="`channel-${field.key}`">{{
            field.label || field.key
          }}</label>
          <input
            v-if="field.type === 'boolean'"
            :id="`channel-${field.key}`"
            type="checkbox"
            v-model="values[field.key]"
            :disabled="extension?.enabled || busy"
          />
          <input
            v-else
            :id="`channel-${field.key}`"
            class="field"
            :type="field.type === 'secret' ? 'password' : 'text'"
            v-model="values[field.key]"
            :required="field.required && field.type !== 'secret'"
            :disabled="extension?.enabled || busy"
            :autocomplete="field.type === 'secret' ? 'new-password' : 'off'"
            :placeholder="
              field.type === 'secret' ? t('留空保留已保存的凭据') : ''
            "
          />
        </template>
        <div class="acts wide">
          <button class="btn" :disabled="extension?.enabled || busy">
            {{ t("保存渠道配置") }}
          </button>
        </div>
      </form>
    </details>
    <p v-else>{{ t("此渠道无需填写账号配置。") }}</p>
    <p>{{ t("消息处理、model 和 Room prompt 在对应会话中设置。") }}</p>
    <button class="btn" type="button" @click="navigate('messages')">
      {{ t("打开消息") }}
    </button>
  </section>
</template>
