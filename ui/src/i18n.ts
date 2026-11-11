import { ref } from "vue";
import translations from "./translations.json";
const dictionary: Record<string, string> = translations;
export const locale = ref("zh-Hans");
export function language(tag: string) {
  locale.value = tag.startsWith("en") ? "en" : "zh-Hans";
  document.documentElement.lang = locale.value;
}
export function t(text: string, values: object = {}, context?: string): string {
  const substitutions = values as Record<string, string | number>;
  const translated = context ? dictionary[`${context}.${text}`] : undefined;
  return (
    locale.value === "en" ? (translated ?? dictionary[text] ?? text) : text
  ).replace(/\{(\w+)\}/g, (match: string, key: string) =>
    String(substitutions[key] ?? match),
  );
}
