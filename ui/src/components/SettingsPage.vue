<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from "vue";
import {
  state,
  shell,
  navigate,
  run,
  ask,
  busy,
  say,
  problem,
  refresh,
  toggleSystemProxy,
} from "../state/desktop";
import { host } from "../host";
import { t } from "../i18n";
import Icon from "./Icon.vue";
import Seg from "./Seg.vue";
import ExtensionSettingsPanel from "./ExtensionSettingsPanel.vue";
import { extensionList, uiOf } from "../extensions";

const configurable = computed(() =>
  extensionList().filter(
    (extension) => extension.installed && uiOf(extension.id).Settings,
  ),
);
const extensionsExpanded = ref(true);

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
const listenerAddress = computed(
  () =>
    (listener.value.includes(":") ? `[${listener.value}]` : listener.value) +
    ":" +
    port.value,
);
const listenerChanged = computed(
  () => listenerAddress.value !== state.value?.gateway.addr,
);
const applyingListener = ref(false);
async function applyListener() {
  if (busy.value || invalidPort.value || !listenerChanged.value) return;
  const address = listenerAddress.value;
  applyingListener.value = true;
  try {
    if (await run(() => host.setListener(address)))
      say(t("已切换到 {addr}", { addr: address }));
  } finally {
    applyingListener.value = false;
  }
}

const languages = [
  ["system", "跟随系统"],
  ["zh-Hans", "简体中文"],
  ["en", "English"],
] as const;
const themes = [
  ["system", "跟随系统"],
  ["light", "浅色"],
  ["dark", "深色"],
] as const;
function setPreference(key: "language" | "theme", value: string) {
  void run(() =>
    host.setPreferences({ ...state.value!.preferences, [key]: value }),
  );
}

const cert = computed(() => state.value!.certificate);
const certState = computed(() =>
  cert.value.pendingDeletion
    ? "pending"
    : !cert.value.exists
      ? "none"
      : cert.value.expired
        ? "expired"
        : cert.value.trusted
          ? "trusted"
          : "untrusted",
);
const installing = ref(false);
async function install(replace = false) {
  installing.value = true;
  try {
    if (
      await run(async () => {
        if (replace) await host.deleteCertificate();
        await host.installCertificate();
      })
    )
      if (state.value?.certificate.trusted) say(t("证书已安装并被系统信任"));
  } finally {
    installing.value = false;
  }
}
function replaceCertificate() {
  ask(
    "更换过期证书",
    "删除过期的本机根证书并安装新证书。使用旧证书的连接将断开。",
    () => install(true),
  );
}
let active = true;
onBeforeUnmount(() => {
  active = false;
});
const day = (value: string) => (value ? value.slice(0, 10) : "");

const newDomain = ref("");
const domainError = ref("");
const addingDomain = ref(false);
async function addDomain() {
  if (addingDomain.value || busy.value) return;
  const draft = newDomain.value;
  const domain = draft.trim().toLowerCase();
  domainError.value = "";
  if (!domain) return;
  if (state.value!.domains.includes(domain)) {
    domainError.value = t("{domain} 已在列表里", { domain });
    return;
  }
  addingDomain.value = true;
  try {
    await host.setDecryptionDomain(domain, true);
    await refresh();
    if (active && newDomain.value === draft) newDomain.value = "";
    say(t("已添加 {domain}", { domain }));
  } catch (error) {
    if (active && shell.route === "proxy" && newDomain.value === draft)
      domainError.value =
        error instanceof Error ? error.message : String(error);
    else problem(error);
  } finally {
    addingDomain.value = false;
  }
}
async function removeDomain(domain: string) {
  if (await run(() => host.setDecryptionDomain(domain, false)))
    say(t("已移除 {domain}", { domain }));
}
</script>
<template>
  <div class="split" v-if="state">
    <aside class="side" id="module-sidebar" :aria-label="t('设置分类')">
      <button
        id="settings-general"
        class="side-item"
        :aria-current="shell.route === 'general' ? 'true' : undefined"
        @click="navigate('general')"
      >
        <span class="label">{{ t("通用") }}</span>
      </button>
      <button
        id="settings-proxy"
        class="side-item"
        :aria-current="shell.route === 'proxy' ? 'true' : undefined"
        @click="navigate('proxy')"
      >
        <span class="label">{{ t("代理") }}</span>
      </button>
      <section v-if="configurable.length">
        <button
          id="settings-extensions"
          class="xsec"
          :aria-expanded="extensionsExpanded"
          @click="extensionsExpanded = !extensionsExpanded"
        >
          <Icon name="chev" /><span class="label">{{ t("扩展") }}</span>
        </button>
        <button
          v-for="extension in configurable"
          v-show="extensionsExpanded"
          :key="extension.id"
          :id="'settings-extension-' + extension.id"
          class="side-item"
          :aria-current="
            (shell.route === 'extension-settings' ||
              shell.route === 'extension-diagnostics') &&
            shell.extension === extension.id
              ? 'true'
              : undefined
          "
          @click="navigate('extension-settings', { extension: extension.id })"
        >
          <span class="label">{{ t(extension.name) }}</span>
        </button>
      </section>
    </aside>
    <section class="main">
      <div class="doc" v-if="shell.route === 'general'">
        <h1>{{ t("通用") }}</h1>
        <div class="rows">
          <div class="row">
            <div>
              <h2>{{ t("界面语言") }}</h2>
            </div>
            <div class="ctl">
              <Seg
                :model-value="state.preferences.language"
                :options="languages"
                :disabled="busy"
                label="界面语言"
                id-prefix="preference-language-"
                @update:model-value="setPreference('language', $event)"
              />
            </div>
          </div>
          <div class="row">
            <div>
              <h2>{{ t("主题") }}</h2>
            </div>
            <div class="ctl">
              <Seg
                :model-value="state.preferences.theme"
                :options="themes"
                :disabled="busy"
                label="主题"
                id-prefix="preference-theme-"
                @update:model-value="setPreference('theme', $event)"
              />
            </div>
          </div>
        </div>
      </div>
      <ExtensionSettingsPanel
        v-else-if="
          shell.route === 'extension-settings' ||
          shell.route === 'extension-diagnostics'
        "
      />
      <div class="doc" v-else>
        <h1>{{ t("代理") }}</h1>
        <p class="lede">
          {{ t("只有明确配置了代理的客户端，流量才会经过 aicat。") }}
        </p>
        <div class="rows">
          <div class="row">
            <div>
              <h2>{{ t("监听地址") }}</h2>
              <p>
                {{
                  t(
                    "代理、API 和 MCP 共用这个地址。API 和 MCP 始终只接受本机请求。",
                  )
                }}
              </p>
              <p v-if="state.gateway.error" class="error">
                {{ state.gateway.error }}
              </p>
            </div>
            <div class="ctl">
              <select
                id="listener-address"
                :disabled="busy"
                v-model="listener"
                class="field"
                :aria-label="t('监听 IP')"
              >
                <option v-for="ip in addresses" :key="ip" :value="ip">
                  {{ ip }}
                </option>
              </select>
              <input
                id="listener-port"
                :disabled="busy"
                v-model="port"
                class="field num"
                style="width: 72px"
                inputmode="numeric"
                :aria-label="t('端口')"
                :aria-invalid="invalidPort"
                :aria-describedby="invalidPort ? 'listener-error' : undefined"
                @keydown.enter="applyListener"
              />
              <button
                id="listener-apply"
                class="btn"
                :disabled="busy || invalidPort || !listenerChanged"
                @click="applyListener"
              >
                {{ t(applyingListener ? "应用中…" : "应用") }}
              </button>
            </div>
            <p v-if="invalidPort" id="listener-error" class="wide error">
              {{ t("端口需要是 1 到 65535 之间的数字") }}
            </p>
          </div>
          <div class="row" id="certificate">
            <div>
              <h2>{{ t("根证书") }}</h2>
              <p>
                {{
                  t(
                    "解密 HTTPS 需要系统信任 aicat 的根证书。私钥只保存在这台 Mac 上。",
                  )
                }}
              </p>
            </div>
            <div class="ctl" id="certificate-status">
              <span v-if="certState === 'trusted'" class="state ok">{{
                t("已信任")
              }}</span>
              <span v-else-if="installing" class="waiting" role="status"
                ><i class="spin"></i>{{ t("等待 macOS 授权…") }}</span
              >
              <template v-else-if="certState === 'pending'"
                ><span class="state warn">{{ t("删除未完成") }}</span
                ><button
                  id="certificate-delete"
                  :disabled="busy"
                  class="btn"
                  @click="
                    ask(
                      '删除本机根证书',
                      '撤销当前用户信任，并删除本机根证书与私钥。HTTPS 恢复直通。',
                      () => run(() => host.deleteCertificate()),
                    )
                  "
                >
                  {{ t("重试删除") }}
                </button></template
              >
              <template v-else
                ><span v-if="certState === 'expired'" class="state warn">{{
                  t("已过期")
                }}</span
                ><span
                  v-else-if="certState === 'untrusted'"
                  class="state idle"
                  >{{ t("未信任") }}</span
                ><button
                  v-if="certState === 'expired'"
                  id="certificate-replace"
                  class="btn primary"
                  :disabled="busy || !cert.supported"
                  @click="replaceCertificate"
                >
                  {{ t("更换证书") }}</button
                ><button
                  v-else
                  id="certificate-install"
                  class="btn primary"
                  :disabled="busy || !cert.supported"
                  @click="install()"
                >
                  {{ t("安装证书") }}
                </button></template
              >
            </div>
            <p v-if="state.certificateError" class="wide error">
              {{ state.certificateError }}
            </p>
            <div
              class="wide"
              v-if="cert.exists && certState !== 'pending' && !installing"
            >
              <details id="certificate-details" class="grow">
                <summary>{{ t("证书详情") }}</summary>
                <div class="wide">
                  <dl class="cert">
                    <dt>{{ t("名称", {}, "certificate") }}</dt>
                    <dd>{{ cert.name }}</dd>
                    <dt>{{ t("有效期") }}</dt>
                    <dd class="num">
                      {{
                        t("{from} 至 {to}", {
                          from: day(cert.notBefore),
                          to: day(cert.notAfter),
                        })
                      }}
                    </dd>
                    <dt>SHA-256</dt>
                    <dd>
                      <code>{{ cert.fingerprint }}</code>
                    </dd>
                  </dl>
                </div>
                <div class="wide">
                  <button
                    id="certificate-export"
                    :disabled="busy"
                    class="btn"
                    @click="run(() => host.exportCertificate())"
                  >
                    {{ t("导出公开证书") }}
                  </button>
                  <button
                    id="certificate-keychain"
                    :disabled="busy"
                    class="btn"
                    @click="run(() => host.openKeychain())"
                  >
                    {{ t("打开钥匙串") }}
                  </button>
                  <button
                    id="certificate-recheck"
                    :disabled="busy"
                    class="btn"
                    @click="run(() => host.recheckCertificate())"
                  >
                    {{ t("重新检测") }}
                  </button>
                  <button
                    id="certificate-delete"
                    :disabled="busy"
                    class="btn danger"
                    @click="
                      ask(
                        '删除本机根证书',
                        '撤销当前用户信任，并删除本机根证书与私钥。HTTPS 恢复直通。',
                        () => run(() => host.deleteCertificate()),
                      )
                    "
                  >
                    {{ t("删除证书") }}
                  </button>
                </div>
              </details>
            </div>
          </div>
          <div class="row">
            <div>
              <h2 id="system-proxy-title">{{ t("系统代理") }}</h2>
              <p>
                {{
                  t(
                    "开启后，这台 Mac 上遵循系统代理的应用都会经过 aicat。退出 aicat 时恢复原来的设置。",
                  )
                }}
              </p>
              <p v-if="state.proxy.problem" class="error">
                {{ state.proxy.problem }}
              </p>
            </div>
            <div class="ctl">
              <button
                id="system-proxy-toggle"
                class="switch"
                role="switch"
                :aria-checked="state.proxy.enabled"
                aria-labelledby="system-proxy-title"
                :disabled="
                  busy || !state.proxy.supported || state.proxy.ownedElsewhere
                "
                @click="toggleSystemProxy"
              ></button>
            </div>
            <div v-if="state.proxy.recovery" class="wide">
              <button
                id="system-proxy-restore"
                :disabled="busy"
                class="btn"
                @click="run(() => host.setSystemProxy(false))"
              >
                {{ t("恢复原设置") }}
              </button>
            </div>
          </div>
          <div class="row">
            <div>
              <h2>{{ t("解密域名") }}</h2>
              <p>{{ t("只解密列表中的域名，改动对新连接生效。") }}</p>
              <details>
                <summary>{{ t("通配域名") }}</summary>
                <p>
                  <code>*.example.com</code>
                  {{ t("匹配所有层级的子域名，不含 example.com 本身。") }}
                </p>
              </details>
            </div>
            <div class="wide">
              <input
                id="decryption-domain-input"
                v-model="newDomain"
                class="field"
                style="flex: 1; min-width: 160px"
                :aria-label="t('要解密的域名')"
                :aria-invalid="!!domainError"
                :aria-describedby="
                  domainError ? 'decryption-domain-error' : undefined
                "
                :placeholder="t('例如 api.example.com 或 *.example.com')"
                autocomplete="off"
                spellcheck="false"
                @keydown.enter="addDomain"
                @input="domainError = ''"
              />
              <button
                id="add-decryption-domain"
                class="btn"
                :disabled="busy || addingDomain || !newDomain.trim()"
                @click="addDomain"
              >
                {{ t(addingDomain ? "添加中…" : "添加域名") }}
              </button>
            </div>
            <p
              v-if="domainError"
              id="decryption-domain-error"
              class="wide error"
            >
              {{ domainError }}
            </p>
            <ul class="hosts" v-if="state.domains.length">
              <li v-for="domain in state.domains" :key="domain">
                <code>{{ domain }}</code
                ><button
                  :id="'remove-decryption-' + domain"
                  :disabled="addingDomain || busy"
                  class="icon-btn"
                  :aria-label="t('移除 {domain}', { domain })"
                  :title="t('移除 {domain}', { domain })"
                  @click="removeDomain(domain)"
                >
                  <Icon name="x" />
                </button>
              </li>
            </ul>
          </div>
        </div>
      </div>
    </section>
  </div>
</template>
