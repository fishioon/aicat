<script setup lang="ts">
import {
  computed,
  nextTick,
  onBeforeUnmount,
  onMounted,
  ref,
  watch,
} from "vue";
import { host } from "../host";
import { ask, modal, navigate, shell, state } from "../state/desktop";
import { locale, t } from "../i18n";
import logo from "../../../assets/logo.svg";
import Icon from "./Icon.vue";
import RoomSettings from "./RoomSettings.vue";
import type {
  Message,
  RoomIssue,
  RoomSummary,
  RoomView,
} from "../generated/mygo";

const deliveryLabels: Record<string, string> = {
  pending: "发送中",
  sent: "渠道确认发送",
  failed: "发送失败",
  unknown: "发送结果不明，请勿重发",
};
const taskLabels: Record<string, string> = {
  pending: "等待处理",
  running: "处理中",
  completed: "处理完成",
  failed: "处理失败",
  unknown: "执行结果不明",
  superseded: "已由更新状态替代",
  skipped: "已跳过",
};
const rooms = ref<RoomSummary[]>([]);
const view = ref<RoomView>();
const query = ref("");
const filter = ref<"all" | "issues">("all");
const panel = ref<"" | "settings" | "activity">("");
const settings = ref<InstanceType<typeof RoomSettings>>();
const closeButton = ref<HTMLButtonElement>();
const error = ref("");
const loading = ref(false);
let active = true;
let generation = 0;
let pending = false;
let refreshing = false;

const channelName = (id: string) =>
  state.value?.extensions.find((x) => x.id === id)?.name ?? id;
const channel = computed(() =>
  state.value?.extensions.find((x) => x.id === view.value?.channel),
);
const roomName = (room: RoomSummary) =>
  room.settings.name || room.settings.sendTarget || room.conversation;
const filteredRooms = computed(() => {
  const search = query.value.trim().toLocaleLowerCase();
  return rooms.value.filter(
    (room) =>
      (filter.value === "all" || room.issues.length > 0) &&
      [roomName(room), room.conversation, channelName(room.channel)].some(
        (text) => text.toLocaleLowerCase().includes(search),
      ),
  );
});
const tasks = computed(() => [...(view.value?.tasks ?? [])].reverse());
const latestStates = computed(() => {
  const result = new Map<string, string>();
  for (const message of view.value?.messages ?? [])
    if (!message.outgoing && message.context_key && message.msgid)
      result.set(message.context_key, message.msgid);
  return result;
});

function compact(value: string, limit = 22) {
  return value.length > limit
    ? value.slice(0, limit - 8) + "…" + value.slice(-7)
    : value;
}
function roomIcon(room: RoomSummary) {
  return room.conversation.startsWith("S:") ? "person" : "people";
}
function date(value: string | number) {
  return new Date(value).toLocaleDateString(locale.value);
}
function time(value: string | number) {
  return new Date(value).toLocaleTimeString(locale.value, {
    hour: "2-digit",
    minute: "2-digit",
    hour12: false,
  });
}
function activity(value: string) {
  return date(value) === date(Date.now()) ? time(value) : date(value);
}
function issueLabel(issue: RoomIssue) {
  return t(
    (issue.kind === "delivery" ? deliveryLabels : taskLabels)[issue.status] ||
      issue.status,
  );
}
function isEarlier(message: Message) {
  return (
    message.outgoing &&
    message.context_key &&
    message.reply_to &&
    latestStates.value.has(message.context_key) &&
    latestStates.value.get(message.context_key) !== message.reply_to
  );
}
function isStructured(message: Message) {
  if (message.outgoing) return false;
  try {
    const value: unknown = JSON.parse(message.text.content);
    return typeof value === "object" && value !== null;
  } catch {
    return false;
  }
}
function preview(room: RoomSummary) {
  try {
    if (typeof JSON.parse(room.lastMessage) === "object") return t("业务状态");
  } catch {}
  return room.lastMessage || t("暂无消息");
}
function originalLabel(id: string) {
  const original = view.value?.messages.find((m) => m.msgid === id);
  return original
    ? (
        original.summary ||
        (isStructured(original) ? t("业务状态") : original.text.content) ||
        t("非文本消息")
      ).slice(0, 60)
    : t("较早的消息");
}
function cleanThen(action: () => void) {
  if (settings.value?.saving) return;
  if (settings.value?.dirty)
    ask(t("放弃未保存的设置"), t("此操作将放弃当前修改。"), action);
  else action();
}
function choose(id: string) {
  if (shell.room === id) return;
  cleanThen(() => navigate("messages", { room: id }));
}
function addChannel() {
  cleanThen(() => {
    const channels =
      state.value?.extensions.filter((item) =>
        item.categories.includes("消息渠道"),
      ) ?? [];
    const target = channels.find((item) => !item.enabled) ?? channels[0];
    navigate("extensions", {
      extensionQuery: "tag:channel",
      extension: target?.id ?? "",
    });
  });
}
function changeFilter(next: "all" | "issues") {
  cleanThen(() => {
    panel.value = "";
    filter.value = next;
    if (
      !filteredRooms.value.some((room) => room.id === shell.room) &&
      filteredRooms.value.length
    )
      navigate("messages", { room: filteredRooms.value[0].id });
  });
}
function closePanel() {
  cleanThen(() => {
    const button =
      panel.value === "settings"
        ? "room-settings-toggle"
        : "room-activity-toggle";
    panel.value = "";
    void nextTick(() => document.getElementById(button)?.focus());
  });
}
function openPanel(next: "settings" | "activity") {
  if (panel.value === next) {
    closePanel();
    return;
  }
  cleanThen(() => {
    panel.value = next;
  });
}
function showOriginal(id: string) {
  cleanThen(() => {
    panel.value = "";
    void nextTick(() =>
      document.getElementById("room-message-" + id)?.scrollIntoView({
        block: "center",
        behavior: matchMedia("(prefers-reduced-motion: reduce)").matches
          ? "instant"
          : "smooth",
      }),
    );
  });
}
async function reload() {
  pending = true;
  if (refreshing) return;
  refreshing = true;
  loading.value = !view.value;
  let request = generation;
  try {
    while (pending && active) {
      pending = false;
      request = ++generation;
      const list = await host.listRooms();
      if (!active || request !== generation) continue;
      rooms.value = list;
      if (!shell.room && list.length)
        navigate("messages", { room: list[0].id }, false);
      const id = shell.room;
      if (!id) {
        view.value = undefined;
        continue;
      }
      const next = await host.readRoomByID(id);
      if (!active || request !== generation || shell.room !== id) continue;
      view.value = next;
      error.value = "";
    }
  } catch (e) {
    if (active && request === generation) error.value = String(e);
  } finally {
    refreshing = false;
    loading.value = false;
  }
}
function onEscape(event: KeyboardEvent) {
  if (event.key === "Escape" && panel.value && !modal.value) {
    event.preventDefault();
    closePanel();
  }
}
watch(
  () => shell.room,
  () => {
    generation++;
    panel.value = "";
    view.value = undefined;
    void reload();
  },
);
watch(state, () => void reload());
watch(panel, () => {
  if (panel.value) void nextTick(() => closeButton.value?.focus());
});
onMounted(() => {
  void reload();
  document.addEventListener("keydown", onEscape);
});
onBeforeUnmount(() => {
  active = false;
  generation++;
  document.removeEventListener("keydown", onEscape);
});
</script>

<template>
  <div class="split" id="messages-page">
    <aside
      class="side room-sidebar"
      id="module-sidebar"
      :aria-label="t('会话列表')"
    >
      <div class="room-list-tools">
        <div class="room-search-row">
          <label class="search"
            ><Icon name="search" /><input
              id="room-search"
              class="field"
              v-model="query"
              :placeholder="t('搜索会话')"
              :aria-label="t('搜索会话')"
          /></label>
          <button
            class="icon-btn"
            id="room-add-channel"
            :aria-label="t('添加消息渠道')"
            :title="t('添加消息渠道')"
            @click="addChannel"
          >
            <Icon name="plus" />
          </button>
        </div>
        <div class="room-filters" :aria-label="t('会话筛选')">
          <button
            id="room-filter-all"
            :aria-pressed="filter === 'all'"
            @click="changeFilter('all')"
          >
            {{ t("全部会话") }}
          </button>
          <button
            id="room-filter-issues"
            :aria-pressed="filter === 'issues'"
            @click="changeFilter('issues')"
          >
            {{ t("异常") }}
          </button>
        </div>
      </div>
      <div class="room-list">
        <button
          v-for="room in filteredRooms"
          :key="room.id"
          class="room-item"
          :data-room-id="room.id"
          :aria-current="shell.room === room.id ? 'true' : undefined"
          :title="roomName(room)"
          @click="choose(room.id)"
        >
          <span class="room-avatar" aria-hidden="true"
            ><Icon :name="roomIcon(room)"
          /></span>
          <span class="room-item-copy">
            <span class="room-item-heading"
              ><strong>{{ roomName(room) }}</strong
              ><time :datetime="room.updatedAt">{{
                activity(room.updatedAt)
              }}</time></span
            >
            <span class="room-preview">{{ preview(room) }}</span>
            <span class="room-source"
              ><span>{{ channelName(room.channel) }}</span
              ><span v-if="room.issues.length" class="room-issue-label"
                ><Icon name="warning" />{{ issueLabel(room.issues[0]) }}</span
              ></span
            >
          </span>
        </button>
        <p v-if="!filteredRooms.length" class="room-empty-list">
          {{
            rooms.length
              ? t("没有匹配的会话")
              : t("渠道收到消息后，会话会显示在这里。")
          }}
          <button
            v-if="!rooms.length"
            class="btn"
            id="room-empty-add-channel"
            @click="addChannel"
          >
            {{ t("添加消息渠道") }}
          </button>
        </p>
      </div>
      <footer class="room-list-footer">
        {{ t("{n} 个会话", { n: filteredRooms.length }) }}
      </footer>
    </aside>
    <section class="main room-main" :aria-label="t('当前会话')">
      <p v-if="error" class="room-page-error" role="alert">
        {{ error }}
        <button class="link" @click="reload">{{ t("重新加载") }}</button>
      </p>
      <template v-if="view">
        <header class="room-header">
          <div>
            <h1 :title="roomName(view)">{{ roomName(view) }}</h1>
            <p>
              {{ channelName(view.channel) }} ·
              {{
                channel?.enabled
                  ? t("最近活动 {time}", { time: activity(view.updatedAt) })
                  : t("已停用，历史仍可查看")
              }}
            </p>
          </div>
          <button
            class="icon-btn"
            id="room-settings-toggle"
            :aria-label="t('Room 设置')"
            :title="t('Room 设置')"
            :aria-expanded="panel === 'settings'"
            aria-controls="room-panel"
            @click="openPanel('settings')"
          >
            <Icon name="settings" />
          </button>
        </header>
        <div
          v-if="view.issues.length"
          id="room-issue-banner"
          class="room-issue-banner"
        >
          <span
            ><Icon name="warning" />{{ issueLabel(view.issues[0])
            }}<span v-if="view.issues.length > 1">
              · {{ t("{n} 项异常", { n: view.issues.length }) }}</span
            ></span
          >
          <button
            class="link"
            id="room-issue-details"
            @click="openPanel('activity')"
          >
            {{ t("查看详情") }}<Icon name="chev" />
          </button>
        </div>
        <div
          id="room-messages"
          class="room-messages"
          :aria-label="t('会话消息')"
        >
          <template
            v-for="(message, index) in view.messages"
            :key="`${message.msgid}-${index}`"
          >
            <p
              v-if="
                message.msgtime != null &&
                (index === 0 ||
                  date(message.msgtime) !==
                    date(view.messages[index - 1].msgtime ?? message.msgtime))
              "
              class="room-date"
            >
              {{ date(message.msgtime) }}
            </p>
            <article
              class="room-message"
              :class="{ outgoing: message.outgoing }"
              :data-message-id="message.msgid"
              :id="'room-message-' + message.msgid"
            >
              <span class="room-avatar" aria-hidden="true"
                ><img v-if="message.outgoing" :src="logo" alt="" /><Icon
                  v-else
                  name="person"
              /></span>
              <div class="room-message-content">
                <div class="room-message-meta">
                  <span :title="message.from">{{
                    message.outgoing
                      ? "aicat"
                      : compact(message.from || t("渠道消息"))
                  }}</span
                  ><span v-if="message.outgoing" class="room-agent-label"
                    >Agent</span
                  ><time
                    v-if="message.msgtime != null"
                    :datetime="new Date(message.msgtime).toISOString()"
                    >{{ time(message.msgtime) }}</time
                  >
                </div>
                <div class="room-bubble">
                  <button
                    v-if="message.reply_to"
                    class="room-quote"
                    @click="showOriginal(message.reply_to)"
                  >
                    {{ t("回复于") }}：{{ originalLabel(message.reply_to) }}
                  </button>
                  <div v-if="message.summary" class="room-message-text">
                    {{ message.summary }}
                  </div>
                  <details v-if="isStructured(message)" class="room-business">
                    <summary>{{ t("业务状态") }}</summary>
                    <pre>{{ message.text.content }}</pre>
                  </details>
                  <div v-else class="room-message-text">
                    {{ message.text.content || t("非文本消息") }}
                  </div>
                </div>
                <p v-if="message.historical" class="room-message-note">
                  {{ t("历史记录，不触发自动处理") }}
                </p>
                <p v-if="isEarlier(message)" class="room-message-note">
                  {{ t("基于较早局面，后续状态已有变化") }}
                </p>
                <p
                  v-if="message.delivery"
                  class="room-delivery"
                  :class="{
                    'room-error': ['failed', 'unknown'].includes(
                      message.delivery.status,
                    ),
                  }"
                  :data-delivery-status="message.delivery.status"
                >
                  <Icon
                    v-if="
                      ['failed', 'unknown'].includes(message.delivery.status)
                    "
                    name="warning"
                  />{{
                    t(
                      deliveryLabels[message.delivery.status] ||
                        message.delivery.status,
                    )
                  }}<span v-if="message.delivery.error">
                    · {{ message.delivery.error }}</span
                  >
                </p>
                <p v-else-if="message.outgoing" class="room-delivery">
                  {{
                    message.action === "publish"
                      ? t("已保存到 Room")
                      : t("外发记录")
                  }}
                </p>
              </div>
            </article>
          </template>
          <p v-if="!view.messages.length" class="room-empty-list">
            {{ t("暂无消息") }}
          </p>
        </div>
        <footer class="room-records-footer">
          <span :title="t('仅展示最近 200 条消息')">{{
            t("最近 {n} 条消息", { n: view.messages.length })
          }}</span
          ><button
            class="link"
            id="room-activity-toggle"
            :aria-expanded="panel === 'activity'"
            aria-controls="room-panel"
            @click="openPanel('activity')"
          >
            {{ t("处理记录") }}<Icon name="chev" />
          </button>
        </footer>
        <template v-if="panel">
          <div class="room-panel-shade" @click="closePanel"></div>
          <section
            class="room-panel"
            id="room-panel"
            aria-labelledby="room-panel-title"
          >
            <header class="room-panel-header">
              <h2 id="room-panel-title">
                {{ panel === "settings" ? t("Room 设置") : t("处理记录") }}
              </h2>
              <button
                class="icon-btn"
                ref="closeButton"
                :aria-label="t('关闭')"
                @click="closePanel"
              >
                <Icon name="x" />
              </button>
            </header>
            <RoomSettings
              v-if="panel === 'settings'"
              :key="view.id"
              ref="settings"
              :room="view"
              @saved="
                panel = '';
                reload();
              "
            />
            <div v-else class="room-panel-body" id="room-tasks">
              <section
                v-if="view.issues.length"
                class="room-issues"
                :aria-label="t('异常详情')"
              >
                <article
                  v-for="issue in view.issues"
                  :key="issue.kind + issue.id"
                  class="room-issue-detail"
                  :data-issue-id="issue.id"
                >
                  <h3><Icon name="warning" />{{ issueLabel(issue) }}</h3>
                  <p v-if="issue.error">{{ issue.error }}</p>
                  <code>{{ issue.id }}</code>
                  <p v-if="issue.kind === 'delivery'" class="room-help">
                    {{ t("查看渠道连接与发送回执；结果不明时请勿重复发送。") }}
                  </p>
                  <button
                    v-if="
                      view.messages.some((m) => m.msgid === issue.messageId)
                    "
                    class="link"
                    @click="showOriginal(issue.messageId)"
                  >
                    {{ t("查看关联消息") }}
                  </button>
                </article>
              </section>
              <div
                v-for="task in tasks"
                :key="task.id"
                class="room-task"
                :data-task-id="task.id"
              >
                <div class="room-task-heading">
                  <strong
                    :class="{
                      'room-error': ['failed', 'unknown'].includes(task.status),
                    }"
                    >{{ t(taskLabels[task.status] || task.status) }}</strong
                  ><time :datetime="task.createdAt"
                    >{{ date(task.createdAt) }} {{ time(task.createdAt) }}</time
                  >
                </div>
                <p>{{ task.trigger.summary || task.trigger.text.content }}</p>
                <p v-if="task.error" class="room-error">{{ task.error }}</p>
                <details>
                  <summary>{{ t("执行信息") }}</summary>
                  <dl>
                    <dt>Agent</dt>
                    <dd>Codex app-server</dd>
                    <dt>{{ t("模型") }}</dt>
                    <dd>{{ task.settings.model || t("Codex 默认模型") }}</dd>
                    <dt>{{ t("任务标识") }}</dt>
                    <dd>{{ task.id }}</dd>
                  </dl>
                </details>
              </div>
              <p v-if="!tasks.length" class="room-help">
                {{ t("本次运行暂无处理记录；任务记录不跨应用重启保留。") }}
              </p>
              <p class="room-help">
                {{ t("发送状态以渠道回执为准，不代表对端已读。") }}
              </p>
            </div>
          </section>
        </template>
      </template>
      <p v-else class="room-empty-state">
        {{ loading ? t("正在加载会话…") : t("选择一个会话查看消息。") }}
      </p>
    </section>
  </div>
</template>
