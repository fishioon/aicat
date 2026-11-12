import fs from 'node:fs';
import path from 'node:path';

export class Store {
  constructor(directory) {
    this.file = path.join(directory, 'host.json');
    this.data = fs.existsSync(this.file) ? JSON.parse(fs.readFileSync(this.file, 'utf8')) : {events: [], receipts: {}, sessions: {}};
    for (const receipt of Object.values(this.data.receipts)) if (receipt.status === 'pending') receipt.status = 'unknown';
    this.save();
  }
  save() {
    const temp = this.file + '.tmp';
    const fd = fs.openSync(temp, 'w', 0o600);
    try { fs.writeFileSync(fd, JSON.stringify(this.data, null, 2)); fs.fsyncSync(fd); } finally { fs.closeSync(fd); }
    fs.renameSync(temp, this.file);
    const dir = fs.openSync(path.dirname(this.file), 'r');
    try { fs.fsyncSync(dir); } finally { fs.closeSync(dir); }
  }
  ingest(ctx) {
    if (!ctx.MessageSid || !ctx.From || !ctx.To) throw Error('INVALID_INBOUND_CONTEXT');
    const key = JSON.stringify([ctx.AccountId, ctx.MessageSidFull ?? ctx.MessageSid]);
    if (this.data.events.some(event => event.key === key)) return;
    this.data.events.push({key, cursor: this.data.events.length + 1, context: ctx});
    this.save();
  }
}
