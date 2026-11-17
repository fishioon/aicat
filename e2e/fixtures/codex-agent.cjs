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
  turns = new Map();
let seq = 0,
  mcpID = 1000;

function parse(text) {
  assert(text.startsWith("trigger_messages:\n"));
  const [messages] = text
    .slice("trigger_messages:\n".length)
    .split("\nrequesters:\n");
  const list = JSON.parse(messages);
  assert(list.length);
  for (const message of list) {
    assert(message.msgid);
    assert(message.roomid);
    assert.equal(message.msgtype, "text");
  }
  return list;
}

function sourceRoom(text) {
  const room = JSON.parse(text.split("\nsource_room:\n")[1]);
  assert(room.id);
  return room.id;
}

function mcpURL() {
  for (let index = 0; index < process.argv.length; index += 1) {
    const value = process.argv[index];
    if (!value.startsWith("mcp_servers.aicat.url=")) continue;
    return JSON.parse(value.slice("mcp_servers.aicat.url=".length));
  }
  throw new Error("missing aicat MCP URL");
}

async function mcpTool(name, args) {
  trace({ method: "mcp_tool_call", name, args });
  const response = await fetch(mcpURL(), {
    method: "POST",
    headers: {
      Authorization: "Bearer " + process.env.AICAT_MCP_TOKEN,
      "Content-Type": "application/json",
      Accept: "application/json, text/event-stream",
    },
    body: JSON.stringify({
      jsonrpc: "2.0",
      id: ++mcpID,
      method: "tools/call",
      params: { name, arguments: args },
    }),
  });
  const payload = await response.json();
  trace({ method: "mcp_tool_result", name, status: response.status, payload });
  const failed = response.status !== 200 || payload.error || payload.result?.isError;
  trace({ method: failed ? "mcp_tool_error" : "mcp_tool_ok", name });
  return (
    payload.result?.structuredContent ??
    JSON.parse(payload.result?.content?.[0]?.text ?? "null")
  );
}

async function finish(thread) {
  const turn = turns.get(thread);
  if (!turn) return;
  const next = turn.next || 0;
  if (next < turn.messages.length) {
    turn.next = next + 1;
    const message = turn.messages[next];
    const text =
      message.msgid === "long-response"
        ? "长🙂\n".repeat(1200)
        : "Codex reply " + message.msgid;
    await mcpTool("messages_send", {
      target_room_id: turn.roomID,
      text,
      reply_to: message.msgid,
    });
    return finish(thread);
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
  turns.delete(thread);
}

function schedule(thread) {
  const turn = turns.get(thread);
  clearTimeout(turn.timer);
  if (turn.messages.some((message) => message.text.content.includes("hold-agent"))) {
    return;
  }
  turn.timer = setTimeout(() => finish(thread), 100);
}

readline.createInterface({ input: process.stdin }).on("line", async (line) => {
  const message = JSON.parse(line),
    params = message.params;
  trace(message);
  if (!message.method) return;
  if (message.method === "initialized") return;
  if (message.method === "initialize") {
    emit({ id: message.id, result: { userAgent: "fixture" } });
    return;
  }
  if (["thread/start", "thread/resume"].includes(message.method)) {
    assert.equal(params.permissions, "aicat-room");
    assert.equal(params.approvalPolicy, "never");
    assert(!params.dynamicTools);
    const id = params.threadId || "thread-" + path.basename(params.cwd);
    workspaces.set(id, params.cwd);
    emit({ id: message.id, result: { thread: { id } } });
    return;
  }
  if (message.method === "turn/interrupt") {
    const turn = turns.get(params.threadId);
    if (turn) clearTimeout(turn.timer);
    turns.delete(params.threadId);
    emit({ id: message.id, result: {} });
    return;
  }
  if (message.method === "turn/start") {
    assert(!turns.has(params.threadId));
    const messages = parse(params.input[0].text);
    const history = fs
      .readFileSync(
        path.join(workspaces.get(params.threadId), "context/messages.log"),
        "utf8",
      )
      .trim()
      .split("\n")
      .map(JSON.parse);
    for (const messageValue of messages) {
      assert(history.some((row) => row.msgid === messageValue.msgid));
    }
    const id = "turn-" + ++seq;
    turns.set(params.threadId, {
      id,
      messages,
      roomID: sourceRoom(params.input[0].text),
    });
    emit({ id: message.id, result: { turn: { id } } });
    if (messages.some((messageValue) => messageValue.msgid === "oversized-agent")) {
      emit({
        method: "fixture/oversized",
        params: { text: "x".repeat(17 * 1024 * 1024) },
      });
      return;
    }
    if (messages.some((messageValue) => messageValue.text.content.includes("tool-check"))) {
      try {
        await mcpTool("messages_send", {
          target_room_id: "unauthorized-room",
          text: "forbidden",
        });
      } catch (error) {
        trace({ method: "forbidden_send_rejected", error: error.message });
      }
      return finish(params.threadId);
    }
    schedule(params.threadId);
    return;
  }
  assert.equal(message.method, "turn/steer");
  const turn = turns.get(params.threadId);
  assert(turn);
  assert.equal(params.expectedTurnId, turn.id);
  turn.messages.push(...parse(params.input[0].text));
  emit({ id: message.id, result: {} });
  schedule(params.threadId);
});
