import readline from 'node:readline';
import {createRequire} from 'node:module';

const silent = () => {};
const {WSClient} = createRequire(process.argv[2])('@wecom/aibot-node-sdk');
const lines = readline.createInterface({input:process.stdin});
lines.once('line', async line => {
  let client;
  try {
    const {botId,secret,endpoint} = JSON.parse(line);
    const url = new URL(endpoint || 'wss://openws.work.weixin.qq.com');
    if (url.href !== 'wss://openws.work.weixin.qq.com/' && !(url.protocol === 'ws:' && url.hostname === '127.0.0.1')) throw Error();
    client = new WSClient({botId,secret,wsUrl:url.href,maxReconnectAttempts:0,maxAuthFailureAttempts:0,
      logger:{info:silent,debug:silent,warn:silent,error:silent}});
    await new Promise((resolve,reject) => {
      const timer=setTimeout(()=>reject(Error('timeout')),12000);
      const done=fn=>value=>{clearTimeout(timer);fn(value)};
      client.on('authenticated',done(resolve));
      client.on('error',done(reject));
      client.on('disconnected',done(reject));
      client.connect();
    });
    client.disconnect();
    process.stdout.write('{"validated":true}\n');
    process.exit(0);
  } catch {
    client?.disconnect();
    process.stdout.write('{"validated":false}\n');
    process.exit(1);
  }
});
