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
const keywords = ref("");
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
  keywords.value = (props.room.settings.trigger.keywords ?? []).join("\n");
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
    settings.trigger.keywords = keywords.value
      .split("\n")
      .map((s) => s.trim())
      .filter(Boolean);
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
          <div class="room-checks">
            <label
              ><input
                id="room-trigger-all"
                type="checkbox"
                v-model="draft.trigger.all"
              />{{ t("所有新消息") }}</label
            >
            <label
              ><input type="checkbox" v-model="draft.trigger.direct" />{{
                t("私聊消息")
              }}</label
            >
            <label
              ><input type="checkbox" v-model="draft.trigger.mention" />{{
                t("群聊提及本人")
              }}</label
            >
            <label
              ><input type="checkbox" v-model="draft.trigger.mentionAll" />{{
                t("群聊提及所有人")
              }}</label
            >
          </div>
          <label class="room-field-label" for="room-keywords">{{
            t("群聊关键词，每行一个")
          }}</label>
          <textarea
            id="room-keywords"
            class="field"
            rows="3"
            v-model="keywords"
          ></textarea>
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
          <label class="room-field-label" for="room-mode">{{
            t("处理方式")
          }}</label>
          <select id="room-mode" class="field" v-model="draft.mode">
            <option value="conversation">{{ t("持续对话") }}</option>
            <option value="snapshot">{{ t("最新状态分析") }}</option>
          </select>
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
      <fieldset class="room-setting-section">
        <legend>{{ t("外发授权") }}</legend>
        <label class="room-check-row">
          <span>{{ t("允许向外部会话发送消息") }}</span>
          <input
            id="room-outbound"
            type="checkbox"
            v-model="draft.allowOutbound"
            :disabled="!channel?.canSend"
          />
        </label>
        <p class="room-help">
          {{
            channel?.canSend
              ? t("自动处理与外发授权独立。设置修改从下一次任务生效。")
              : t("此渠道的回复仅在 Room 展示，不向外部平台发送。")
          }}
        </p>
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
