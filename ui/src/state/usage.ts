import { shallowRef } from "vue";
import type { Group, Report } from "../generated/mygo";
import { host } from "../host";

// Usage of the model calls aicat forwarded to providers, summed over a
// period by the usage service.
export type Period = "today" | "7d" | "30d";
export type UsageGroup = Group;
export const period = shallowRef<Period>("today");
export const usage = shallowRef<Report | null>(null);

let latest = 0;
// loadUsage reads the selected period; only the latest read is kept, so a
// slow reply for an earlier period never replaces a newer one.
export async function loadUsage() {
  const id = ++latest;
  const report = await host.usage(period.value);
  if (id === latest) usage.value = report;
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
