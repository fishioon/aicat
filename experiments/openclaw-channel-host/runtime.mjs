import path from 'node:path';

// Both plugins use this same runtime. No platform-specific dispatch or send implementation.
export function createRuntime(store, config, stateDir, status) {
  const calls = {};
  const track = (name, fn) => (...args) => { calls[name] = (calls[name] ?? 0) + 1; return fn(...args); };
  const ingest = track('inbound.dispatch', async ({ctx}) => {
    store.ingest(ctx);
    return {queuedFinal: false, counts: {tool: 0, block: 0, final: 0}};
  });
  const reply = {
    finalizeInboundContext: track('reply.finalizeInboundContext', value => value),
    resolveHumanDelayConfig: () => ({mode: 'off'}),
    createReplyDispatcherWithTyping: ({deliver}) => ({
      dispatcher: {sendFinalReply: deliver, sendBlockReply: deliver, sendToolResult: deliver,
        markComplete() {}, waitForIdle: async () => {}, getQueuedCounts: () => ({tool: 0, block: 0, final: 0})},
      replyOptions: {}, markDispatchIdle() {},
    }),
    withReplyDispatcher: async ({run}) => run(),
    dispatchReplyFromConfig: ingest,
    dispatchReplyWithBufferedBlockDispatcher: ingest,
  };
  const runtime = {
    version: '2026.5.12', // Advertised compatibility baseline, not a full OpenClaw implementation.
    config: {loadConfig: () => config, readConfigFileSnapshot: async () => ({config})},
    logging: {getChildLogger: () => console},
    channel: {
      reply,
      routing: {resolveAgentRoute: track('routing.resolveAgentRoute', ({accountId, peer}) => ({
        agentId: 'external-aicat', accountId, sessionKey: JSON.stringify([accountId, peer.id]), mainSessionKey: JSON.stringify([accountId, peer.id]),
      }))},
      session: {
        resolveStorePath: () => path.join(stateDir, 'sessions'),
        recordInboundSession: track('session.recordInboundSession', async ({sessionKey, ctx}) => {
          store.data.sessions[sessionKey] = {messageId: ctx.MessageSid}; store.save();
        }),
      },
      commands: {
        shouldComputeCommandAuthorized: () => false,
        isControlCommandMessage: () => false,
        shouldHandleTextCommands: () => false,
        resolveCommandAuthorizedFromAuthorizers: () => false,
      },
      text: {chunkMarkdownText: (text, limit) => Array.from({length: Math.ceil(text.length / limit)}, (_, i) => text.slice(i * limit, (i + 1) * limit))},
      pairing: {readAllowFromStore: async () => []},
    },
  };
  return {runtime, calls};
}
