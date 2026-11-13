<script setup lang="ts">
import { ref, watch } from "vue";
import Icon from "./Icon.vue";

const props = defineProps<{ image?: string }>();
const failed = ref(false);
watch(
  () => props.image,
  () => (failed.value = false),
);
</script>
<template>
  <span class="provider-icon" aria-hidden="true">
    <img
      v-if="image?.startsWith('data:image/') && !failed"
      :src="image"
      alt=""
      @error="failed = true"
    />
    <Icon v-else name="provider" />
  </span>
</template>
