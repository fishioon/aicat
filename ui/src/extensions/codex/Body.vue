<script setup lang="ts">
import { computed } from "vue";
import { navigate, proxyAddress } from "../../state/desktop";
import { copy } from "../../state/traffic";
import { t } from "../../i18n";
import Icon from "../../components/Icon.vue";

defineProps<{ tab: "detail" | "features" }>();
const baseURL = computed(
  () => `openai_base_url = "http://${proxyAddress()}/backend-api/codex"`,
);
</script>
<template>
  <template v-if="tab === 'detail'">
    <h2>{{ t("开始使用") }}</h2>
    <ol>
      <li>
        <button
          id="codex-manage-models"
          class="link"
          @click="navigate('provider')"
        >
          {{ t("接入供应商") }}
        </button>
        {{ t("并启用要在 Codex 中使用的模型。") }}
      </li>
      <li>
        {{ t("启用扩展，在") }}
        <button
          id="codex-proxy-settings"
          class="link"
          @click="navigate('proxy')"
        >
          {{ t("代理设置") }}
        </button>
        {{ t("中开启系统代理，然后重启 Codex。") }}
      </li>
    </ol>
    <details id="codex-manual">
      <summary>{{ t("手动接入") }}</summary>
      <p>{{ t("在 ~/.codex/config.toml 添加以下配置，然后重启 Codex。") }}</p>
      <div class="snippet">
        <header>
          <span>~/.codex/config.toml</span>
          <button
            id="codex-copy"
            class="btn"
            @click="copy('codex-snippet', baseURL)"
          >
            <Icon name="copy" />{{ t("复制配置") }}
          </button>
        </header>
        <pre id="codex-snippet">{{ baseURL }}</pre>
      </div>
    </details>
    <h2>{{ t("使用限制") }}</h2>
    <p>
      {{
        t(
          "供应商需支持 Responses。供应商模型暂不支持对话压缩，接近上下文上限时请新开对话。",
        )
      }}
    </p>
    <h2>{{ t("停用之后") }}</h2>
    <p>
      {{ t("重启 Codex；如果手动配置过 openai_base_url，请先删除该行配置。") }}
    </p>
  </template>
  <table v-else class="feat">
    <tbody>
      <tr>
        <th scope="row">{{ t("模型列表") }}</th>
        <td>
          <button class="link" @click="navigate('provider')">
            {{ t("管理供应商模型") }}
          </button>
        </td>
      </tr>
      <tr>
        <th scope="row">{{ t("接入") }}</th>
        <td>{{ t("系统代理，或手动配置 openai_base_url") }}</td>
      </tr>
      <tr>
        <th scope="row">{{ t("设置") }}</th>
        <td>
          <button class="link" @click="navigate('proxy')">
            {{ t("代理设置") }}
          </button>
        </td>
      </tr>
    </tbody>
  </table>
</template>
