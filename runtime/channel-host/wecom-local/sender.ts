import { fileURLToPath } from "node:url";
import path from "node:path";

export class Sender {
  private closed = false;
  private process?: ReturnType<typeof Bun.spawn>;
  constructor(private script = "") {}
  close() {
    this.closed = true;
    this.process?.kill();
  }
  async send(event: any) {
    if (this.closed) throw Error("LOCAL_SENDER_CLOSED");
    const data = event?.data;
    if (
      event?.type !== "org.aicat.message.send.requested.v1" ||
      typeof data?.roomid !== "string" ||
      typeof data?.search_name !== "string" ||
      !data.search_name.trim() ||
      typeof data?.text?.content !== "string" ||
      !data.text.content.trim() ||
      Buffer.byteLength(data.text.content) > 4000
    )
      throw Error("INVALID_SEND_REQUEST");
    const command = this.script
      ? [this.script]
      : [
          "/usr/bin/osascript",
          fileURLToPath(new URL("./send.applescript", import.meta.url)),
        ];
    if (this.script && !path.isAbsolute(this.script))
      throw Error("INVALID_SEND_SCRIPT");
    const child = Bun.spawn(command, {
      stdin: new Response(
        JSON.stringify({
          searchName: data.search_name,
          text: data.text.content,
        }),
      ),
      stdout: "ignore",
      stderr: "pipe",
    });
    const diagnostics = new Response(child.stderr).text();
    this.process = child;
    let timedOut = false;
    const timeout = setTimeout(() => {
      timedOut = true;
      child.kill();
    }, 12000);
    try {
      if ((await child.exited) !== 0) {
        const detail = (await diagnostics)
          .split(data.text.content)
          .join("<text>")
          .split(data.search_name)
          .join("<target>")
          .trim()
          .slice(0, 1000);
        throw Error(
          timedOut
            ? "LOCAL_SEND_SCRIPT_TIMEOUT"
            : "LOCAL_SEND_SCRIPT_FAILED" + (detail ? ": " + detail : ""),
        );
      }
    } finally {
      clearTimeout(timeout);
      this.process = undefined;
    }
    if (this.closed) throw Error("LOCAL_SENDER_CLOSED");
    return {
      chatId: data.roomid,
      channel: "wecom-local",
      confirmation: "client_script",
    };
  }
}
