<script setup lang="ts">
const props = withDefaults(
  defineProps<{
    id: string;
    axis: "x" | "y";
    container: string;
    modelValue: number;
    label: string;
    unit?: "percent" | "px";
    min?: number;
    max?: number;
  }>(),
  { unit: "percent", min: 20, max: 80 },
);
const emit = defineEmits<{ "update:modelValue": [value: number] }>();
function update(value: number) {
  emit("update:modelValue", Math.max(props.min, Math.min(props.max, value)));
}
function down(event: PointerEvent) {
  if (event.button !== 0 || !event.isPrimary) return;
  event.preventDefault();
  (event.currentTarget as HTMLElement).setPointerCapture(event.pointerId);
  (event.currentTarget as HTMLElement).classList.add("dragging");
}
function move(event: PointerEvent) {
  if (!(event.currentTarget as HTMLElement).hasPointerCapture(event.pointerId))
    return;
  const rect = document.querySelector(props.container)!.getBoundingClientRect();
  const offset =
    props.axis === "x" ? event.clientX - rect.left : event.clientY - rect.top;
  const size = props.axis === "x" ? rect.width : rect.height;
  update(props.unit === "px" ? offset : (offset / size) * 100);
}
function up(event: PointerEvent) {
  const target = event.currentTarget as HTMLElement;
  if (target.hasPointerCapture(event.pointerId))
    target.releasePointerCapture(event.pointerId);
  (event.currentTarget as HTMLElement).classList.remove("dragging");
}
function key(event: KeyboardEvent) {
  const negative = props.axis === "x" ? "ArrowLeft" : "ArrowUp";
  const positive = props.axis === "x" ? "ArrowRight" : "ArrowDown";
  if (![negative, positive, "Home", "End"].includes(event.key)) return;
  event.preventDefault();
  const step = props.unit === "px" ? 8 : 1;
  update(
    event.key === "Home"
      ? props.min
      : event.key === "End"
        ? props.max
        : props.modelValue + (event.key === negative ? -step : step),
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
    :aria-valuemin="min"
    :aria-valuemax="max"
    @pointerdown="down"
    @pointermove="move"
    @pointerup="up"
    @pointercancel="up"
    @lostpointercapture="up"
    @keydown="key"
  ></div>
</template>
