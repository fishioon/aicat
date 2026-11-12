// Explicit, deliberately limited OpenClaw compatibility surface for text-only E2E.
import path from 'node:path';
import fs from 'node:fs';
export const DEFAULT_ACCOUNT_ID = 'default';
export const normalizeAccountId = value => String(value).trim().toLowerCase().replace(/[^a-z0-9_-]/g, '-');
export const resolvePreferredOpenClawTmpDir = () => process.env.TMPDIR;
export const buildChannelConfigSchema = schema => ({ schema: schema?.toJSONSchema?.() ?? {} });
export const emptyPluginConfigSchema = () => ({ type: 'object', additionalProperties: false });
export function createPluginRuntimeStore(message) {
  let runtime;
  return { setRuntime(value) { runtime = value; }, getRuntime() { if (!runtime) throw Error(message); return runtime; } };
}
export function createTypingCallbacks({start, stop}) {
  return { onReplyStart: start, onIdle: stop, onCleanup: stop };
}
export async function resolveChannelMessageIngress(params) {
  const allowed = (await params.readStoreAllowFrom()).includes(params.subject.stableId);
  return {senderAccess: {allowed}, commandAccess: {authorized: false}};
}
export const getGlobalHookRunner = () => undefined; // No hooks are installed in this host.
export const buildCanonicalSentMessageHookContext = value => value;
export const toPluginMessageContext = value => value;
export const toPluginMessageSentEvent = value => value;
export const fireAndForgetHook = promise => promise.catch(error => console.error(error));
export const addWildcardAllowFrom = values => [...new Set([...(values ?? []), '*'])];
export const resolveStateDir = () => process.env.OPENCLAW_STATE_DIR;
export const getDefaultMediaLocalRoots = () => [process.env.OPENCLAW_STATE_DIR];
export function mutateConfigFile() { throw Error('UNSUPPORTED: interactive config mutation'); }
export async function loadOutboundMediaFromUrl() { throw Error('UNSUPPORTED: media'); }
export async function detectMime() { throw Error('UNSUPPORTED: media'); }
export function readConfig() { return JSON.parse(fs.readFileSync(path.join(resolveStateDir(), 'openclaw.json'))); }
