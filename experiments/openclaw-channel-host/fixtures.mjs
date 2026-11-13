import http from 'node:http';
import {WebSocketServer} from 'ws';
import {eventFrame} from './feishu-frame.mjs';

export async function createPlatform(kind) {
  const requests = [], deliveries = [], inbox = [];
  const sockets = new Set(), clients = new Set();
  let acknowledged = false, revision = 0, holdResponses = false;
  const server = http.createServer(async (req, res) => {
    const chunks = [];
    for await (const chunk of req) chunks.push(chunk);
    const body = JSON.parse(Buffer.concat(chunks).toString() || '{}');
    requests.push({path: req.url, body});
    const reply = data => { if (!res.destroyed) {res.setHeader('content-type', 'application/json'); res.end(JSON.stringify(data));} };
    if (kind === 'feishu') {
      if (req.url === '/callback/ws/endpoint') return reply({code: 0, msg: 'ok', data: {
        URL: `ws://127.0.0.1:${server.address().port}/ws?device_id=synthetic&service_id=1`,
        ClientConfig: {PingInterval: 60, ReconnectCount: 0, ReconnectInterval: 1, ReconnectNonce: 0},
      }});
      if (req.url.includes('/tenant_access_token/')) return reply({code: 0, tenant_access_token: 'synthetic-tenant-token', expire: 7200});
      if (req.url.includes('/openclaw_bot/ping')) return reply({code: 0, data: {pingBotInfo: {botID: 'ou_synthetic_bot', botName: 'Synthetic Bot'}}});
      if (/\/im\/v1\/messages(?:\?|$)/.test(req.url) && req.method === 'POST') {
        deliveries.push(body); if (holdResponses) return;
        return reply({code: 0, data: {message_id: `sent-${deliveries.length}`, chat_id: 'oc_synthetic_dm'}});
      }
      if (req.url.includes('/contact/v3/users/')) return reply({code: 0, data: {user: {name: 'Synthetic Owner', open_id: 'ou_synthetic_owner'}}});
      if (req.url.includes('/im/v1/chats/')) return reply({code: 0, data: {name: 'Synthetic DM', chat_mode: 'group', owner_id: 'ou_synthetic_owner'}});
      return reply({code: 0, data: {items: [], scopes: []}});
    }
    if (req.url.endsWith('/getupdates')) {
      acknowledged = true;
      // Simulate a bounded platform long poll, preserving events after platform cursor.
      const position = Number(body.get_updates_buf || 0);
      setTimeout(() => reply({ret: 0, msgs: inbox.slice(position), get_updates_buf: String(inbox.length), longpolling_timeout_ms: 1000}), 80);
    } else if (req.url.endsWith('/sendmessage')) {
      deliveries.push(body);
      if (holdResponses) return;
      reply({ret: 0, message_id: `sent-${deliveries.length}`});
    } else reply({ret: 0});
  });
  server.on('connection', socket => {sockets.add(socket); socket.on('close', () => sockets.delete(socket));});
  const wss = new WebSocketServer({server});
  wss.on('connection', ws => {
    clients.add(ws); if (kind === 'feishu') acknowledged = true; ws.on('close', () => clients.delete(ws));
    ws.on('message', raw => {
      if (kind === 'feishu') {requests.push({protobufBase64: raw.toString('base64')}); return;}
      const frame = JSON.parse(raw.toString());
      requests.push(frame);
      if (frame.cmd === 'aibot_subscribe') acknowledged = true;
      if (['aibot_send_msg', 'aibot_respond_msg'].includes(frame.cmd)) {
        deliveries.push(frame);
        if (holdResponses) return;
      }
      ws.send(JSON.stringify({headers: frame.headers, errcode: 0, errmsg: 'ok'}));
    });
  });
  await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
  const endpoint = `${kind === 'wecom' ? 'ws' : 'http'}://127.0.0.1:${server.address().port}`;
  return {
    endpoint, requests, deliveries,
    get connected() { return acknowledged && (kind === 'weixin' || clients.size > 0); },
    hold(value) { holdResponses = value; },
    publish(id = 'incoming-1') {
      revision++;
      if (kind === 'weixin') inbox.push({
        message_id: id, from_user_id: 'synthetic-owner', to_user_id: 'synthetic-bot',
        create_time_ms: Date.now(), message_type: 1, message_state: 2,
        context_token: 'synthetic-context-token', item_list: [{type: 1, text_item: {text: 'synthetic hello'}}],
      });
      else if (kind === 'feishu') for (const ws of clients) ws.send(eventFrame(revision, `event-${revision}`, {
        schema: '2.0', header: {event_id: `event-${revision}`, event_type: 'im.message.receive_v1', create_time: String(Date.now()), app_id: 'cli_aaaaaaaaaaaaaaaa'},
        event: {
          sender: {sender_id: {open_id: 'ou_synthetic_owner', user_id: 'synthetic-owner'}, sender_type: 'user', tenant_key: 'synthetic'},
          message: {message_id: id, chat_id: 'oc_synthetic_dm', chat_type: 'p2p', message_type: 'text', create_time: String(Date.now()), content: JSON.stringify({text: 'synthetic hello'}), mentions: []},
        },
      }));
      else for (const ws of clients) ws.send(JSON.stringify({
        cmd: 'aibot_msg_callback', headers: {req_id: `incoming-req-${revision}`},
        body: {msgid: id, chattype: 'single', from: {userid: 'synthetic-owner'}, msgtype: 'text', text: {content: 'synthetic hello'}},
      }));
    },
    async close() {
      for (const ws of clients) ws.terminate();
      for (const socket of sockets) socket.destroy();
      await new Promise(resolve => wss.close(resolve));
      await new Promise(resolve => server.close(resolve));
    },
  };
}
