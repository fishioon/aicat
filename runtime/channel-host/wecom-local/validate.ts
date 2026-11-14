import { Source } from "./source";
try {
  const config = await Bun.stdin.json();
  const source = new Source(config);
  try {
    source.refresh();
    for (const room of Object.keys(config.replyRooms ?? {}))
      source.validateRoom(room);
  } finally {
    source.close();
  }
  process.stdout.write('{"validated":true}\n');
} catch {
  process.stdout.write('{"validated":false}\n');
  process.exitCode = 1;
}
