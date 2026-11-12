<script setup lang="ts">
import { computed, ref, watch } from "vue";
import {
  state,
  navigate,
  run,
  ask,
  say,
  problem,
  refresh,
  proxyAddress,
} from "../state/desktop";
import {
  providerId,
  providerPresets,
  customEndpoints,
  protocolNames,
  protocolEnv,
  type Protocol,
  type ProviderPreset,
} from "../state/providers";
import { copy } from "../state/traffic";
import { host } from "../host";
import { t, locale } from "../i18n";
import Icon from "./Icon.vue";
import Seg from "./Seg.vue";

const saved = computed(() =>
  state.value?.providers.find((p) => p.id === providerId.value),
);
// A new provider starts from a preset or as a custom one; nothing picked yet
// shows the catalog.
const picked = ref<ProviderPreset | "custom">();
const preset = computed(() =>
  saved.value
    ? providerPresets.find((p) => p.id === saved.value!.preset)
    : picked.value === "custom"
      ? undefined
      : picked.value,
);
const picking = computed(() => !saved.value && !picked.value);
const endpoints = computed(() => preset.value?.endpoints ?? customEndpoints);
const name = ref("");
const protocol = ref<Protocol>("openai");
const baseUrl = ref("");
const key = ref("");
// The form follows the provider being edited and what is saved for it. The
// key is never sent back, so its field always starts empty.
// Getters only: a shallowRef source makes Vue run the callback on every state
// refresh, which would wipe an address being typed whenever a sync ends.
watch(
  [
    () => providerId.value,
    () => picked.value,
    () => saved.value?.baseUrl,
    () => saved.value?.protocol,
    () => saved.value?.name,
  ],
  () => {
    const [first, url] = endpoints.value[0];
    name.value = saved.value?.name ?? preset.value?.name ?? "";
    protocol.value = (saved.value?.protocol as Protocol) ?? first;
    baseUrl.value = saved.value?.baseUrl ?? url;
    key.value = "";
  },
  { immediate: true },
);
const protocols = computed(() => {
  const list = endpoints.value.map(([id]) => id);
  if (!list.includes(protocol.value)) list.push(protocol.value);
  return list.map((id) => [id, protocolNames[id]] as const);
});
// Another protocol of the same vendor lives at another address: follow it
// unless the address was typed by hand.
function setProtocol(next: Protocol) {
  const known = endpoints.value.map(([, url]) => url);
  if (!baseUrl.value.trim() || known.includes(baseUrl.value.trim()))
    baseUrl.value = endpoints.value.find(([id]) => id === next)?.[1] ?? "";
  protocol.value = next;
}
const incomplete = computed(
  () =>
    !name.value.trim() ||
    !baseUrl.value.trim() ||
    (!key.value.trim() && !saved.value),
);
async function save() {
  let id = "";
  if (
    await run(async () => {
      id = await host.saveProvider({
        id: saved.value?.id ?? "",
        name: name.value,
        preset: preset.value?.id ?? "",
        protocol: protocol.value,
        baseUrl: baseUrl.value,
        key: key.value,
      });
    })
  ) {
    providerId.value = id;
    key.value = "";
    say(t("已保存 {name} 的密钥", { name: name.value.trim() }));
  }
}
function remove() {
  const id = providerId.value;
  const label = saved.value?.name ?? "";
  ask(
    "移除供应商",
    "删除保存的接口地址和 API Key。指向 aicat 的 Agent 将无法再调用这个供应商。",
    async () => {
      if (await run(() => host.deleteProvider(id))) {
        say(t("已移除 {name}", { name: label }));
        navigate("overview");
      }
    },
  );
}
// The model list is asked for when a provider is saved with a new endpoint or
// key; a sync can also be asked for again here.
const syncing = ref(false);
async function sync() {
  const id = providerId.value;
  syncing.value = true;
  try {
    await host.syncProviderModels(id);
    await refresh();
    const n =
      state.value?.providers.find((p) => p.id === id)?.models.models.length ??
      0;
    say(t("已同步 {n} 个模型", { n }));
  } catch (error) {
    await refresh().catch(() => {});
    problem(error);
  } finally {
    syncing.value = false;
  }
}
const modelStatus = computed(() => {
  const p = saved.value;
  if (!p) return "";
  if (p.syncing || syncing.value) return t("正在同步…");
  if (!p.models.syncedAt) return p.models.error ? "" : t("尚未同步");
  return t("{n} 个模型 · {time} 同步", {
    n: p.models.models.length,
    time: new Date(p.models.syncedAt).toLocaleString(locale.value),
  });
});
// Whether the saved endpoint and key work, as far as asking the vendor for
// its model list can tell.
const status = computed(() => {
  const p = saved.value;
  if (!p) return undefined;
  if (p.syncing || syncing.value)
    return { kind: "plain", text: t("正在验证接口地址和密钥…") };
  const where = { host: new URL(p.baseUrl).host };
  switch (p.models.error ? p.models.errorKind || "endpoint" : "") {
    case "auth":
      return {
        kind: "bad",
        text: t(
          "{host} 拒绝了这把密钥。请确认密钥是这家供应商签发的，而不是别家的。",
          where,
        ),
      };
    case "network":
      return {
        kind: "bad",
        text: t("连不上 {host}。请检查接口地址和网络。", where),
      };
    case "endpoint":
      return {
        kind: "warn",
        text: t(
          "没有在这个地址找到模型列表，接口地址可能不对。少数供应商不提供模型列表，这时请求仍然可以转发。",
        ),
      };
  }
  if (!p.models.syncedAt) return undefined;
  return { kind: "ok", text: t("已连通，密钥有效。") };
});
const connected = computed(() => status.value?.kind === "ok");
// Every provider is reached at the same address; the model's prefix picks
// the provider. An OpenAI-style base URL ends in /v1, Anthropic's does not.
const snippet = computed(
  () =>
    `export ${protocolEnv[protocol.value]}=http://${proxyAddress()}${protocol.value === "anthropic" ? "" : "/v1"}`,
);
const modelExample = computed(() => {
  const p = saved.value;
  return p ? `${p.id}/${p.models.models[0]?.id ?? t("模型名")}` : "";
});
</script>
<template>
  <div class="main" id="provider">
    <div class="ov">
      <button class="btn back" id="provider-back" @click="navigate('overview')">
        <Icon name="back" />{{ t("返回概览") }}
      </button>
      <div class="ov-head">
        <div>
          <h1>{{ t("接入供应商") }}</h1>
          <p>
            {{
              t(
                "把供应商的密钥交给 aicat 保管，Agent 只需要指向 aicat，不用各自保存密钥。",
              )
            }}
          </p>
        </div>
      </div>
      <section v-if="picking" class="presets" id="provider-presets">
        <h2>{{ t("选择供应商") }}</h2>
        <div>
          <button
            v-for="p in providerPresets"
            :key="p.id"
            :id="'preset-' + p.id"
            class="btn"
            @click="picked = p"
          >
            {{ t(p.name) }}
          </button>
          <button id="preset-custom" class="btn" @click="picked = 'custom'">
            {{ t("自定义…") }}
          </button>
        </div>
        <p>
          {{
            t(
              "列表里没有的供应商选“自定义”，填它的接口地址即可；同一家可以接入多次。",
            )
          }}
        </p>
      </section>
      <ol v-else class="steps">
        <li class="step" :class="connected ? 'done' : 'now'">
          <span class="n num">1</span>
          <div>
            <h2>
              {{ t("填写供应商")
              }}<button
                v-if="!saved"
                id="provider-repick"
                class="link"
                @click="picked = undefined"
              >
                {{ t("重新选择") }}
              </button>
            </h2>
            <div class="form">
              <label for="provider-name">{{ t("名称") }}</label
              ><input
                id="provider-name"
                v-model="name"
                class="field"
                maxlength="40"
                autocomplete="off"
                spellcheck="false"
              />
              <template v-if="protocols.length > 1">
                <label>{{ t("接口协议") }}</label>
                <Seg
                  :model-value="protocol"
                  :options="protocols"
                  label="接口协议"
                  id-prefix="provider-protocol-"
                  @update:model-value="setProtocol"
                />
              </template>
              <label for="provider-url">{{ t("接口地址") }}</label
              ><input
                id="provider-url"
                v-model="baseUrl"
                class="field code"
                placeholder="https://"
                autocomplete="off"
                spellcheck="false"
              />
              <label for="provider-key">API Key</label
              ><input
                id="provider-key"
                v-model="key"
                class="field code"
                type="password"
                :placeholder="
                  saved
                    ? t('已保存 ····{hint}，留空保持不变', {
                        hint: saved.keyHint,
                      })
                    : 'API Key'
                "
                autocomplete="off"
                @keydown.enter="!incomplete && save()"
              />
            </div>
            <p>
              {{
                t(
                  "密钥保存在这台 Mac 的 aicat 目录里（providers.json，仅当前用户可读），不会出现在流量记录里。",
                )
              }}
            </p>
            <div class="acts">
              <button
                id="provider-save"
                class="btn"
                :class="{ primary: !saved }"
                :disabled="incomplete"
                @click="save"
              >
                {{ t(saved ? "重新保存" : "保存") }}
              </button>
              <button
                v-if="preset && !saved"
                id="provider-keys"
                class="btn"
                @click="run(() => host.openLink(preset!.keys))"
              >
                {{ t("获取密钥") }} ↗
              </button>
              <button
                v-if="saved"
                id="provider-delete"
                class="btn danger"
                @click="remove"
              >
                {{ t("移除") }}
              </button>
            </div>
            <p
              v-if="status"
              id="provider-status"
              class="note"
              :class="status.kind"
            >
              {{ status.text }}
            </p>
            <section v-if="saved" class="model-list" id="provider-models">
              <header>
                <strong>{{ t("模型列表") }}</strong>
                <span id="provider-models-status">{{ modelStatus }}</span>
                <button
                  id="provider-sync"
                  class="btn"
                  :disabled="saved.syncing || syncing"
                  @click="sync"
                >
                  {{ t("重新同步") }}
                </button>
              </header>
              <details v-if="saved.models.error" id="provider-models-error">
                <summary>{{ t("供应商的回答") }}</summary>
                {{ saved.models.error }}
              </details>
              <p v-if="!saved.responses">
                {{ t("这个供应商不支持 Responses 接口。") }}
              </p>
              <ul v-if="saved.models.models.length">
                <li
                  v-for="m in saved.models.models"
                  :key="m.id"
                  class="code"
                  :title="m.name || m.id"
                >
                  {{ m.id }}
                </li>
              </ul>
            </section>
          </div>
        </li>
        <li class="step" :class="{ now: connected }">
          <span class="n num">2</span>
          <div>
            <h2>{{ t("让 Agent 经 aicat 调用模型") }}</h2>
            <p>
              {{
                t(
                  "把 Agent 的模型地址指向 aicat，所有供应商共用这一个地址。aicat 按请求里的模型名选择供应商，带上保管的密钥转发。只接受本机请求。",
                )
              }}
            </p>
            <div class="snippet">
              <header>
                <span>{{ t("在 Agent 的环境变量里设置") }}</span
                ><button
                  id="provider-copy"
                  class="btn"
                  @click="copy('provider-snippet', snippet, t('配置'))"
                >
                  <Icon name="copy" />{{ t("复制") }}
                </button>
              </header>
              <pre id="provider-snippet">{{ snippet }}</pre>
            </div>
            <p v-if="saved" id="provider-model-hint">
              {{ t("模型名写作“供应商 ID/模型”，例如") }}
              <code class="code">{{ modelExample }}</code
              >{{ t("。只有一家供应商提供这个模型时，可以只写模型名。") }}
            </p>
          </div>
        </li>
      </ol>
    </div>
  </div>
</template>
