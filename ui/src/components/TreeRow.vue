<script setup lang="ts">
import { computed } from "vue";
import { shell, navigate, popup, state } from "../state/desktop";
import { pinDomain } from "../state/traffic";
import type { TreeNode } from "../state/sidebar";
import { expanded } from "../state/sidebar";
import { t } from "../i18n";
import Icon from "./Icon.vue";
const props = defineProps<{ node: TreeNode; depth: number }>();
const open = computed(
  () => expanded.has(props.node.key) || !!shell.sideQuery.trim(),
);
const selected = computed(
  () =>
    shell.app === (props.node.app ?? null) &&
    shell.host === (props.node.host ?? "") &&
    shell.path === (props.node.path ?? ""),
);
function choose() {
  navigate("traffic", {
    host: props.node.host ?? "",
    path: props.node.path ?? "",
    app: props.node.app ?? null,
  });
}
function toggle() {
  const node = props.node;
  if (!node.children.length) return;
  if (expanded.has(node.key)) {
    expanded.delete(node.key);
    const path = node.path ?? "";
    if (
      shell.app === (node.app ?? null) &&
      ((node.host === undefined && shell.host) ||
        (shell.host === node.host &&
          shell.path !== path &&
          (!path || shell.path.startsWith(path.replace(/\/$/, "") + "/"))))
    )
      choose();
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
  <li>
    <div
      class="trow tree-row"
      :style="{ paddingLeft: 22 + depth * 14 + 'px' }"
      :data-selected="selected"
      @contextmenu="context"
    >
      <button
        v-if="node.children.length"
        class="twist tree-toggle"
        :aria-label="t(open ? '收起' : '展开') + ' ' + node.label"
        :aria-expanded="open"
        @click="$event.detail < 2 && toggle()"
      >
        <Icon name="chev" />
      </button>
      <span v-else class="twist none"></span>
      <button
        class="side-item tree-item"
        :aria-current="selected ? 'true' : undefined"
        :title="(node.host ?? '') + (node.path ?? '') || node.label"
        :data-host="node.host"
        :data-path="node.host ? (node.path ?? '') : undefined"
        :data-app="node.app"
        @click="$event.detail < 2 && choose()"
        @dblclick="toggle"
      >
        <span class="label" :class="{ path: !!node.path }">{{
          node.label
        }}</span
        ><i
          v-if="node.errors"
          class="sd bad"
          :title="t('{n} 个出错', { n: node.errors })"
        ></i
        ><span class="count num">{{ node.count }}</span>
      </button>
    </div>
    <ul v-if="open && node.children.length">
      <TreeRow
        v-for="child in node.children"
        :key="child.key"
        :node="child"
        :depth="depth + 1"
      />
    </ul>
  </li>
</template>
