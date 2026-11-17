<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { host } from "../host";
import { ask, navigate, shell, state } from "../state/desktop";
import { locale, t } from "../i18n";
import Icon from "./Icon.vue";
import RoomSettings from "./RoomSettings.vue";
import type { RoomSummary } from "../generated/mygo";

const rooms = ref<RoomSummary[]>([]);
const query = ref("");
const settings = ref<InstanceType<typeof RoomSettings>>();
const error = ref("");
const loading = ref(false);
let active = true;
let generation = 0;
let pending = false;
let refreshing = false;

const channelName = (id: string) =>
  state.value?.extensions.find((x) => x.id === id)?.name ?? id;
const roomName = (room: RoomSummary) =>
  room.settings.name ||
  room.presentation.name ||
  room.settings.sendTarget ||
  room.conversation;
const filteredRooms = computed(() => {
  const search = query.value.trim().toLocaleLowerCase();
  return rooms.value.filter((room) =>
    [
      roomName(room),
      room.conversation,
      room.id,
      channelName(room.channel),
    ].some((text) => text.toLocaleLowerCase().includes(search)),
  );
});
const room = computed(() => rooms.value.find((item) => item.id === shell.room));

function activity(value: string) {
  return new Date(value).toLocaleString(locale.value, {
    month: "numeric",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
    hour12: false,
  });
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
async function reload() {
  pending = true;
  if (refreshing) return;
  refreshing = true;
  loading.value = !rooms.value.length;
  let request = generation;
  try {
    while (pending && active) {
      pending = false;
      request = ++generation;
      const list = await host.listRooms();
      if (!active || request !== generation) continue;
      rooms.value = list;
      error.value = "";
      if (!list.some((item) => item.id === shell.room) && list.length)
        navigate("messages", { room: list[0].id }, false);
    }
  } catch (e) {
    if (active && request === generation) error.value = String(e);
  } finally {
    refreshing = false;
    loading.value = false;
  }
}
watch(state, () => void reload());
onMounted(() => void reload());
onBeforeUnmount(() => {
  active = false;
  generation++;
});
</script>

<template>
  <div class="split" id="messages-page">
    <aside
      class="side room-sidebar"
      id="module-sidebar"
      :aria-label="t('Room 列表')"
    >
      <div class="room-list-tools">
        <div class="room-search-row">
          <label class="search"
            ><Icon name="search" /><input
              id="room-search"
              class="field"
              v-model="query"
              :placeholder="t('搜索 Room')"
              :aria-label="t('搜索 Room')"
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
      </div>
      <div class="room-list">
        <button
          v-for="item in filteredRooms"
          :key="item.id"
          class="room-item"
          :data-room-id="item.id"
          :aria-current="shell.room === item.id ? 'true' : undefined"
          :title="roomName(item)"
          @click="choose(item.id)"
        >
          <span class="room-item-heading"
            ><strong>{{ roomName(item) }}</strong
            ><time :datetime="item.updatedAt">{{
              activity(item.updatedAt)
            }}</time></span
          >
          <span class="room-source"
            ><span>{{ channelName(item.channel) }}</span
            ><span>{{
              item.settings.automatic ? t("自动处理") : t("未自动处理")
            }}</span></span
          >
        </button>
        <p v-if="!filteredRooms.length" class="room-empty-list">
          {{
            rooms.length
              ? t("没有匹配的 Room")
              : t("渠道收到消息后，Room 会显示在这里。")
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
        {{ t("{n} 个 Room", { n: filteredRooms.length }) }}
      </footer>
    </aside>
    <section class="main room-main" :aria-label="t('Room 设置')">
      <p v-if="error" class="room-page-error" role="alert">
        {{ error }}
        <button class="link" @click="reload">{{ t("重新加载") }}</button>
      </p>
      <template v-if="room">
        <header class="room-header">
          <h1 :title="roomName(room)">{{ roomName(room) }}</h1>
          <p>
            {{ channelName(room.channel) }} ·
            {{ t("最近活动 {time}", { time: activity(room.updatedAt) }) }}
          </p>
        </header>
        <RoomSettings
          :key="room.id"
          ref="settings"
          :room="room"
          @saved="reload"
        />
      </template>
      <p v-else class="room-empty-state">
        {{ loading ? t("正在加载 Room…") : t("选择一个 Room 编辑设置。") }}
      </p>
    </section>
  </div>
</template>
