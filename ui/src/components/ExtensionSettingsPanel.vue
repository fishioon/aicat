<script setup lang="ts">
import { computed } from "vue";
import { shell, navigate } from "../state/desktop";
import { extensionOf, uiOf } from "../extensions";
import { t } from "../i18n";

const extension = computed(() => extensionOf(shell.extension));
const ui = computed(() => uiOf(shell.extension));
const diagnostics = computed(() => shell.route === "extension-diagnostics");
const status = computed(() => ui.value.status?.());
const body = computed(() =>
  diagnostics.value ? ui.value.Diagnostics : ui.value.Settings,
);
</script>
<template>
  <div
    class="doc extension-panel"
    :id="diagnostics ? 'extension-diagnostics-page' : 'extension-settings-page'"
    :data-extension="extension?.id"
  >
    <h1>
      {{
        diagnostics
          ? t("{name}诊断", { name: t(extension?.name ?? "扩展") })
          : t(extension?.name ?? "扩展")
      }}
    </h1>
    <p v-if="status" class="lede" id="extension-panel-status">
      {{ status.text }}
    </p>
    <div class="acts">
      <button
        id="extension-manage"
        class="btn"
        @click="navigate('extensions', { extension: shell.extension })"
      >
        {{ t("管理扩展") }}
      </button>
      <button
        v-if="!diagnostics && ui.Diagnostics"
        id="extension-settings-diagnostics"
        class="btn"
        @click="
          navigate('extension-diagnostics', { extension: shell.extension })
        "
      >
        {{ t("诊断") }}
      </button>
      <button
        v-if="diagnostics && ui.Settings"
        id="extension-diagnostics-configure"
        class="btn"
        @click="navigate('extension-settings', { extension: shell.extension })"
      >
        {{ t("配置") }}
      </button>
    </div>
    <component v-if="body" :is="body" :key="shell.extension + shell.route" />
    <p v-else>{{ t("此扩展没有独立配置。") }}</p>
  </div>
</template>
