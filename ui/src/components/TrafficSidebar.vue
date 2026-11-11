<script setup lang="ts">
import { shell, navigate } from "../state/desktop";
import { groups, expanded } from "../state/sidebar";
import { records } from "../state/traffic";
import { t } from "../i18n";
import Icon from "./Icon.vue";
import TreeRow from "./TreeRow.vue";
function toggle(key: string) {
  if (expanded.has(key)) {
    expanded.delete(key);
    if (
      (key === "apps" && shell.app !== null) ||
      (key === "domains" && shell.app === null && shell.host)
    )
      navigate("traffic");
  } else expanded.add(key);
}
</script>
<template>
  <aside id="traffic-sidebar" class="side" :aria-label="t('筛选')">
    <label class="search"
      ><span class="sr">{{ t("查找应用或域名") }}</span
      ><Icon name="search" /><input
        id="domain-search"
        v-model="shell.sideQuery"
        class="field"
        :placeholder="t('查找应用或域名')"
        autocomplete="off"
        spellcheck="false"
    /></label>
    <button
      id="all-domains"
      class="side-item all-requests"
      :aria-current="!shell.host && shell.app === null ? 'true' : undefined"
      @click="navigate('traffic')"
    >
      <span class="label">{{ t("全部请求") }}</span
      ><span class="count num">{{ records.size }}</span>
    </button>
    <div id="domains">
      <section
        v-for="group in groups"
        :key="group.key"
        :data-group="group.key"
        class="sidebar-group"
      >
        <button
          class="side-title group-title"
          :aria-expanded="expanded.has(group.key)"
          @click="$event.detail < 2 && toggle(group.key)"
        >
          <span
            ><Icon name="chev" />{{
              t(group.title, {}, "traffic-sidebar")
            }}</span
          ><span
            class="count num"
            :id="group.key === 'domains' ? 'domain-count' : undefined"
            >{{ group.count }}</span
          >
        </button>
        <ul v-if="expanded.has(group.key)">
          <TreeRow
            v-for="node in group.nodes"
            :key="node.key"
            :node="node"
            :depth="0"
          />
        </ul>
      </section>
    </div>
  </aside>
</template>
