<script setup lang="ts">
import { computed, ref, watch } from "vue";
import {
  state,
  shell,
  navigate,
  run,
  modal,
  closeDialog,
  ask,
} from "../state/desktop";
import { host } from "../host";
import { t } from "../i18n";
const listener = ref("127.0.0.1");
const port = ref("");
watch(
  () => state.value?.gateway.addr,
  (addr) => {
    const match = addr?.match(/^(?:\[([^\]]+)\]|([^:]+)):(\d+)$/);
    listener.value = match?.[1] || match?.[2] || "127.0.0.1";
    port.value = match?.[3] || "";
  },
  { immediate: true },
);
const invalidPort = computed(
  () => !/^\d+$/.test(port.value) || +port.value < 1 || +port.value > 65535,
);
const addresses = computed(() => [
  ...new Set([...(state.value?.addresses ?? []), listener.value]),
]);
function preferences(event: Event) {
  void run(() =>
    host.setPreferences({
      ...state.value!.preferences,
      language: (event.target as HTMLSelectElement).value,
    }),
  );
}
function details() {
  const cert = state.value!.certificate;
  modal.value = {
    title: "证书详情",
    message: [
      cert.name,
      cert.fingerprint,
      cert.notBefore,
      cert.notAfter,
      t("信任范围：当前 macOS 用户"),
    ].join("\n"),
    actions: [
      { label: "重新检测", run: () => run(() => host.recheckCertificate()) },
      { label: "关闭", run: closeDialog },
    ],
  };
}
function addDomain() {
  modal.value = {
    title: "添加解密域名",
    input: "",
    inputId: "decryption-domain-input",
    actions: [
      { label: "取消", run: closeDialog },
      {
        label: "添加",
        run: async () => {
          const domain = modal.value?.input ?? "";
          if (await run(() => host.setDecryptionDomain(domain, true)))
            closeDialog();
        },
      },
    ],
  };
}
const themes = [
  ["system", "跟随系统"],
  ["light", "浅色"],
  ["dark", "深色"],
] as const;
</script>
<template>
  <template v-if="state">
    <h1>{{ t(shell.route === "proxy" ? "代理" : "通用") }}</h1>
    <nav class="settings-tabs">
      <button id="settings-tab-proxy" @click="navigate('proxy')">
        {{ t("代理") }}</button
      ><button id="settings-tab-general" @click="navigate('general')">
        {{ t("通用") }}
      </button>
    </nav>
    <template v-if="shell.route === 'general'">
      <section class="card general-row">
        <h3>{{ t("语言") }}</h3>
        <p class="ac-rule-desc">{{ t("选择界面语言，保存后立即生效。") }}</p>
        <select
          id="preference-language"
          :value="state.preferences.language"
          @change="preferences"
        >
          <option value="system">{{ t("跟随系统") }}</option>
          <option value="zh-Hans">简体中文</option>
          <option value="en">English</option>
        </select>
      </section>
      <section class="card general-row">
        <h3>{{ t("主题") }}</h3>
        <p class="ac-rule-desc">{{ t("选择应用外观，保存后立即生效。") }}</p>
        <div class="ac-appearance">
          <button
            v-for="[id, label] in themes"
            :key="id"
            :id="'preference-theme-' + id"
            :aria-pressed="state.preferences.theme === id"
            @click="
              run(() =>
                host.setPreferences({ ...state!.preferences, theme: id }),
              )
            "
          >
            {{ t(label) }}
          </button>
        </div>
      </section>
    </template>
    <template v-else>
      <section class="card">
        <h3>{{ t("本地代理") }}</h3>
        <p id="proxy-address">{{ state.gateway.addr }}</p>
        <select id="listener-address" v-model="listener">
          <option v-for="ip in addresses" :key="ip" :value="ip">
            {{ ip }}
          </option>
        </select>
        <input
          id="listener-port"
          v-model="port"
          inputmode="numeric"
          :aria-label="t('端口')"
        />
        <button
          id="listener-apply"
          :disabled="invalidPort"
          @click="
            run(() =>
              host.setListener(
                (listener.includes(':') ? `[${listener}]` : listener) +
                  ':' +
                  port,
              ),
            )
          "
        >
          {{ t("应用") }}
        </button>
        <p id="listener-error">
          {{ invalidPort ? t("端口范围为 1–65535") : "" }}
        </p>
      </section>
      <section class="card">
        <h3>{{ t("系统代理") }}</h3>
        <p>{{ t(state.proxy.enabled ? "已启用" : "未启用") }}</p>
        <button
          id="system-proxy-toggle"
          @click="
            state.proxy.enabled
              ? run(() => host.setSystemProxy(false))
              : ask(
                  '系统代理',
                  '更改此 Mac 的 HTTP 与 HTTPS 代理，关闭时恢复原设置。',
                  () => run(() => host.setSystemProxy(true)),
                )
          "
        >
          {{ t(state.proxy.enabled ? "关闭" : "启用") }}
        </button>
        <p v-if="state.proxy.problem">{{ state.proxy.problem }}</p>
        <button
          v-if="state.proxy.recovery"
          id="system-proxy-restore"
          @click="run(() => host.setSystemProxy(false))"
        >
          {{ t("恢复原设置") }}
        </button>
      </section>
      <section class="card">
        <h3>{{ t("本机证书") }}</h3>
        <p id="certificate-status">
          {{
            t(
              state.certificate.pendingDeletion
                ? "删除未完成"
                : !state.certificate.exists
                  ? "未安装"
                  : state.certificate.expired
                    ? "已过期"
                    : state.certificate.trusted
                      ? "已信任"
                      : "未信任",
            )
          }}
        </p>
        <button
          v-if="
            !state.certificate.trusted && !state.certificate.pendingDeletion
          "
          id="certificate-install"
          @click="run(() => host.installCertificate())"
        >
          {{ t("安装证书") }}
        </button>
        <template v-if="state.certificate.exists">
          <button id="certificate-details" @click="details">
            {{ t("查看证书") }}</button
          ><button
            id="certificate-export"
            @click="run(() => host.exportCertificate())"
          >
            {{ t("导出证书") }}
          </button>
          <button
            id="certificate-keychain"
            @click="run(() => host.openKeychain())"
          >
            {{ t("在钥匙串中管理") }}</button
          ><button
            id="certificate-recheck"
            @click="run(() => host.recheckCertificate())"
          >
            {{ t("重新检测") }}
          </button>
          <button
            id="certificate-delete"
            @click="
              ask(
                '删除本机根证书',
                '撤销当前用户信任，并删除本机根证书与私钥。',
                () => run(() => host.deleteCertificate()),
              )
            "
          >
            {{
              t(state.certificate.pendingDeletion ? "重试删除…" : "删除证书…")
            }}
          </button>
        </template>
        <p v-if="state.certificateError">{{ state.certificateError }}</p>
      </section>
      <section class="card">
        <h3>{{ t("解密域名") }}</h3>
        <p>{{ t("仅解密此列表中的域名。") }}</p>
        <button id="add-decryption-domain" @click="addDomain">
          {{ t("添加域名") }}
        </button>
        <div v-for="domain in state.domains" :key="domain" class="setting-row">
          <p>{{ domain }}</p>
          <button
            :id="'remove-decryption-' + domain"
            @click="run(() => host.setDecryptionDomain(domain, false))"
          >
            {{ t("移除") }}
          </button>
        </div>
      </section>
    </template>
  </template>
</template>
