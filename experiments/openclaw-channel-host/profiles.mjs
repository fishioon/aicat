import fs from 'node:fs';
import path from 'node:path';
import {pathToFileURL} from 'node:url';

export const profiles = {
  weixin: {package: '@tencent-weixin/openclaw-weixin', version: '2.4.10', channel: 'openclaw-weixin'},
  feishu: {package: '@larksuite/openclaw-lark', version: '2026.7.16', channel: 'feishu'},
  wecom: {package: '@wecom/wecom-openclaw-plugin', version: '2026.10.30', channel: 'wecom'},
};
// Only fixture configuration/credential layout differs. Platform protocol handling stays upstream.
export function configure(kind, stateDir, endpoint) {
  const accountId = 'synthetic-bot';
  if (kind === 'weixin') {
    const dir = path.join(stateDir, 'openclaw-weixin');
    fs.mkdirSync(path.join(dir, 'accounts'), {recursive: true});
    fs.writeFileSync(path.join(dir, 'accounts.json'), JSON.stringify([accountId]), {mode: 0o600});
    fs.writeFileSync(path.join(dir, 'accounts', `${accountId}.json`), JSON.stringify({token: 'synthetic-token', baseUrl: endpoint, userId: 'synthetic-owner'}), {mode: 0o600});
    return {accountId, config: {channels: {'openclaw-weixin': {enabled: true, replyProgressMessages: false, quoteCache: {enabled: false}, cdnBaseUrl: endpoint}}}};
  }
  if (kind === 'feishu') return {accountId, config: {tools: {}, channels: {feishu: {
    enabled: true, defaultAccount: accountId,
    accounts: {[accountId]: {appId: 'cli_aaaaaaaaaaaaaaaa', appSecret: 'synthetic-secret', domain: endpoint,
      connectionMode: 'websocket', dmPolicy: 'open', allowFrom: ['*'], groupPolicy: 'disabled',
      replyMode: 'static', streaming: false, typingIndicator: false}},
  }}}};
  return {accountId, config: {tools: {}, channels: {wecom: {
    enabled: true, botId: accountId, secret: 'synthetic-secret', websocketUrl: endpoint,
    dmPolicy: 'open', groupPolicy: 'disabled', sendThinkingMessage: false,
    dynamicAgents: {enabled: false},
  }}}};
}
export function packageEntry(profile) {
  const directory = path.join(import.meta.dirname, 'node_modules', profile.package);
  const metadata = JSON.parse(fs.readFileSync(path.join(directory, 'package.json')));
  if (metadata.version !== profile.version) throw Error('PLUGIN_VERSION_MISMATCH');
  const entry = metadata.openclaw?.runtimeExtensions?.[0] ?? metadata.openclaw?.extensions?.[0] ?? metadata.main;
  if (!entry || entry.endsWith('.ts')) throw Error('COMPILED_PLUGIN_ENTRY_REQUIRED');
  return pathToFileURL(path.join(directory, entry)).href;
}
