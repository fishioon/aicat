<script setup lang="ts">
import { computed } from "vue";
import { navigate, proxyAddress } from "../../state/desktop";
import { copy } from "../../state/traffic";
import { t } from "../../i18n";
import Icon from "../../components/Icon.vue";

const baseURL = computed(
  () => `openai_base_url = "http://${proxyAddress()}/backend-api/codex"`,
);
</script>
<template>
  <h2>{{ t("接入 Codex") }}</h2>
  <p>
    {{
      t("启用系统代理，或将以下配置加入 ~/.codex/config.toml，然后重启 Codex。")
    }}
  </p>
  <div class="snippet">
    <header>
      <span>~/.codex/config.toml</span
      ><button
        id="codex-copy"
        class="btn"
        @click="copy('codex-snippet', baseURL, t('配置'))"
      >
        <Icon name="copy" />{{ t("复制") }}
      </button>
    </header>
    <pre id="codex-snippet">{{ baseURL }}</pre>
  </div>
  <p>{{ t("停用后，移除上述配置并重启 Codex。") }}</p>
  <button class="btn" @click="navigate('provider')">
    {{ t("接入供应商") }}
  </button>
</template>
