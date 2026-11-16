<script setup lang="ts">
import { computed } from "vue";
import { navIcon, type NavIconName } from "../nav-icons";

const props = defineProps<{ name: NavIconName }>();
const markup = computed(() => navIcon(props.name));
</script>
<template><span class="nav-icon" v-html="markup"></span></template>
<style scoped>
.nav-icon {
  display: inline-block;
  width: 20px;
  height: 20px;
  line-height: 0;
  flex: none;
}
.nav-icon :deep(svg) {
  display: block;
  width: 20px;
  height: 20px;
}
</style>
