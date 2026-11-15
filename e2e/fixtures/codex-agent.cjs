const fs = require("node:fs"),
  path = require("node:path"),
  readline = require("node:readline"),
  assert = require("node:assert/strict");
const root = path.dirname(process.argv[1]);
const trace = (value) =>
  fs.appendFileSync(
    path.join(root, "codex-trace.jsonl"),
    JSON.stringify({ ...value, pid: process.pid }) + "\n",
  );
const emit = (value) => process.stdout.write(JSON.stringify(value) + "\n");
const workspaces = new Map(),
  active = new Map(),
  calls = new Map();
let seq = 0,
  callID = 1000;
trace({ method: "spawn" });
function parse(text) {
  assert(text.startsWith("trigger_messages:\n"));
  const [messages, people] = text
    .slice("trigger_messages:\n".length)
    .split("\nrequesters:\n");
  const list = JSON.parse(messages);
  assert(list.length);
  for (const m of list) {
    assert(m.msgid);
    assert(m.roomid);
    assert.equal(m.msgtype, "text");
    assert.equal(typeof m.text.content, "string");
    assert(!m.event);
    assert(!m.account);
  }
  assert(Array.isArray(JSON.parse(people.split("\nroom_context:\n")[0])));
  return list;
}
function finish(thread) {
  const turn = active.get(thread);
  if (!turn) return;
  const next = turn.next || 0;
  if (next < turn.messages.length) {
    const m = turn.messages[next];
    turn.next = next + 1;
    const id = ++callID;
    calls.set(id, { thread, forbidden: false });
    emit({
      id,
      method: "item/tool/call",
      params: {
        threadId: thread,
        turnId: turn.id,
        tool: "messages_send",
        arguments: {
          event: {
            text:
              m.msgid === "long-response"
                ? "长🙂\n".repeat(1200)
                : "Codex reply " + m.msgid,
          },
        },
      },
    });
    return;
  }
  emit({
    method: "item/completed",
    params: {
      threadId: thread,
      turnId: turn.id,
      item: {
        id: "ordinary",
        type: "agentMessage",
        phase: "final_answer",
        text: "ordinary output must not be forwarded",
      },
    },
  });
  emit({
    method: "turn/completed",
    params: { threadId: thread, turn: { id: turn.id, status: "completed" } },
  });
  active.delete(thread);
}
function schedule(thread) {
  const turn = active.get(thread);
  clearTimeout(turn.timer);
  if (turn.messages.some((m) => m.text.content.includes("hold-agent"))) return;
  turn.timer = setTimeout(() => finish(thread), 700);
}
readline.createInterface({ input: process.stdin }).on("line", (line) => {
  try {
    const message = JSON.parse(line),
      p = message.params;
    trace(message);
    if (!message.method) {
      const call = calls.get(message.id);
      assert(call);
      assert.equal(message.result.success, !call.forbidden);
      calls.delete(message.id);
      schedule(call.thread);
      return;
    }
    if (message.method === "initialized") return;
    if (message.method === "initialize") {
      emit({ id: message.id, result: { userAgent: "fixture" } });
      return;
    }
    if (["thread/start", "thread/resume"].includes(message.method)) {
      assert.equal(p.permissions, "aicat-room");
      assert.equal(p.approvalPolicy, "never");
      assert(p.developerInstructions.includes("trigger_messages"));
      if (message.method === "thread/start")
        assert(p.dynamicTools.some(x=>x.name==="messages_send")&&p.dynamicTools.some(x=>x.name==="rooms_reply"));
      const id = p.threadId || "thread-" + path.basename(p.cwd);
      workspaces.set(id, p.cwd);
      emit({ id: message.id, result: { thread: { id } } });
      return;
    }
    if (message.method === "turn/interrupt") {
      const turn = active.get(p.threadId);
      if (turn) {
        clearTimeout(turn.timer);
        active.delete(p.threadId);
      }
      emit({ id: message.id, result: {} });
      return;
    }
    if (message.method === "turn/start") {
      assert(!active.has(p.threadId));
      const messages = parse(p.input[0].text);
      assert.equal(p.clientUserMessageId, messages[0].msgid);
      const history = fs
        .readFileSync(
          path.join(workspaces.get(p.threadId), "context/messages.log"),
          "utf8",
        )
        .trim()
        .split("\n")
        .map(JSON.parse);
      for (const m of messages)
        assert(history.some((row) => row.msgid === m.msgid));
      const id = "turn-" + ++seq;
      active.set(p.threadId, { id, messages });
      emit({ id: message.id, result: { turn: { id } } });
      if (messages.some((m) => m.msgid === "oversized-agent")) {
        emit({
          method: "fixture/oversized",
          params: { text: "x".repeat(17 * 1024 * 1024) },
        });
        return;
      }
      if (messages.some((m) => m.text.content.includes("tool-check"))) {
        emit({
          id: ++callID,
          method: "item/tool/call",
          params: {
            threadId: p.threadId,
            turnId: id,
            tool: "messages_send",
            arguments: {
              event: {
                specversion: "1.0",
                type: "org.aicat.message.send.requested.v1",
                datacontenttype: "application/json",
                id: "forbidden",
                source: "aicat-agent://codex",
                data: {
                  action: "send",
                  roomid: "unauthorized-room",
                  msgtype: "text",
                  text: { content: "forbidden" },
                },
              },
            },
          },
        });
        calls.set(callID, { thread: p.threadId, forbidden: true });
      } else schedule(p.threadId);
      return;
    }
    assert.equal(message.method, "turn/steer");
    const turn = active.get(p.threadId);
    assert(turn);
    assert.equal(p.expectedTurnId, turn.id);
    turn.messages.push(...parse(p.input[0].text));
    emit({ id: message.id, result: {} });
    schedule(p.threadId);
  } catch (error) {
    trace({ fixtureError: error.stack });
    process.exit(1);
  }
});
