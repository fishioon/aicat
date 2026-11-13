import { Source } from "./source";
try {
  const source = new Source(await Bun.stdin.json());
  try {
    source.refresh();
  } finally {
    source.close();
  }
  process.stdout.write('{"validated":true}\n');
} catch {
  process.stdout.write('{"validated":false}\n');
  process.exitCode = 1;
}
