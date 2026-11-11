<script setup lang="ts">
import { shell, navigate } from "../state/desktop";
import { groups, expanded } from "../state/sidebar";
import { t } from "../i18n";
import TreeRow from "./TreeRow.vue";
function toggle(key: string) {
  if (expanded.has(key)) {
    expanded.delete(key);
    if (
      (key === "apps" && shell.app !== null) ||
      (key === "domains" && shell.host)
    )
      navigate("traffic");
  } else expanded.add(key);
}
</script>
<template>
  <div id="traffic-sidebar">
    <div class="side-search">
      <input
        id="domain-search"
        v-model="shell.sideQuery"
        :aria-label="t('查找应用或域名')"
        :placeholder="t('查找应用或域名…')"
      />
    </div>
    <button
      id="all-domains"
      class="domain-heading active"
      @click="navigate('traffic', { query: shell.query })"
    >
      <span>⌄　{{ t("域名") }}</span
      ><span id="domain-count">{{
        groups.find((group) => group.key === "domains")?.count ?? 0
      }}</span>
    </button>
    <div id="domains">
      <section
        v-for="group in groups"
        :key="group.key"
        :data-group="group.key"
        class="sidebar-group"
      >
        <div class="ac-domain-parent">
          <button class="ac-domain-toggle" @click="toggle(group.key)">
            <svg
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              stroke-width="1.5"
            >
              <path
                :d="expanded.has(group.key) ? 'm6 9 6 6 6-6' : 'm9 6 6 6-6 6'"
              />
            </svg>
          </button>
          <button
            class="ac-pin-group-title"
            @click="
              group.key === 'domains' ? navigate('traffic') : toggle(group.key)
            "
          >
            <span>{{ t(group.title, {}, "traffic-sidebar") }}</span
            ><em class="ac-count">{{ group.count }}</em>
          </button>
        </div>
        <template v-if="expanded.has(group.key)"
          ><TreeRow v-for="node in group.nodes" :key="node.key" :node="node"
        /></template>
      </section>
    </div>
  </div>
</template>
