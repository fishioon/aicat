<script setup lang="ts">
import { computed } from "vue";
import type { TrafficRecord } from "../generated/mygo";
import {
  state,
  busy,
  modal,
  closeDialog,
  run,
  decryptionRule,
  say,
  showCertificateRecovery,
} from "../state/desktop";
import { host } from "../host";
import { t } from "../i18n";
import Icon from "./Icon.vue";
const props = defineProps<{ record: TrafficRecord }>();
const domain = computed(() =>
  new URL(props.record.url).hostname.replace(/^\[|\]$/g, ""),
);
const rule = computed(() => decryptionRule(domain.value));
const unavailable = computed(() => {
  const certificate = state.value?.certificate;
  return (
    !certificate?.exists || certificate.expired || certificate.pendingDeletion
  );
});
async function enable(target: string) {
  if (await run(() => host.setDecryptionDomain(target, true)))
    say(t("已开始解密 {domain}，新连接生效", { domain: target }));
}
function decrypt() {
  const target = domain.value;
  if (showCertificateRecovery()) return;
  if (state.value?.certificate.trusted) {
    void enable(target);
    return;
  }
  const exists = state.value?.certificate.exists;
  modal.value = {
    title: "HTTPS 解密",
    message: t(
      exists
        ? "解密 {domain} 需要系统信任 aicat 的根证书。也可以在客户端里单独信任它。"
        : "解密 {domain} 需要先安装根证书。",
      { domain: target },
    ),
    actions: [
      { label: "取消", run: closeDialog },
      ...(exists
        ? [
            {
              label: "客户端已单独信任",
              run: () => {
                closeDialog();
                return enable(target);
              },
            },
          ]
        : []),
      {
        label: "安装证书",
        run: async () => {
          closeDialog();
          if (await run(() => host.installCertificate())) await enable(target);
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
        <p>{{ t("此连接未解密，无法查看响应内容。") }}</p>
        <div v-if="rule && unavailable" class="note plain">
          {{ t("已配置 {rule}，根证书不可用，HTTPS 暂不解密。", { rule }) }}
          <button
            id="repair-certificate"
            class="btn"
            :disabled="busy"
            @click="decrypt"
          >
            {{ t("处理证书") }}
          </button>
        </div>
        <div v-else-if="rule" id="decryption-covered" class="note plain">
          {{
            rule === domain
              ? t("{domain} 已配置解密，仅新连接生效。", { domain })
              : t("{domain} 匹配 {rule}，仅新连接生效。", { domain, rule })
          }}
        </div>
        <button
          v-else
          id="enable-decryption"
          :disabled="busy"
          class="btn primary"
          @click="decrypt"
        >
          {{ t("仅启用此域名") }}
        </button>
      </div>
    </div>
  </section>
</template>
