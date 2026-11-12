// 16px outline geometry from docs/design/aicat-redesign.html.
const paths: Record<string, string> = {
  copy: '<rect x="5.5" y="5.5" width="8" height="8" rx="1.5"/><path d="M10.5 5.5v-2a1 1 0 0 0-1-1h-6a1 1 0 0 0-1 1v6a1 1 0 0 0 1 1h2"/>',
  search: '<circle cx="7" cy="7" r="4.5"/><path d="m10.5 10.5 3 3"/>',
  chev: '<path d="m6 3.5 4.5 4.5L6 12.5"/>',
  back: '<path d="M10 3.5 5.5 8l4.5 4.5"/>',
  sun: '<circle cx="8" cy="8" r="3"/><path d="M8 1.5V3M8 13v1.5M1.5 8H3M13 8h1.5M3.4 3.4l1 1M11.6 11.6l1 1M3.4 12.6l1-1M11.6 4.4l1-1"/>',
  moon: '<path d="M13.5 9.5a6 6 0 0 1-7-7 6 6 0 1 0 7 7Z"/>',
  pause: '<path d="M5.5 3.5v9M10.5 3.5v9"/>',
  play: '<path d="M5 3.5v9l7.5-4.5Z"/>',
  down: '<path d="M8 2.5v8M4.5 7.5 8 11l3.5-3.5M3 13.5h10"/>',
  plus: '<path d="M8 3v10M3 8h10"/>',
  x: '<path d="m4 4 8 8M12 4l-8 8"/>',
  home: '<path d="M2.5 7.5 8 3l5.5 4.5v5a1 1 0 0 1-1 1h-9a1 1 0 0 1-1-1Z"/><path d="M6.5 13.5v-4h3v4"/>',
  traffic:
    '<path d="M2.5 5.5h10M10 3l2.5 2.5L10 8M13.5 10.5h-10M6 8l-2.5 2.5L6 13"/>',
  extensions:
    '<rect x="2.5" y="2.5" width="4.5" height="4.5" rx="1"/><rect x="2.5" y="9" width="4.5" height="4.5" rx="1"/><rect x="9" y="9" width="4.5" height="4.5" rx="1"/><path d="M11.25 2.5V7M9 4.75h4.5"/>',
  more: '<circle cx="3.5" cy="8" r=".6"/><circle cx="8" cy="8" r=".6"/><circle cx="12.5" cy="8" r=".6"/>',
  settings:
    '<path d="M2.5 4.5h6M12.5 4.5h1M2.5 11.5h1M7.5 11.5h6"/><circle cx="10.5" cy="4.5" r="1.75"/><circle cx="5.5" cy="11.5" r="1.75"/>',
  pin: '<path d="M6 2.5h4l-.5 4 2 2.5h-7l2-2.5Z"/><path d="M8 9v4.5"/>',
  gear: '<g transform="scale(.6667)" stroke-width="2.2"><path d="M9.671 4.136a2.34 2.34 0 0 1 4.659 0 2.34 2.34 0 0 0 3.319 1.915 2.34 2.34 0 0 1 2.33 4.033 2.34 2.34 0 0 0 0 3.831 2.34 2.34 0 0 1-2.33 4.033 2.34 2.34 0 0 0-3.319 1.915 2.34 2.34 0 0 1-4.659 0 2.34 2.34 0 0 0-3.32-1.915 2.34 2.34 0 0 1-2.33-4.033 2.34 2.34 0 0 0 0-3.831A2.34 2.34 0 0 1 6.35 6.051a2.34 2.34 0 0 0 3.319-1.915"/><circle cx="12" cy="12" r="3"/></g>',
  info: '<circle cx="8" cy="8" r="5.5"/><path d="M8 7.5v3.5M8 5.2v.1"/>',
};
// svg draws 16px outline geometry, such as an extension's own icon.
export const svg = (geometry: string) =>
  `<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">${geometry}</svg>`;
export const icon = (name: string) => svg(paths[name] ?? paths.extensions);
