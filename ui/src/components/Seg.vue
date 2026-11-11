<script setup lang="ts" generic="T extends string">
import { t } from "../i18n";
const props = defineProps<{
  modelValue: T;
  options: readonly (readonly [T, string])[];
  label: string;
  idPrefix?: string;
}>();
const emit = defineEmits<{ "update:modelValue": [value: T] }>();
function key(event: KeyboardEvent, index: number) {
  if (event.key !== "ArrowRight" && event.key !== "ArrowLeft") return;
  event.preventDefault();
  const count = props.options.length;
  const next = (index + (event.key === "ArrowRight" ? 1 : -1) + count) % count;
  emit("update:modelValue", props.options[next][0]);
  const group = (event.currentTarget as HTMLElement).parentElement!;
  group.querySelectorAll<HTMLButtonElement>("button")[next]?.focus();
}
</script>
<template>
  <div class="seg" role="radiogroup" :aria-label="t(label)">
    <button
      v-for="([value, text], index) in options"
      :key="value"
      :id="idPrefix ? idPrefix + value : undefined"
      type="button"
      role="radio"
      :aria-checked="value === modelValue"
      :tabindex="value === modelValue ? 0 : -1"
      @click="emit('update:modelValue', value)"
      @keydown="key($event, index)"
    >
      {{ t(text) }}
    </button>
  </div>
</template>
