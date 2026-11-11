import { ref } from "vue";
import translations from "./translations.json";
const dictionary: Record<string, string> = translations;
export const locale = ref("zh-Hans");
export function language(tag: string) {
  locale.value = tag.startsWith("en") ? "en" : "zh-Hans";
  document.documentElement.lang = locale.value;
}
export function t(text: string, values: object = {}): string {
  const substitutions = values as Record<string, string | number>;
  return (locale.value === "en" ? (dictionary[text] ?? text) : text).replace(
    /\{(\w+)\}/g,
    (match: string, key: string) => String(substitutions[key] ?? match),
  );
}
