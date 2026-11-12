<script setup lang="ts">
import { computed, shallowRef } from "vue";
import type { Bucket } from "../generated/mygo";
import { count } from "../state/usage";
import { t } from "../i18n";

// Stacked token bars per bucket, one segment per series. series is in slot
// order; keys outside it are summed into "other".
const props = defineProps<{
  buckets: Bucket[];
  series: { key: string; name: string; slot: number }[];
  hourly: boolean;
}>();

const known = computed(() => new Set(props.series.map((s) => s.key)));
const columns = computed(() =>
  props.buckets.map((b) => {
    const parts = props.series.map((s) => ({
      ...s,
      value: b.tokens[s.key] ?? 0,
    }));
    const other = Object.entries(b.tokens)
      .filter(([key]) => !known.value.has(key))
      .reduce((sum, [, v]) => sum + v, 0);
    if (other) parts.push({ key: "", name: t("其他"), slot: 0, value: other });
    const date = new Date(b.start);
    return {
      start: b.start,
      label: props.hourly
        ? String(date.getHours()).padStart(2, "0") + ":00"
        : date.getMonth() + 1 + "/" + date.getDate(),
      requests: b.requests,
      total: parts.reduce((sum, p) => sum + p.value, 0),
      parts: parts.filter((p) => p.value),
    };
  }),
);
// A round top for the axis: 1, 2 or 5 times a power of ten.
const top = computed(() => {
  const max = Math.max(0, ...columns.value.map((c) => c.total));
  if (!max) return 1;
  const step = 10 ** Math.floor(Math.log10(max));
  return [1, 2, 5, 10].map((m) => m * step).find((v) => v >= max)!;
});
const ticks = computed(() => [top.value, top.value / 2, 0]);
const every = computed(() =>
  props.hourly ? 3 : columns.value.length > 10 ? 5 : 1,
);
const hover = shallowRef(-1);
</script>
<template>
  <div class="uc" @mouseleave="hover = -1">
    <div class="uc-axis" aria-hidden="true">
      <span v-for="tick in ticks" :key="tick" class="num">{{
        count(tick)
      }}</span>
    </div>
    <div class="uc-plot">
      <div class="uc-grid" aria-hidden="true">
        <i v-for="tick in ticks" :key="tick"></i>
      </div>
      <div class="uc-cols">
        <div
          v-for="(col, index) in columns"
          :key="col.start"
          class="uc-col"
          :class="{ on: hover === index }"
          @mouseenter="hover = index"
        >
          <div
            class="uc-bar"
            :style="{ height: (col.total / top) * 100 + '%' }"
          >
            <i
              v-for="part in col.parts"
              :key="part.key"
              :class="'s' + part.slot"
              :style="{ flexGrow: part.value }"
            ></i>
          </div>
          <span class="uc-x num" :class="{ hide: index % every }">{{
            col.label
          }}</span>
          <div v-if="hover === index" class="uc-tip" role="tooltip">
            <strong>{{ col.label }}</strong>
            <span class="num"
              >{{ count(col.total) }} Token · {{ col.requests }}
              {{ t("次请求") }}</span
            >
            <ul>
              <li v-for="part in col.parts" :key="part.key">
                <i :class="'s' + part.slot"></i>{{ part.name
                }}<b class="num">{{ count(part.value) }}</b>
              </li>
            </ul>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>
