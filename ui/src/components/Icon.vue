<script setup lang="ts">
import { computed } from "vue";
import { icon, svg } from "../icons";
// geometry, when given, is drawn instead of the shared icon of that name.
const props = defineProps<{ name: string; geometry?: string }>();
const markup = computed(() =>
  props.geometry ? svg(props.geometry) : icon(props.name),
);
</script>
<template><span class="icon" v-html="markup"></span></template>
<style scoped>
.icon {
  display: inline-block;
  vertical-align: middle;
  line-height: 0;
  flex: none;
}
.icon :deep(svg) {
  display: block;
}
</style>
