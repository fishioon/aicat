// E2E only. Real WKWebView always uses MyGo's injected runtime and Channels.
import type { Channel, Runtime } from "mygo-runtime";

class PreviewChannel<T> implements Channel<T> {
  onmessage: ((value: T) => void) | null;
  onclose: (() => void) | null = null;
  closed = false;
  controller = new AbortController();
  constructor(onmessage?: (value: T) => void) {
    this.onmessage = onmessage ?? null;
  }
  close() {
    if (!this.closed) {
      this.closed = true;
      this.controller.abort();
      this.onclose?.();
    }
  }
  async *[Symbol.asyncIterator](): AsyncIterator<T> {
    throw new Error("PreviewChannel requires an onmessage handler");
  }
}

export function installPreview() {
  const token = new URL(location.href).searchParams.get("token");
  if (!token) throw new Error("E2E preview token missing");
  const headers = {
    "X-Aicat-Preview": token,
    "Content-Type": "application/json",
  };
  async function lines(response: Response, receive: (data: unknown) => void) {
    if (!response.ok) throw new Error(await response.text());
    const reader = response
      .body!.pipeThrough(new TextDecoderStream())
      .getReader();
    let buffer = "";
    try {
      while (true) {
        const { done, value } = await reader.read();
        if (done) break;
        buffer += value;
        let index: number;
        while ((index = buffer.indexOf("\n")) >= 0) {
          receive(JSON.parse(buffer.slice(0, index)));
          buffer = buffer.slice(index + 1);
        }
      }
    } finally {
      await reader.cancel().catch(() => {});
    }
  }
  const listeners = new Map<string, Set<(value: unknown) => void>>();
  let eventController: AbortController | undefined;
  const bridge: Pick<Runtime, "call" | "channel" | "on" | "once"> = {
    async call<T>(method: string, ...args: unknown[]): Promise<T> {
      const channel = args.find((arg) => arg instanceof PreviewChannel);
      const response = await fetch("/rpc", {
        method: "POST",
        headers,
        body: JSON.stringify({
          method,
          args: args.filter((arg) => arg !== channel),
        }),
        signal: channel?.controller.signal,
      });
      if (channel) {
        try {
          await lines(response, (data) => channel.onmessage?.(data));
        } finally {
          channel.close();
        }
        return undefined as T;
      }
      const value = await response.json();
      if (!response.ok) throw new Error(value.error ?? "preview RPC failed");
      return value as T;
    },
    channel<T>(onmessage?: (value: T) => void): Channel<T> {
      return new PreviewChannel(onmessage);
    },
    on<T>(name: string, listener: (payload: T) => void) {
      const receive = (value: unknown) => listener(value as T);
      if (!listeners.has(name)) listeners.set(name, new Set());
      listeners.get(name)!.add(receive);
      if (!eventController) {
        eventController = new AbortController();
        const signal = eventController.signal;
        void (async () => {
          while (!signal.aborted) {
            try {
              await lines(
                await fetch("/events", { headers, signal }),
                (value) => {
                  for (const callback of listeners.get("desktop:changed") ?? [])
                    callback(value);
                },
              );
            } catch {
              if (signal.aborted) return;
            }
            await new Promise((resolve) => setTimeout(resolve, 300));
          }
        })();
      }
      return () => {
        listeners.get(name)?.delete(receive);
        if (![...listeners.values()].some((set) => set.size)) {
          eventController?.abort();
          eventController = undefined;
        }
      };
    },
    once<T>(name: string, listener: (payload: T) => void) {
      const off = bridge.on<T>(name, (value) => {
        off();
        listener(value);
      });
      return off;
    },
  };
  Object.defineProperty(window, "mygo", { value: bridge });
  Object.defineProperty(window, "__aicatPreview", { value: true });
}
