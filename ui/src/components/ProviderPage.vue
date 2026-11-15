<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from "vue";
import {
  state,
  shell,
  navigate,
  run,
  ask,
  say,
  problem,
  refresh,
  busy,
} from "../state/desktop";
import { host } from "../host";
import { t, locale } from "../i18n";
import Icon from "./Icon.vue";
import ProviderIcon from "./ProviderIcon.vue";

const saved = computed(() =>
  state.value?.providers.find((p) => p.id === shell.provider),
);
const name = ref("");
const baseUrl = ref("");
const key = ref("");
// Watch only persisted fields; unrelated state notifications must not wipe drafts.
watch(
  [() => shell.provider, () => saved.value?.baseUrl, () => saved.value?.name],
  () => {
    name.value = saved.value?.name ?? "";
    baseUrl.value = saved.value?.baseUrl ?? "";
    key.value = "";
  },
  { immediate: true },
);
const incomplete = computed(
  () =>
    !name.value.trim() ||
    !baseUrl.value.trim() ||
    (!key.value.trim() && !saved.value),
);
// Saving a new endpoint or key first asks the vendor for its model list; a
// provider that gives none is not saved, and why is shown under the form.
let active = true;
onBeforeUnmount(() => {
  active = false;
});
const saving = ref(false);
const saveError = ref("");
watch([name, baseUrl, key], () => (saveError.value = ""));
async function save() {
  if (modelBusy.value) return;
  const label = name.value.trim();
  const provider = shell.provider;
  saving.value = true;
  saveError.value = "";
  try {
    const id = await host.saveProvider({
      id: saved.value?.id ?? "",
      name: name.value,
      baseUrl: baseUrl.value,
      key: key.value,
    });
    await refresh();
    if (active && shell.provider === provider) {
      shell.provider = id;
      key.value = "";
    }
    say(t("已保存供应商 {name}", { name: label }));
  } catch (error) {
    await refresh().catch(() => {});
    if (active && shell.provider === provider)
      saveError.value = error instanceof Error ? error.message : String(error);
    else problem(error);
  } finally {
    saving.value = false;
  }
}
function remove() {
  const id = shell.provider;
  const label = saved.value?.name ?? "";
  ask(
    "移除供应商",
    "删除保存的接口地址和 API Key。指向 aicat 的 Agent 将无法再调用这个供应商。",
    async () => {
      if (await run(() => host.deleteProvider(id))) {
        say(t("已移除 {name}", { name: label }));
        if (active && shell.route === "provider" && shell.provider === id)
          navigate("overview");
      }
    },
  );
}
// The model list is asked for when a provider is saved with a new endpoint or
// key; a sync can also be asked for again here.
const syncing = ref(false);
async function sync() {
  if (modelBusy.value) return;
  const id = shell.provider;
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
  if (saving.value)
    return { kind: "plain", text: t("正在验证接口地址和密钥…") };
  if (saveError.value) return { kind: "bad", text: saveError.value };
  const p = saved.value;
  if (!p) return undefined;
  if (p.syncing || syncing.value)
    return { kind: "plain", text: t("正在验证接口地址和密钥…") };
  const where = { host: new URL(p.baseUrl).host };
  switch (p.models.error ? p.models.errorKind || "endpoint" : "") {
    case "auth":
      return {
        kind: "bad",
        text: t("{host} 拒绝了密钥，请检查 API Key。", where),
      };
    case "network":
      return {
        kind: "bad",
        text: t("连不上 {host}。请检查接口地址和网络。", where),
      };
    case "endpoint":
      return {
        kind: "warn",
        text: t("未找到模型列表，请检查接口地址。"),
      };
  }
  if (!p.models.syncedAt) return undefined;
  return { kind: "ok", text: t("模型列表可用") };
});
// Ticked models are the ones agents are offered; a filter narrows a long list.
const modelQuery = ref("");
watch(
  () => shell.provider,
  () => (modelQuery.value = ""),
);
const shownModels = computed(() => {
  const q = modelQuery.value.trim().toLowerCase();
  const all = saved.value?.models.models ?? [];
  return q
    ? all.filter((m) => (m.id + " " + (m.name ?? "")).toLowerCase().includes(q))
    : all;
});
const modelBusy = computed(
  () => busy.value || saving.value || syncing.value || !!saved.value?.syncing,
);
const shownEnabled = computed(
  () =>
    shownModels.value.filter((model) => saved.value?.enabled.includes(model.id))
      .length,
);
async function setModels(next: string[]) {
  if (modelBusy.value) return false;
  const id = shell.provider;
  // the vendor's order, whatever order they were ticked in
  const order = saved.value?.models.models.map((m) => m.id) ?? [];
  return run(() =>
    host.setProviderModels(
      id,
      order.filter((m) => next.includes(m)),
    ),
  );
}
async function toggleModel(model: string, event: Event) {
  const input = event.target as HTMLInputElement;
  const focused = document.activeElement === input;
  const enabled = saved.value?.enabled ?? [];
  await setModels(
    input.checked ? [...enabled, model] : enabled.filter((m) => m !== model),
  );
  if (input.isConnected) {
    input.checked = !!saved.value?.enabled.includes(model);
    if (focused && document.activeElement === document.body)
      input.focus({ preventScroll: true });
  }
}
// All and None act on the models the filter shows.
function tickShown(on: boolean) {
  const shown = shownModels.value.map((m) => m.id);
  const enabled = saved.value?.enabled ?? [];
  setModels(
    on ? [...enabled, ...shown] : enabled.filter((m) => !shown.includes(m)),
  );
}
</script>
<template>
  <div class="main" id="provider">
    <div class="ov">
      <button class="btn back" id="provider-back" @click="navigate('overview')">
        <Icon name="back" />{{ t("返回主页") }}
      </button>
      <div class="ov-head">
        <h1 class="provider-heading">
          <ProviderIcon v-if="saved" :image="saved.icon" />
          {{ saved ? saved.name : t("添加供应商") }}
        </h1>
      </div>
      <div class="provider-content">
        <form @submit.prevent="!incomplete && save()">
          <div class="form provider-form">
            <label for="provider-name">{{ t("名称", {}, "provider") }}</label>
            <input
              id="provider-name"
              v-model="name"
              :disabled="saving"
              class="field"
              maxlength="40"
              autocomplete="off"
              spellcheck="false"
              required
            />
            <label for="provider-url">{{ t("接口地址") }}</label>
            <input
              id="provider-url"
              v-model="baseUrl"
              :disabled="saving"
              class="field code"
              placeholder="https://"
              autocomplete="off"
              spellcheck="false"
              required
            />
            <label for="provider-key">API Key</label>
            <input
              id="provider-key"
              v-model="key"
              :disabled="saving"
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
              :required="!saved"
            />
          </div>
          <p class="provider-key-note">{{ t("密钥仅保存在本机。") }}</p>
          <div class="acts">
            <button
              id="provider-save"
              type="submit"
              class="btn"
              :class="{ primary: !saved }"
              :disabled="incomplete || modelBusy"
            >
              {{ t(saving ? "正在保存…" : saved ? "保存" : "添加") }}
            </button>
            <button
              v-if="saved"
              id="provider-delete"
              :disabled="modelBusy"
              type="button"
              class="btn danger"
              @click="remove"
            >
              {{ t("移除") }}
            </button>
          </div>
        </form>
        <p v-if="status" id="provider-status" class="note" :class="status.kind">
          {{ status.text }}
        </p>
        <section
          v-if="saved"
          class="model-list"
          id="provider-models"
          :aria-busy="modelBusy"
        >
          <header>
            <strong>{{ t("模型列表") }}</strong>
            <span id="provider-models-status">{{ modelStatus }}</span>
            <button
              id="provider-sync"
              class="btn"
              :disabled="modelBusy"
              @click="sync"
            >
              {{ t("重新同步") }}
            </button>
          </header>
          <details v-if="saved.models.error" id="provider-models-error">
            <summary>{{ t("供应商的回答") }}</summary>
            {{ saved.models.error }}
          </details>
          <template v-if="saved.models.models.length">
            <div class="model-tools">
              <span id="provider-models-enabled">{{
                t("已启用 {n} / {total}", {
                  n: saved.enabled.length,
                  total: saved.models.models.length,
                })
              }}</span>
              <input
                id="provider-models-filter"
                v-model="modelQuery"
                class="field"
                :placeholder="t('筛选模型')"
                autocomplete="off"
                spellcheck="false"
              />
              <button
                id="provider-models-all"
                class="btn"
                :disabled="modelBusy || shownEnabled === shownModels.length"
                @click="tickShown(true)"
              >
                {{ t("全选") }}
              </button>
              <button
                id="provider-models-none"
                class="btn"
                :disabled="modelBusy || !shownEnabled"
                @click="tickShown(false)"
              >
                {{ t("全不选") }}
              </button>
            </div>
            <ul>
              <li v-for="m in shownModels" :key="m.id" :title="m.name || m.id">
                <label class="code">
                  <input
                    type="checkbox"
                    :value="m.id"
                    :checked="saved.enabled.includes(m.id)"
                    :disabled="modelBusy"
                    @change="toggleModel(m.id, $event)"
                  />{{ m.id }}
                </label>
              </li>
            </ul>
            <p v-if="!shownModels.length" class="dim">
              {{ t("没有匹配的模型") }}
            </p>
            <p v-if="saved.picked">
              {{ t("供应商新增的模型不会自动启用。") }}
            </p>
          </template>
        </section>
      </div>
    </div>
  </div>
</template>
