import fs from 'node:fs';
import path from 'node:path';
import crypto from 'node:crypto';
import os from 'node:os';
import {spawn, execFileSync} from 'node:child_process';

if (process.versions.bun) throw Error('Run this comparison driver with Node to keep fixtures and assertions identical.');
const [bunExecutable, selected = 'all'] = process.argv.slice(2);
if (!bunExecutable || !path.isAbsolute(bunExecutable) || !['all', 'weixin', 'wecom', 'feishu'].includes(selected)) throw Error('Usage: node runtime-compare.mjs /absolute/path/to/bun [all|weixin|wecom|feishu]');
const root = import.meta.dirname;
const directory = path.join(root, 'artifacts', `runtime-compare-${new Date().toISOString().replace(/[:.]/g, '-')}`);
fs.mkdirSync(directory, {recursive: true});
const sha256 = bytes => crypto.createHash('sha256').update(bytes).digest('hex');
function sourceDigest() {
  const files = ['host.mjs', 'runtime.mjs', 'profiles.mjs', 'network.mjs', 'store.mjs', 'loader.mjs', 'fixtures.mjs', 'feishu-frame.mjs', 'e2e.mjs', 'runtime-compare.mjs', 'package.json', 'package-lock.json', ...fs.readdirSync(path.join(root, 'shim')).filter(name => /\.(mjs|json)$/.test(name)).map(name => `shim/${name}`)].sort();
  return sha256(files.map(file => `${file}\0${sha256(fs.readFileSync(path.join(root, file)))}`).join('\n'));
}
const report = {
  startedAt: new Date().toISOString(), sourceDigest: sourceDigest(), driver: {executable: process.execPath, version: process.version},
  platform: {os: process.platform, arch: process.arch, release: os.release()},
  coverage: 'Identical Node driver, fixtures, assertions, dependency tree and host code; only host executable changes. Sequential synthetic text scenarios, not a performance benchmark or shared multi-account validation.',
  runtimes: [
    {name: 'node', executable: process.execPath}, {name: 'bun', executable: bunExecutable},
  ].map(runtime => ({...runtime, version: execFileSync(runtime.executable, ['--version'], {encoding: 'utf8'}).trim(), binarySha256: sha256(fs.readFileSync(runtime.executable))})),
  cases: [],
};
const reportFile = path.join(directory, 'report.json');
const save = () => fs.writeFileSync(reportFile, JSON.stringify(report, null, 2));
save();
async function run(runtime, kind) {
  const name = `${runtime.name}-${kind}`;
  const stdoutFile = path.join(directory, `${name}.stdout.log`), stderrFile = path.join(directory, `${name}.stderr.log`);
  const child = spawn(process.execPath, [path.join(root, 'e2e.mjs'), kind, '--runtime', runtime.executable], {cwd: root, detached: true, stdio: ['ignore', 'pipe', 'pipe']});
  const output = fs.createWriteStream(stdoutFile), errors = fs.createWriteStream(stderrFile);
  let stdout = '', timedOut = false;
  child.stdout.on('data', chunk => {stdout += chunk; output.write(chunk);});
  child.stderr.pipe(errors);
  const started = Date.now();
  const timer = setTimeout(() => {timedOut = true; try {process.kill(-child.pid, 'SIGKILL');} catch {}}, 90000);
  const exit = await new Promise((resolve, reject) => {child.once('error', reject); child.once('close', (code, signal) => resolve({code, signal}));}).finally(() => clearTimeout(timer));
  await new Promise(resolve => output.end(resolve));
  let summary, detail, parseError;
  try {summary = JSON.parse(stdout.slice(stdout.lastIndexOf('\n{') + 1)); detail = JSON.parse(fs.readFileSync(summary.report));}
  catch (error) {parseError = String(error);}
  const result = {runtime: runtime.name, kind, elapsedMs: Date.now() - started, ...exit, timedOut, stdoutFile, stderrFile, summary, detail, parseError};
  result.passed = exit.code === 0 && !timedOut && detail?.harnessPassed === true;
  const pids = detail?.scenarios.flatMap(scenario => scenario.processes) ?? [];
  result.survivingHostPids = pids.filter(pid => {try {process.kill(pid, 0); return true;} catch {return false;}});
  if (result.survivingHostPids.length) {
    for (const pid of result.survivingHostPids) {try {process.kill(pid, 'SIGKILL');} catch {}}
    result.passed = false;
  }
  report.cases.push(result); save();
  console.log(`${name}: ${result.passed ? 'transport passed' : 'execution failed'}, policyCompatible=${detail?.policyCompatible ?? 'unavailable'}`);
}
for (const runtime of report.runtimes) for (const kind of selected === 'all' ? ['weixin', 'wecom', 'feishu'] : [selected]) await run(runtime, kind);
report.sourceDigestAfter = sourceDigest();
report.sourceUnchanged = report.sourceDigestAfter === report.sourceDigest;
report.finishedAt = new Date().toISOString();
report.comparison = (selected === 'all' ? ['weixin', 'wecom', 'feishu'] : [selected]).map(kind => {
  const cases = report.cases.filter(result => result.kind === kind);
  const [node, bun] = cases.map(result => result.detail?.scenarios[0]);
  return {kind, nodePassed: cases[0]?.passed, bunPassed: cases[1]?.passed,
    samePlugin: Boolean(node && bun && node.hashBefore === bun.hashBefore && node.hashBefore === node.hashAfter && bun.hashBefore === bun.hashAfter && node.npmIntegrity === bun.npmIntegrity),
    sameAssertions: Boolean(node && bun && JSON.stringify(node.assertions) === JSON.stringify(bun.assertions)),
    nodeAssertions: node?.assertions.length, bunAssertions: bun?.assertions.length,
    nodeLoader: node?.loader, bunLoader: bun?.loader,
    nodePolicyCompatible: node?.policyCompatible, bunPolicyCompatible: bun?.policyCompatible};
});
report.transportPassed = report.sourceUnchanged && report.cases.every(result => result.passed) && report.comparison.every(result => result.samePlugin && result.sameAssertions);
report.policyCompatible = report.cases.every(result => result.detail?.policyCompatible === true);
save();
const rows = report.comparison.map(result => `| ${result.kind} | ${result.nodePassed ? 'pass' : 'fail'} (${result.nodeAssertions ?? 0}) | ${result.bunPassed ? 'pass' : 'fail'} (${result.bunAssertions ?? 0}) | ${result.nodeLoader ?? '-'} / ${result.bunLoader ?? '-'} | ${result.nodePolicyCompatible} / ${result.bunPolicyCompatible} |`).join('\n');
fs.writeFileSync(path.join(directory, 'summary.md'), `# Runtime comparison\n\nPlatform: ${process.platform}/${process.arch}. Node: ${report.runtimes[0].version}. Bun: ${report.runtimes[1].version}.\n\n| Plugin | Node assertions | Bun assertions | Loader Node / Bun | Policy Node / Bun |\n| --- | --- | --- | --- | --- |\n${rows}\n\nSource unchanged: ${report.sourceUnchanged}. Transport passed: ${report.transportPassed}. Policy compatible: ${report.policyCompatible}.\n\n${report.coverage}\n\nReproduce from this directory's project root:\n\n\`\`\`sh\n${process.execPath} runtime-compare.mjs ${bunExecutable} ${selected}\n\`\`\`\n\nSee report.json and per-case transcripts for errors, policy evidence and process cleanup. Elapsed times are scenario durations, not runtime benchmarks.\n`);
console.log(JSON.stringify({report: reportFile, transportPassed: report.transportPassed, policyCompatible: report.policyCompatible}, null, 2));
if (!report.transportPassed) process.exitCode = 1;
