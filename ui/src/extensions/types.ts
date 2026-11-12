import type { Component } from "vue";
import type { MenuEntry } from "../state/desktop";

// What an extension adds to its own detail on the extensions page. All of it
// is optional: the page shows an extension from what the backend declares,
// and a folder here, named after the extension's ID, only adds to that.
export interface ExtensionUI {
  // status replaces the line under the switch, with an element ID if the
  // page's checks need one.
  status?(): { text: string; id?: string } | undefined;
  // menu adds entries to the gear menu, before "copy identifier".
  menu?(): MenuEntry[];
  // Body shows the detail and features tabs; it takes a `tab` prop.
  Body?: Component;
}
