<script setup lang="ts">
import { computed } from "vue";
import { icon } from "../icons";
const props = defineProps<{ name: string }>();
const markup = computed(() => icon(props.name));
</script>
<template><span class="icon" v-html="markup"></span></template>
<style scoped>
.icon {
  display: contents;
}
</style>
