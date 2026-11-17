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
const threads = new Map(),
  active = new Map();
let seq = 0,
  mcpID = 100;

function parse(text) {
  return JSON.parse(
    text.split("trigger_messages:\n")[1].split("\nrequesters:\n")[0],
  );
}

function sourceRoom(text) {
  const value = JSON.parse(text.split("\nsource_room:\n")[1]);
  assert(value.id);
  return value.id;
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
  assert.equal(response.status, 200);
  assert(!payload.error && !payload.result?.isError);
  return (
    payload.result?.structuredContent ??
    JSON.parse(payload.result?.content?.[0]?.text ?? "null")
  );
}

function finish(thread) {
  const turn = active.get(thread);
  if (!turn) return;
  emit({
    method: "item/completed",
    params: {
      threadId: thread,
      turnId: turn.id,
      item: {
        type: "agentMessage",
        id: "ordinary",
        phase: "final_answer",
        text: "ordinary output must not be published",
      },
    },
  });
  emit({
    method: "turn/completed",
    params: { threadId: thread, turn: { id: turn.id, status: "completed" } },
  });
  active.delete(thread);
}

async function reply(thread, content) {
  const turn = active.get(thread);
  if (!turn) return;
  const message = turn.messages.at(-1);
  await mcpTool("messages_send", {
    target_room_id: turn.roomID,
    text: content,
    reply_to: message.msgid,
  });
  finish(thread);
}

async function schedule(thread) {
  const turn = active.get(thread);
  const actions = turn.scheduleActions.shift();
  if (!actions) return reply(thread, "schedule complete");
  await mcpTool(actions.name, { ...actions.arguments, room_id: turn.roomID });
  return schedule(thread);
}

async function processTrigger(thread) {
  const turn = active.get(thread);
  const message = turn.messages.at(-1);
  const gates = path.join(root, "codex-gates.json");
  if (
    fs.existsSync(gates) &&
    JSON.parse(fs.readFileSync(gates)).includes(message.msgid)
  ) {
    turn.timer = setTimeout(() => processTrigger(thread), 50);
    return;
  }
  if (message.text.content.startsWith("schedule-actions:")) {
    turn.scheduleActions = JSON.parse(
      message.text.content.slice("schedule-actions:".length),
    );
    return schedule(thread);
  }
  if (message.text.content === "ordinary-only") return finish(thread);
  if (message.text.content.includes("hold-agent")) return;
  if (message.text.content === "forged-target") {
    try {
      await mcpTool("messages_send", {
        target_room_id: "not-a-room",
        text: "forged",
      });
      trace({ method: "forged_target_unexpectedly_accepted" });
    } catch (error) {
      trace({ method: "forged_target_rejected", error: error.message });
    }
    return finish(thread);
  }
  return reply(thread, "MCP reply " + message.msgid);
}

readline.createInterface({ input: process.stdin }).on("line", async (line) => {
  const message = JSON.parse(line),
    params = message.params;
  trace(message);
  if (!message.method) return;
  if (message.method === "initialize") {
    emit({ id: message.id, result: { userAgent: "fixture" } });
    return;
  }
  if (message.method === "initialized") return;
  if (message.method === "model/list") {
    emit({
      id: message.id,
      result: {
        data: params.cursor
          ? [{ model: "room-b", displayName: "Room B", isDefault: false }]
          : [{ model: "room-a", displayName: "Room A", isDefault: true }],
        nextCursor: params.cursor ? null : "page2",
      },
    });
    return;
  }
  if (["thread/start", "thread/resume"].includes(message.method)) {
    assert(!params.dynamicTools);
    if (params.model === "bad-model") {
      emit({ id: message.id, error: { code: -1, message: "MODEL_UNAVAILABLE" } });
      return;
    }
    const id = params.threadId || "thread-" + process.pid + "-" + ++seq;
    threads.set(id, params);
    emit({ id: message.id, result: { thread: { id }, model: params.model || "room-a" } });
    return;
  }
  if (message.method === "thread/inject_items") {
    emit({ id: message.id, result: {} });
    return;
  }
  if (message.method === "turn/start") {
    const messages = parse(params.input[0].text);
    const history = fs
      .readFileSync(path.join(threads.get(params.threadId).cwd, "context/messages.log"), "utf8")
      .trim()
      .split("\n")
      .map(JSON.parse);
    for (const item of messages) {
      assert.equal(
        history.some((row) => row.msgid === item.msgid),
        !item.schedule,
      );
    }
    const id = "turn-" + ++seq;
    active.set(params.threadId, {
      id,
      messages,
      roomID: sourceRoom(params.input[0].text),
      scheduleActions: [],
    });
    emit({ id: message.id, result: { turn: { id } } });
    if (params.model === "rerouted") {
      setTimeout(
        () =>
          emit({
            method: "model/rerouted",
            params: {
              threadId: params.threadId,
              turnId: id,
              fromModel: params.model,
              toModel: "room-a",
              reason: "fixture",
            },
          }),
        30,
      );
      return;
    }
    setTimeout(() => processTrigger(params.threadId), 80);
    return;
  }
  if (message.method === "turn/steer") {
    const turn = active.get(params.threadId);
    assert(turn && turn.id === params.expectedTurnId);
    turn.messages.push(...parse(params.input[0].text));
    emit({ id: message.id, result: {} });
    setTimeout(() => processTrigger(params.threadId), 80);
    return;
  }
  if (message.method === "turn/interrupt") {
    clearTimeout(active.get(params.threadId)?.timer);
    active.delete(params.threadId);
    emit({ id: message.id, result: {} });
    return;
  }
  if (message.method === "thread/unsubscribe") {
    threads.delete(params.threadId);
    emit({ id: message.id, result: { status: "unsubscribed" } });
    return;
  }
  if (message.id) emit({ id: message.id, result: {} });
});
