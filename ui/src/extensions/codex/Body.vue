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
    <h2>{{ t("这个扩展做什么") }}</h2>
    <p>
      {{
        t(
          "Codex 读取模型列表时，aicat 在 ChatGPT 自带的模型之后加入供应商的模型，名称写作“供应商/模型”，例如 openai/gpt-5。Codex 保留自己的 ChatGPT 登录。",
        )
      }}
    </p>
    <p>
      {{
        t(
          "选择供应商的模型时，请求带上 aicat 保管的密钥发给供应商，不带 ChatGPT 的登录凭据；选择 ChatGPT 自带的模型时，请求原样发往 ChatGPT。",
        )
      }}
    </p>
    <p>
      {{
        t(
          "只提供支持 Responses 接口的供应商，即 OpenAI 和兼容 OpenAI 的服务。模型名称、上下文窗口和推理强度先取供应商返回的信息，缺少的部分参考 models.dev。",
        )
      }}
    </p>
    <h2>{{ t("让 Codex 经过 aicat") }}</h2>
    <p>
      {{
        t(
          "开启代理设置里的系统代理后，aicat 解密 Codex 到 chatgpt.com 的连接，Codex 不用改配置。也可以不开系统代理，在 ~/.codex/config.toml 里加入下面这一行，让 Codex 直接连接 aicat：",
        )
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
    <h2>{{ t("限制") }}</h2>
    <p>
      {{
        t(
          "供应商模型的对话暂不支持压缩，接近上下文上限时请新开对话。启用期间 Codex 改用 HTTP 流式请求，不使用 WebSocket。",
        )
      }}
    </p>
    <h2>{{ t("停用之后") }}</h2>
    <p>
      {{
        t(
          "不再解密 Codex 的连接，模型列表恢复为 ChatGPT 自带的模型。已在运行的 Codex 需要重新启动；如果加过上面那行 openai_base_url，也要从 ~/.codex/config.toml 里删掉。",
        )
      }}
    </p>
  </template>
  <table v-else class="feat">
    <tbody>
      <tr>
        <th scope="row">{{ t("模型列表") }}</th>
        <td>
          {{ t("供应商与模型列表在") }}
          <button class="link" @click="navigate('provider')">
            {{ t("接入供应商") }}
          </button>
          {{ t("里，保存后自动同步") }}
        </td>
      </tr>
      <tr>
        <th scope="row">{{ t("接入") }}</th>
        <td>
          {{
            t(
              "经系统代理解密 chatgpt.com，或在 Codex 中把 openai_base_url 指向 aicat",
            )
          }}
        </td>
      </tr>
      <tr>
        <th scope="row">{{ t("设置") }}</th>
        <td>
          {{ t("根证书与系统代理在") }}
          <button class="link" @click="navigate('proxy')">
            {{ t("代理设置") }}
          </button>
          {{ t("里") }}
        </td>
      </tr>
    </tbody>
  </table>
</template>
