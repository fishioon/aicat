<script setup lang="ts">
const props = defineProps<{
  id: string;
  axis: "x" | "y";
  container: string;
  modelValue: number;
  label: string;
}>();
const emit = defineEmits<{ "update:modelValue": [value: number] }>();
function update(value: number) {
  emit("update:modelValue", Math.max(20, Math.min(80, value)));
}
function down(event: PointerEvent) {
  (event.currentTarget as HTMLElement).setPointerCapture(event.pointerId);
  (event.currentTarget as HTMLElement).classList.add("dragging");
}
function move(event: PointerEvent) {
  if (!(event.currentTarget as HTMLElement).hasPointerCapture(event.pointerId))
    return;
  const rect = document.querySelector(props.container)!.getBoundingClientRect();
  update(
    props.axis === "x"
      ? ((event.clientX - rect.left) / rect.width) * 100
      : ((event.clientY - rect.top) / rect.height) * 100,
  );
}
function up(event: PointerEvent) {
  (event.currentTarget as HTMLElement).releasePointerCapture(event.pointerId);
  (event.currentTarget as HTMLElement).classList.remove("dragging");
}
function key(event: KeyboardEvent) {
  if (!["ArrowLeft", "ArrowUp", "ArrowRight", "ArrowDown"].includes(event.key))
    return;
  event.preventDefault();
  update(
    props.modelValue + (["ArrowLeft", "ArrowUp"].includes(event.key) ? -1 : 1),
  );
}
</script>
<template>
  <div
    :id="id"
    class="splitter"
    role="separator"
    tabindex="0"
    :aria-label="label"
    :aria-orientation="axis === 'x' ? 'vertical' : 'horizontal'"
    :aria-valuenow="Math.round(modelValue)"
    aria-valuemin="20"
    aria-valuemax="80"
    @pointerdown="down"
    @pointermove="move"
    @pointerup="up"
    @keydown="key"
  ></div>
</template>
