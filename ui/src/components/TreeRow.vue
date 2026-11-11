<script setup lang="ts">
import { computed } from "vue";
import { shell, navigate, popup, state } from "../state/desktop";
import { pinDomain } from "../state/traffic";
import type { TreeNode } from "../state/sidebar";
import { expanded } from "../state/sidebar";
import { t } from "../i18n";
const props = defineProps<{ node: TreeNode }>();
const open = computed(() => expanded.has(props.node.key));
const selected = computed(() =>
  props.node.app !== undefined
    ? shell.app === props.node.app
    : shell.app === null &&
      shell.host === props.node.host &&
      shell.path === (props.node.path ?? ""),
);
function choose() {
  navigate("traffic", {
    host: props.node.host ?? "",
    path: props.node.path ?? "",
    app: props.node.app ?? null,
    query: props.node.app === undefined ? "" : shell.query,
  });
}
function toggle() {
  const node = props.node;
  if (!node.children.length) return;
  if (open.value) {
    expanded.delete(node.key);
    if (
      shell.host === node.host &&
      shell.path &&
      shell.path !== (node.path ?? "") &&
      shell.path.startsWith(node.path ?? "/")
    )
      navigate("traffic", { host: node.host, path: node.path ?? "" });
  } else expanded.add(node.key);
}
function context(event: MouseEvent) {
  if (!props.node.host) return;
  event.preventDefault();
  const domain = props.node.host;
  popup({ x: event.clientX, y: event.clientY }, [
    {
      label: state.value?.pinnedDomains.includes(domain) ? "取消固定" : "固定",
      run: () => pinDomain(domain),
    },
  ]);
}
</script>
<template>
  <div class="ac-tree-row" :data-selected="selected" @contextmenu="context">
    <button
      :class="node.children.length ? 'ac-tree-toggle' : 'ac-tree-spacer'"
      :aria-label="t('展开或收起') + ' ' + node.label"
      :aria-expanded="open"
      @click="toggle"
    >
      <svg
        v-if="node.children.length"
        viewBox="0 0 24 24"
        fill="none"
        stroke="currentColor"
        stroke-width="1.5"
      >
        <path :d="open ? 'm6 9 6 6 6-6' : 'm9 6 6 6-6 6'" />
      </svg>
    </button>
    <button
      class="ac-side-item"
      :title="(node.host ?? '') + (node.path ?? '') || node.label"
      :data-host="node.host"
      :data-path="node.host ? (node.path ?? '') : undefined"
      :data-app="node.app"
      @click="choose"
    >
      <span>{{ node.label }}</span
      ><em class="ac-count">{{ node.count }}</em>
    </button>
  </div>
  <div v-if="open" class="ac-tree-children">
    <TreeRow v-for="child in node.children" :key="child.key" :node="child" />
  </div>
</template>
