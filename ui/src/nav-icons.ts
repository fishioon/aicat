export type NavIconName =
  "home" | "messages" | "traffic" | "extensions" | "settings";

const paths: Record<NavIconName, string> = {
  home: '<path d="M3 8.2 8.6 3.4a2.2 2.2 0 0 1 2.8 0L17 8.2v6.2a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V8.2Z"/><path d="M8 16.4v-3.7a1 1 0 0 1 1-1h2a1 1 0 0 1 1 1v3.7"/>',
  messages:
    '<path d="M4.2 3.4h11.6a1.8 1.8 0 0 1 1.8 1.8v7.2a1.8 1.8 0 0 1-1.8 1.8H9.8l-3.9 2.5c-.6.38-1.4-.05-1.4-.77v-1.73H4.2a1.8 1.8 0 0 1-1.8-1.8V5.2a1.8 1.8 0 0 1 1.8-1.8Z"/>',
  traffic:
    '<circle cx="10" cy="10" r="7"/><path d="M5.8 10h1.6l1.1-2.6 2.7 5.2 1.1-2.6h1.9"/>',
  extensions:
    '<rect x="3.5" y="3.5" width="5.5" height="5.5" rx="1.5"/><rect x="11" y="3.5" width="5.5" height="5.5" rx="1.5"/><rect x="3.5" y="11" width="5.5" height="5.5" rx="1.5"/><rect x="11" y="11" width="5.5" height="5.5" rx="1.5"/>',
  settings:
    '<path d="M3.2 6.5h8.1M14.7 6.5h2.1M3.2 13.5h2.1M8.7 13.5h8.1"/><circle cx="12.5" cy="6.5" r="2.1"/><circle cx="6.5" cy="13.5" r="2.1"/>',
};

export const navIcon = (name: NavIconName) =>
  `<svg viewBox="0 0 20 20" fill="none" stroke="currentColor" stroke-width="1" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">${paths[name]}</svg>`;
