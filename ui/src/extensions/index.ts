import { state } from "../state/desktop";
import type { ExtensionState } from "../generated/mygo";
import type { ExtensionUI } from "./types";

// Each folder here named after an extension's ID adds to its detail; none
// is registered by hand.
const modules = import.meta.glob<{ default: ExtensionUI }>("./*/index.ts", {
  eager: true,
});
const uis: Record<string, ExtensionUI> = Object.fromEntries(
  Object.entries(modules).map(([path, m]) => [path.split("/")[1], m.default]),
);

export const uiOf = (id: string): ExtensionUI => uis[id] ?? {};

// The extensions the backend registered, in its order.
export const extensionList = (): ExtensionState[] =>
  state.value?.extensions ?? [];

export const extensionOf = (id: string): ExtensionState | undefined =>
  extensionList().find((x) => x.id === id);
