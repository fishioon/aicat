<script setup lang="ts">
import { ref, watch, nextTick, onMounted, onUnmounted } from "vue";
import {
  menu,
  modal,
  closeDialog,
  notice,
  noticeKind,
  problem,
} from "../state/desktop";
import { t } from "../i18n";
const dialog = ref<HTMLDialogElement>();
const popupRef = ref<HTMLElement>();
const input = ref("");
watch(modal, async (value) => {
  input.value = value?.input ?? "";
  await nextTick();
  if (value && !dialog.value?.open) dialog.value?.showModal();
  if (!value) dialog.value?.close();
});
let returnFocus: HTMLElement | null = null;
watch(menu, async (value, old) => {
  if (value && !old) returnFocus = document.activeElement as HTMLElement;
  await nextTick();
  if (value)
    popupRef.value
      ?.querySelector<HTMLButtonElement>("button:not(:disabled)")
      ?.focus();
  else if (old) returnFocus?.focus?.({ preventScroll: true });
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
function key(event: KeyboardEvent) {
  const buttons = [
    ...popupRef.value!.querySelectorAll<HTMLButtonElement>(
      "button:not(:disabled)",
    ),
  ];
  const index = buttons.indexOf(document.activeElement as HTMLButtonElement);
  if (event.key === "ArrowDown" || event.key === "ArrowUp") {
    event.preventDefault();
    const step = event.key === "ArrowDown" ? 1 : -1;
    buttons[(index + step + buttons.length) % buttons.length]?.focus();
  } else if (event.key === "Tab") menu.value = null;
}
onMounted(() => document.addEventListener("pointerdown", outside, true));
onUnmounted(() => document.removeEventListener("pointerdown", outside, true));
</script>
<template>
  <div
    v-if="menu"
    id="popup"
    ref="popupRef"
    class="menu"
    role="menu"
    :style="{ left: menu.x + 'px', top: menu.y + 'px' }"
    @keydown="key"
  >
    <button
      v-for="item in menu.entries"
      :key="item.label"
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
  </div>
  <dialog id="dialog" ref="dialog" @cancel="closeDialog">
    <template v-if="modal">
      <h2>{{ t(modal.title) }}</h2>
      <p v-if="modal.message">{{ modal.message }}</p>
      <input
        v-if="modal.input !== undefined"
        :id="modal.inputId"
        v-model="input"
        class="field"
        style="width: 100%; margin-top: 12px"
        :placeholder="t(modal.placeholder ?? 'example.com')"
        @input="modal.input = input"
      />
      <footer>
        <button
          v-for="(action, index) in modal.actions"
          :key="action.label"
          class="btn"
          :class="{ primary: index === modal.actions.length - 1 }"
          @click="invoke(action.run)"
        >
          {{ t(action.label) }}
        </button>
      </footer>
    </template>
  </dialog>
  <div
    id="notice"
    class="toast"
    :class="noticeKind"
    role="status"
    aria-live="polite"
    :hidden="!notice"
  >
    {{ notice }}
  </div>
</template>
