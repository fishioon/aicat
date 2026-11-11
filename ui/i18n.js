const dictionary = await fetch("translations.json").then((r) => r.json());
let english = false;
export function language(tag) {
  english = tag.startsWith("en");
  document.documentElement.lang = english ? "en" : "zh-Hans";
}
export const t = (s, values = {}) =>
  (english ? dictionary[s] || s : s).replace(
    /\{(\w+)\}/g,
    (match, key) => values[key] ?? match,
  );
export function translate(root = document) {
  for (const [selector, attribute, key] of [
    ["[data-i18n-placeholder]", "placeholder", "i18nPlaceholder"],
    ["[data-i18n-label]", "aria-label", "i18nLabel"],
  ])
    for (const e of root.querySelectorAll(selector))
      e.setAttribute(attribute, t(e.dataset[key]));
  for (const e of root.querySelectorAll("[data-i18n]"))
    e.textContent = t(e.dataset.i18n);
}
