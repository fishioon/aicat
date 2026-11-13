import { shallowRef } from "vue";
import type { Group, Report } from "../generated/mygo";
import { host } from "../host";

export type Period = "today" | "7d" | "30d";
export type UsageGroup = Group;
export const period = shallowRef<Period>("today");
export const usage = shallowRef<Report | null>(null);

export const usageLoading = shallowRef(false);
export const usageError = shallowRef("");

let latest = 0;
let pendingPeriod: Period | undefined;
export async function loadUsage() {
  if (usageLoading.value && pendingPeriod === period.value) return;
  pendingPeriod = period.value;
  const id = ++latest;
  usageLoading.value = true;
  usageError.value = "";
  try {
    const report = await host.usage(period.value);
    if (id === latest) usage.value = report;
  } catch (error) {
    if (id === latest)
      usageError.value = error instanceof Error ? error.message : String(error);
  } finally {
    if (id === latest) usageLoading.value = false;
  }
}

export const tokens = (g: UsageGroup) =>
  g.input + g.output + g.cacheRead + g.cacheWrite;
export function hitRate(g: UsageGroup) {
  const all = g.input + g.cacheRead + g.cacheWrite;
  return all ? g.cacheRead / all : 0;
}
export function count(n: number): string {
  const units: [number, string][] = [
    [1e9, "B"],
    [1e6, "M"],
    [1e3, "K"],
  ];
  for (const [size, unit] of units)
    if (n >= size) {
      const v = n / size;
      return (
        (v >= 100 ? v.toFixed(0) : v.toFixed(1).replace(/\.0$/, "")) + unit
      );
    }
  return String(Math.round(n));
}
export function seconds(ms: number): string {
  return ms < 1000 ? Math.round(ms) + " ms" : (ms / 1000).toFixed(1) + " s";
}
