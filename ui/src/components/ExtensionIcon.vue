<script setup lang="ts">
import { computed } from "vue";
import { uiOf } from "../extensions";
import { theme } from "../state/desktop";
import Icon from "./Icon.vue";

const props = defineProps<{ name: string }>();
const ui = computed(() => uiOf(props.name));
const image = computed(() =>
  theme.value === "dark"
    ? (ui.value.iconImageDark ?? ui.value.iconImage)
    : ui.value.iconImage,
);
</script>
<template>
  <span class="xicon" :class="{ branded: !!image }" aria-hidden="true">
    <img v-if="image" class="extension-icon" :src="image" alt="" />
    <Icon v-else :name="name" :geometry="ui.icon" />
  </span>
</template>
