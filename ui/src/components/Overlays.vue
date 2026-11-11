<script setup lang="ts">
import { ref, watch, nextTick, onMounted, onUnmounted } from "vue";
import { menu, modal, closeDialog, notice, problem } from "../state/desktop";
import { t } from "../i18n";
import Icon from "./Icon.vue";
const dialog = ref<HTMLDialogElement>();
const input = ref("");
watch(modal, async (value) => {
  input.value = value?.input ?? "";
  await nextTick();
  if (value && !dialog.value?.open) dialog.value?.showModal();
  if (!value) dialog.value?.close();
});
async function invoke(action: (() => unknown) | undefined) {
  try {
    await action?.();
  } catch (error) {
    problem(error);
  }
}
function outside(event: PointerEvent) {
  if (event.button !== 0) return;
  if (!(event.target instanceof Element) || !event.target.closest("#popup"))
    menu.value = null;
}
onMounted(() => document.addEventListener("pointerdown", outside, true));
onUnmounted(() => document.removeEventListener("pointerdown", outside, true));
</script>
<template>
  <div
    id="popup"
    class="popup"
    role="menu"
    :hidden="!menu"
    :style="menu ? { left: menu.x + 'px', top: menu.y + 'px' } : {}"
  >
    <div v-for="item in menu?.entries" :key="item.label" class="menu-row">
      <button
        role="menuitem"
        :disabled="item.disabled"
        :data-action="item.id ?? ''"
        @click="
          menu = null;
          invoke(item.run);
        "
      >
        {{ t(item.label) }}
      </button>
      <button
        v-if="item.pin"
        class="icon-button"
        data-action="pin-navigation"
        :title="t(item.pinned ? '取消固定' : '固定到侧边栏')"
        @click="
          invoke(item.pin);
          menu = null;
        "
      >
        <Icon :name="item.pinned ? 'pin-off' : 'pin'" />
      </button>
    </div>
  </div>
  <dialog id="dialog" ref="dialog" @cancel="closeDialog">
    <template v-if="modal">
      <h2>{{ t(modal.title) }}</h2>
      <p v-if="modal.message" style="white-space: pre-wrap">
        {{ modal.message }}
      </p>
      <input
        v-if="modal.input !== undefined"
        :id="modal.inputId"
        v-model="input"
        placeholder="example.com"
        @input="modal.input = input"
      />
      <footer>
        <button
          v-for="action in modal.actions"
          :key="action.label"
          @click="invoke(action.run)"
        >
          {{ t(action.label) }}
        </button>
      </footer>
    </template>
  </dialog>
  <div id="notice" role="status" :hidden="!notice">{{ notice }}</div>
</template>
