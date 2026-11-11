// Lucide-compatible 24px outline geometry, matching the HTML reference.
const paths: Record<string, string> = {
  pin: '<path d="m16 3 5 5-4 1-4 4-1 5-3-3-5 6 6-5-3-3 5-1 4-4z"/>',
  "pin-off":
    '<path d="m3 3 18 18M16 3l5 5-4 1-2 2M13 13l-1 5-3-3-5 6M7 9l-1 3 3 1"/>',
  traffic: '<path d="M22 12h-4l-3 9-6-18-3 9H2"/>',
  extensions:
    '<rect x="3" y="3" width="7" height="7" rx="1"/><rect x="14" y="3" width="7" height="7" rx="1"/><rect x="3" y="14" width="7" height="7" rx="1"/><rect x="14" y="14" width="7" height="7" rx="1"/>',
  more: '<circle cx="4" cy="12" r="1"/><circle cx="12" cy="12" r="1"/><circle cx="20" cy="12" r="1"/>',
  back: '<path d="m12 19-7-7 7-7M5 12h14"/>',
  forward: '<path d="m12 5 7 7-7 7M5 12h14"/>',
  sidebar:
    '<rect x="3" y="3" width="18" height="18" rx="2"/><path d="M9 3v18"/>',
  export:
    '<path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4M7 10l5 5 5-5M12 15V3"/>',
  sun: '<circle cx="12" cy="12" r="4"/><path d="M12 2v2M12 20v2M2 12h2M20 12h2M4.93 4.93l1.42 1.42M17.65 17.65l1.42 1.42M4.93 19.07l1.42-1.42M17.65 6.35l1.42-1.42"/>',
  copy: '<rect x="9" y="9" width="13" height="13" rx="2"/><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"/>',
  search: '<circle cx="11" cy="11" r="8"/><path d="m21 21-4.35-4.35"/>',
  trash: '<path d="M3 6h18M19 6l-1 14H6L5 6M9 6V3h6v3M10 10v7M14 10v7"/>',
  settings:
    '<path d="M4 21v-7M4 10V3M12 21v-9M12 8V3M20 21v-5M20 12V3M1 14h6M9 8h6M17 16h6"/>',
};
export const icon = (name: string) =>
  `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">${paths[name] || paths.extensions}</svg>`;
