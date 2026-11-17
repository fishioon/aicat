<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { host } from "../host";
import { state, say } from "../state/desktop";
import { t } from "../i18n";
import type { RoomSettings, RoomView } from "../generated/mygo";

const props = defineProps<{ room: RoomView }>();
const emit = defineEmits<{ saved: [] }>();
const draft = ref<RoomSettings>();
const context = ref("{}");
const dirty = ref(false);
const saving = ref(false);
const error = ref("");
const modelError = ref("");
const modelsLoading = ref(false);
const models = ref<Awaited<ReturnType<typeof host.listRoomModels>>>([]);
const channel = computed(() =>
  state.value?.extensions.find((item) => item.id === props.room.channel),
);
const modelMissing = computed(
  () =>
    draft.value?.model &&
    !models.value.some((m) => m.model === draft.value?.model),
);

function reset() {
  draft.value = JSON.parse(JSON.stringify(props.room.settings));
  context.value = JSON.stringify(props.room.settings.context ?? {}, null, 2);
  dirty.value = false;
  error.value = "";
}

async function loadModels() {
  if (modelsLoading.value) return;
  modelsLoading.value = true;
  modelError.value = "";
  try {
    models.value = await host.listRoomModels();
  } catch (e) {
    modelError.value = String(e);
  } finally {
    modelsLoading.value = false;
  }
}

function advancedToggle(event: Event) {
  if ((event.target as HTMLDetailsElement).open && !models.value.length)
    void loadModels();
}

async function save() {
  if (!draft.value || saving.value) return;
  saving.value = true;
  error.value = "";
  try {
    const values: unknown = JSON.parse(context.value);
    if (
      !values ||
      Array.isArray(values) ||
      typeof values !== "object" ||
      Object.values(values).some((v) => typeof v !== "string")
    )
      throw Error(t("上下文必须是键值均为文本的 JSON 对象"));
    const settings: RoomSettings = JSON.parse(JSON.stringify(draft.value));
    settings.context = values as Record<string, string>;
    await host.updateRoomSettings(props.room.id, settings);
    dirty.value = false;
    say(t("Room 设置已保存，下次任务生效"));
    emit("saved");
  } catch (e) {
    error.value = String(e);
  } finally {
    saving.value = false;
  }
}

watch(
  () => props.room.settings,
  () => {
    if (!dirty.value) reset();
  },
);
reset();
defineExpose({ dirty, saving });
</script>

<template>
  <form
    v-if="draft"
    id="room-settings"
    class="room-settings-form"
    @submit.prevent="save"
    @input="dirty = true"
    @change="dirty = true"
  >
    <div class="room-panel-body">
      <p v-if="error" class="room-error" role="alert">{{ error }}</p>
      <label class="room-field-label" for="room-name">{{
        t("会话名称")
      }}</label>
      <input
        id="room-name"
        class="field"
        v-model="draft.name"
        :placeholder="room.conversation"
      />
      <details class="room-identity">
        <summary>{{ t("会话标识") }}</summary>
        <code>{{ room.conversation }}</code>
        <span>{{ room.source }}</span>
      </details>
      <template v-if="channel?.sendTargetLabel">
        <label class="room-field-label" for="room-send-target">{{
          channel.sendTargetLabel
        }}</label>
        <input id="room-send-target" class="field" v-model="draft.sendTarget" />
      </template>
      <fieldset class="room-setting-section">
        <legend>{{ t("Agent 处理") }}</legend>
        <label class="room-check-row">
          <span>{{ t("Agent 自动处理") }}</span>
          <input
            id="room-automatic"
            type="checkbox"
            v-model="draft.automatic"
          />
        </label>
        <details class="room-trigger">
          <summary>{{ t("触发条件") }}</summary>
          <label class="room-check-row">
            <span>{{ t("Chat 模式") }}</span>
            <input
              id="room-trigger-chat"
              type="checkbox"
              v-model="draft.trigger.chat.enabled"
            />
          </label>
          <p class="room-help">
            {{
              t(
                "私聊直接触发；群聊中真实 @ 当前账号时触发，仅 @ 其他人不会触发。",
              )
            }}
          </p>
          <label class="room-check-row">
            <span>{{ t("规则匹配") }}</span>
            <input
              id="room-trigger-regex"
              type="checkbox"
              v-model="draft.trigger.regex.enabled"
            />
          </label>
          <template v-if="draft.trigger.regex.enabled">
            <label class="room-field-label" for="room-regex-pattern">{{
              t("正则表达式")
            }}</label>
            <input
              id="room-regex-pattern"
              class="field code"
              v-model="draft.trigger.regex.pattern"
              autocomplete="off"
              spellcheck="false"
            />
            <label class="room-check-row">
              <span>{{ t("允许匹配自己发送的消息") }}</span>
              <input
                id="room-regex-self"
                type="checkbox"
                v-model="draft.trigger.regex.self"
              />
            </label>
          </template>
          <p class="room-help">
            {{
              t("两种模式可以单独开启，也可以同时开启；任一模式命中即触发。")
            }}
          </p>
        </details>
        <details
          id="room-advanced"
          class="room-advanced"
          @toggle="advancedToggle"
        >
          <summary>{{ t("高级配置") }}</summary>
          <label class="room-field-label" for="room-model">{{
            t("模型")
          }}</label>
          <select
            id="room-model"
            class="field"
            v-model="draft.model"
            :disabled="modelsLoading"
          >
            <option value="">{{ t("Codex 默认模型") }}</option>
            <option v-if="modelMissing" :value="draft.model">
              {{ draft.model }} · {{ t("当前列表不可用") }}
            </option>
            <option
              v-for="model in models"
              :key="model.model"
              :value="model.model"
            >
              {{ model.displayName || model.model }}
            </option>
          </select>
          <p v-if="modelError" class="room-error" role="alert">
            {{ modelError }}
            <button class="link" type="button" @click="loadModels">
              {{ t("重新获取模型") }}
            </button>
          </p>
          <label class="room-field-label" for="room-prompt">Room prompt</label>
          <textarea
            id="room-prompt"
            class="field"
            rows="5"
            v-model="draft.prompt"
          ></textarea>
          <label class="room-field-label" for="room-context">{{
            t("会话上下文")
          }}</label>
          <textarea
            id="room-context"
            class="field"
            rows="4"
            v-model="context"
          ></textarea>
        </details>
      </fieldset>
    </div>
    <footer class="room-panel-footer">
      <button
        class="btn"
        type="button"
        :disabled="saving || !dirty"
        @click="reset"
      >
        {{ t("撤销修改") }}
      </button>
      <button
        class="btn primary"
        id="room-settings-save"
        :disabled="saving || !dirty"
      >
        {{ saving ? t("正在保存…") : t("保存设置") }}
      </button>
    </footer>
  </form>
</template>
