<script setup lang="ts">
import { computed } from "vue";
import type { TrafficRecord } from "../generated/mygo";
import {
  state,
  modal,
  closeDialog,
  run,
  decryptionRule,
  say,
} from "../state/desktop";
import { host } from "../host";
import { t } from "../i18n";
import Icon from "./Icon.vue";
const props = defineProps<{ record: TrafficRecord }>();
const domain = computed(() => new URL(props.record.url).hostname);
const rule = computed(() => decryptionRule(domain.value));
async function enable() {
  if (await run(() => host.setDecryptionDomain(domain.value, true))) {
    closeDialog();
    say(t("已开始解密 {domain}，新连接生效", { domain: domain.value }));
  }
}
function decrypt() {
  if (state.value?.certificate.trusted) {
    void enable();
    return;
  }
  modal.value = {
    title: "HTTPS 解密",
    message: t(
      "解密 {domain} 需要系统信任 aicat 的根证书。也可以在客户端里单独信任它。",
      { domain: domain.value },
    ),
    actions: [
      { label: "取消", run: closeDialog },
      { label: "客户端已单独信任", run: enable },
      {
        label: "安装证书",
        run: async () => {
          if (await run(() => host.installCertificate())) await enable();
        },
      },
    ],
  };
}
</script>
<template>
  <section
    class="pane inspector"
    data-side="response"
    :aria-label="t('响应')"
    id="tunnel-detail"
  >
    <div class="pane-head">
      <h3>{{ t("响应") }}</h3>
    </div>
    <div class="tunnel-body" tabindex="0">
      <div class="tunnel">
        <h3><Icon name="lock" />{{ t("HTTPS 响应") }}</h3>
        <p>
          {{ t("此 HTTPS 响应已加密。") }}<br />{{
            t("启用 SSL 代理以查看内容。")
          }}
        </p>
        <div v-if="rule" id="decryption-covered" class="note plain">
          {{
            rule === domain
              ? t(
                  "{domain} 已在解密域名里。这条隧道建立得更早，保持未解密；新连接会被解密。",
                  { domain },
                )
              : t(
                  "{domain} 匹配解密域名 {rule}。这条隧道建立得更早，保持未解密；新连接会被解密。",
                  { domain, rule },
                )
          }}
        </div>
        <button
          v-else
          id="enable-decryption"
          class="btn primary"
          @click="decrypt"
        >
          {{ t("仅启用此域名") }}
        </button>
      </div>
    </div>
  </section>
</template>
