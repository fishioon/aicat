<script setup lang="ts">
import { computed, ref } from "vue";
import Icon from "./Icon.vue";
const props = defineProps<{
  avatar?: string;
  members?: string[];
  group?: boolean;
}>();
const failed = ref(new Set<string>());
const images = computed(() => {
  const candidates = props.avatar
    ? [props.avatar]
    : props.group
      ? (props.members ?? [])
      : [];
  return [...new Set(candidates)]
    .filter((url) => url && !failed.value.has(url))
    .slice(0, 4);
});
function unavailable(url: string) {
  failed.value = new Set([...failed.value, url]);
}
</script>
<template>
  <span
    class="room-avatar"
    :class="{ 'room-avatar-group': images.length > 1 }"
    aria-hidden="true"
  >
    <img
      v-for="url in images"
      :key="url"
      :src="url"
      alt=""
      referrerpolicy="no-referrer"
      @error="unavailable(url)"
    />
    <Icon v-if="!images.length" :name="group ? 'people' : 'person'" />
  </span>
</template>
