import { Channel } from "mygo-runtime";
import { Desktop, events, type TrafficBatch } from "./generated/mygo";

// This adapter and the generated client are the only MyGo dependencies.
export const host = {
  ...Desktop,
  onStateChanged: events.desktopChanged.on,
  async watch(onBatch: (batch: TrafficBatch) => void, signal: AbortSignal) {
    const channel = new Channel<TrafficBatch>(onBatch);
    const stop = () => channel.close();
    signal.addEventListener("abort", stop, { once: true });
    if (signal.aborted) stop();
    try {
      await Desktop.watchTraffic(channel);
    } finally {
      signal.removeEventListener("abort", stop);
      channel.close();
    }
  },
};

export async function initializeHost() {
  // Vite eliminates this branch and its module in production bundles.
  if (import.meta.env.MODE === "e2e" && !window.mygo) {
    const { installPreview } = await import("./testing/preview");
    installPreview();
  }
}
