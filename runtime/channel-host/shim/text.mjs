import fs from 'node:fs';
import path from 'node:path';
export const DEFAULT_GROUP_HISTORY_LIMIT = 50;
export const PAIRING_APPROVED_MESSAGE = 'Pairing approved';
export const SILENT_REPLY_TOKEN = 'NO_REPLY';
export const hasOwnProperty = (value, key) => Object.prototype.hasOwnProperty.call(value, key);
export const normalizeSecretStringValue = value => typeof value === 'string' ? value.trim() : undefined;
export const resolveDefaultAgentId = cfg => cfg.agents?.list?.find(agent => agent.default)?.id ?? 'external-aicat';
export const resolveStorePath = (configured, {agentId} = {}) => configured ?? path.join(process.env.OPENCLAW_STATE_DIR, `sessions-${encodeURIComponent(agentId ?? 'default')}.json`);
export const loadSessionStore = file => fs.existsSync(file) ? JSON.parse(fs.readFileSync(file, 'utf8')) : {};
export const resolveSessionStoreEntry = ({store, sessionKey}) => ({existing: store[sessionKey], key: sessionKey});
export const createReplyPrefixContext = ({cfg}) => ({responsePrefix: cfg.messages?.responsePrefix, responsePrefixContextProvider: () => ({})});
export const jsonResult = value => ({content: [{type: 'text', text: JSON.stringify(value)}], details: value});
export const formatDocsLink = route => `https://docs.openclaw.ai${route}`;
export function isNormalizedSenderAllowed({senderId, allowFrom}) {
  return (allowFrom ?? []).some(id => String(id) === '*' || String(id).trim().toLowerCase() === String(senderId).trim().toLowerCase());
}
export async function resolveSenderCommandAuthorization(params) {
  const store = await params.readAllowFromStore?.() ?? [];
  const allowed = [...(params.isGroup ? params.configuredGroupAllowFrom ?? [] : params.configuredAllowFrom ?? []), ...store];
  return {senderAllowedForCommands: isNormalizedSenderAllowed({senderId: params.senderId, allowFrom: allowed}), commandAuthorized: false};
}
export function readStringParam(params, key, options = {}) {
  const value = params[key];
  if (typeof value === 'string' && value.trim()) return value;
  if (options.required) throw Error(`MISSING_STRING: ${key}`);
  return undefined;
}
export function clearHistoryEntriesIfEnabled({historyMap, historyKey, limit}) {if (limit > 0) historyMap?.delete(historyKey);}
export function recordPendingHistoryEntryIfEnabled({historyMap, historyKey, entry, limit}) {
  if (!historyMap || !historyKey || !limit) return;
  historyMap.set(historyKey, [...(historyMap.get(historyKey) ?? []), entry].slice(-limit));
}
export function buildPendingHistoryContextFromMap({historyMap, historyKey, currentMessage, formatEntry}) {
  const history = (historyMap?.get(historyKey) ?? []).map(formatEntry ?? (entry => entry.body ?? '')).join('\n');
  return [history, currentMessage].filter(Boolean).join('\n');
}
export function extractToolSend() {throw Error('UNSUPPORTED: channel action tools');}
export function dispatchPluginInteractiveHandler() {throw Error('UNSUPPORTED: interactive handlers');}
export function resolveThreadSessionKeys() {throw Error('UNSUPPORTED: group threads');}
export function logTypingFailure(error) {console.error(error);}
