import { fileURLToPath } from "node:url";
import path from "node:path";
import { Source } from "./source";

export class Sender {
  private closed = false;
  private process?: ReturnType<typeof Bun.spawn>;
  constructor(
    private source: Source,
    private script = "",
  ) {}
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
    const after = this.source.prepareSend(data.roomid);
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
      stderr: "ignore",
    });
    this.process = child;
    const timeout = setTimeout(() => child.kill(), 12000);
    try {
      if ((await child.exited) !== 0) throw Error("LOCAL_SEND_SCRIPT_FAILED");
    } finally {
      clearTimeout(timeout);
      this.process = undefined;
    }
    const deadline = Date.now() + 12000;
    while (!this.closed && Date.now() < deadline) {
      const id = this.source.confirmSend(data.roomid, data.text.content, after);
      if (id)
        return {
          messageId: id,
          chatId: data.roomid,
          channel: "wecom-local",
          confirmation: "local_database",
        };
      await Bun.sleep(250);
    }
    throw Error("LOCAL_SEND_UNCONFIRMED");
  }
}
