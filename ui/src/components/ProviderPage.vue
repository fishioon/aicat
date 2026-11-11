<script setup lang="ts">
import { computed, ref, watch } from "vue";
import {
  state,
  navigate,
  run,
  ask,
  say,
  proxyAddress,
  providerChoice,
  providerKinds,
  type ProviderKind,
} from "../state/desktop";
import { copy } from "../state/traffic";
import { host } from "../host";
import { t } from "../i18n";
import Icon from "./Icon.vue";
import Seg from "./Seg.vue";

const kind = computed(() => providerKinds[providerChoice.value]);
const saved = computed(() =>
  state.value?.providers.find((p) => p.id === providerChoice.value),
);
const baseUrl = ref("");
const key = ref("");
// The form follows the chosen provider and what is saved for it. The key is
// never sent back, so its field always starts empty.
watch(
  [providerChoice, () => saved.value?.baseUrl],
  () => {
    baseUrl.value = saved.value?.baseUrl ?? kind.value.url;
    key.value = "";
  },
  { immediate: true },
);
const options = computed(() =>
  (Object.keys(providerKinds) as ProviderKind[]).map(
    (id) => [id, providerKinds[id].name] as const,
  ),
);
const incomplete = computed(
  () => !baseUrl.value.trim() || (!key.value.trim() && !saved.value),
);
async function save() {
  const name = t(kind.value.name);
  if (
    await run(() =>
      host.setProvider(providerChoice.value, baseUrl.value, key.value),
    )
  ) {
    key.value = "";
    say(t("已保存 {name} 的密钥", { name }));
  }
}
function remove() {
  const name = t(kind.value.name);
  ask(
    "移除供应商",
    "删除保存的接口地址和 API Key。指向 aicat 的 Agent 将无法再调用这个供应商。",
    async () => {
      if (await run(() => host.deleteProvider(providerChoice.value)))
        say(t("已移除 {name}", { name }));
    },
  );
}
const snippet = computed(
  () => `export ${kind.value.env}=http://${proxyAddress()}${kind.value.mount}`,
);
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
      <ol class="steps">
        <li class="step" :class="saved ? 'done' : 'now'">
          <span class="n num">1</span>
          <div>
            <h2>{{ t("填写供应商") }}</h2>
            <Seg
              v-model="providerChoice"
              :options="options"
              label="供应商"
              id-prefix="provider-"
            />
            <div class="form">
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
                    : kind.placeholder
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
                v-if="saved"
                id="provider-delete"
                class="btn danger"
                @click="remove"
              >
                {{ t("移除") }}
              </button>
            </div>
          </div>
        </li>
        <li class="step" :class="{ now: !!saved }">
          <span class="n num">2</span>
          <div>
            <h2>{{ t("让 Agent 经 aicat 调用模型") }}</h2>
            <p>
              {{
                t(
                  "把 Agent 的模型地址指向 aicat，请求会带上保管的密钥转发给 {name}。只接受本机请求。",
                  { name: t(kind.name) },
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
          </div>
        </li>
      </ol>
    </div>
  </div>
</template>
