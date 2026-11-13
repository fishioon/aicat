import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import time


def digest(path):
    return hashlib.sha256(Path(path).read_bytes()).hexdigest()


def scenarios(binary, cli, root):
    result = {}
    for name in ['tray', 'overview', 'traffic-default', 'traffic-browse', 'traffic-content', 'traffic-events', 'ipv6-tunnel', 'traffic-lifecycle', 'inspector-copy', 'extension-details', 'extension-icons', 'agent-traffic', 'control-traffic', 'busy-state', 'desktop-shell', 'http-origins', 'proxy-settings', 'system-proxy', 'certificate-entries', 'certificate', 'tunnel', 'provider-lifecycle', 'provider-model-state', 'redesign', 'provider', 'codex', 'connect', 'channel-install-failure']:
        result[name] = ['node', 'e2e/' + name + '.cjs', binary, str(root / name)]
    result['tray-assets'] = ['node', 'e2e/tray-assets.cjs', str(root / 'tray-assets')]
    for name, flag in [('provider-appearance', '--appearance'), ('provider-model-path', '--model-path'), ('overview-provider', '--overview-provider')]:
        result[name] = ['node', 'e2e/provider.cjs', binary, str(root / name), flag]
    for name, flag in [('wecom', None), ('wecom-agent', 'agent'), ('wecom-ui', 'ui')]:
        result[name] = ['node', 'e2e/wecom.cjs', binary, str(root / name)] + ([flag] if flag else [])
    result['transport'] = ['python3', 'e2e/listener.py', '--binary', cli, '--transport-only', '--output', str(root / 'transport')]
    result['mcp'] = ['python3', 'e2e/mcp.py', '--binary', cli, '--output', str(root / 'mcp')]
    result['wecom-local'] = ['python3', 'e2e/wecom-local.py', '--binary', cli, '--output', str(root / 'wecom-local')]
    result['wecom-local-send'] = ['python3', 'e2e/wecom-local-send.py', '--binary', cli, '--output', str(root / 'wecom-local-send')]
    result['wecom-local-fixture'] = ['python3', 'e2e/wecom-local-fixture.py', '--output', str(root / 'wecom-local-fixture')]
    result['wecom-local-ui'] = ['node', 'e2e/wecom-local-ui.cjs', binary, str(root / 'wecom-local-fixture'), str(root / 'wecom-local-ui'), 'send-review']
    result['wecom-local-errors'] = ['node', 'e2e/wecom-local-ui.cjs', binary, str(root / 'wecom-local-fixture'), str(root / 'wecom-local-errors'), 'error-review']
    return result


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--binary', default='bin/aicat-e2e')
    parser.add_argument('--cli', default='bin/aicat-cli-e2e')
    parser.add_argument('--output')
    parser.add_argument('--only', nargs='+')
    parser.add_argument('--list', action='store_true')
    args = parser.parse_args()
    root = Path(args.output or 'build/headless-acceptance-' + time.strftime('%Y%m%d-%H%M%S')).resolve()
    binary, cli = str(Path(args.binary).resolve()), str(Path(args.cli).resolve())
    commands = scenarios(binary, cli, root)
    if args.list:
        print('\n'.join(commands))
        return
    selected = set(args.only or commands)
    if selected - commands.keys():
        parser.error('Unknown scenarios: ' + ', '.join(sorted(selected - commands.keys())))
    if {'wecom-local-ui', 'wecom-local-errors'} & selected:
        selected.add('wecom-local-fixture')
    os.umask(0o077)
    root.mkdir(parents=True, exist_ok=False)
    tracked = subprocess.check_output(['git', 'ls-files', '-z']).decode().split('\0')
    sources = {name: digest(name) for name in tracked if name and Path(name).is_file() and Path(name).suffix in {'.go', '.ts', '.vue', '.css', '.cjs', '.mjs', '.py', '.json', '.md', '.applescript'} and not name.startswith('build/')}
    report = {'passed': False, 'status': 'running', 'scope': 'Selected headless browser, real application services and local protocol fixtures; native windows, system authorization and real platform messages are not covered', 'startedAt': time.strftime('%Y-%m-%dT%H:%M:%S%z'), 'commit': subprocess.check_output(['git', 'rev-parse', 'HEAD']).decode().strip(), 'sources': sources, 'binaries': {binary: digest(binary), cli: digest(cli)}, 'stages': {}, 'nativeCoverage': 'not run', 'pendingNative': ['WKWebView pointer behavior and window hide/Dock/tray restore', 'Native tray menu pointer input', 'Real certificate authorization, keychain and save panel', 'Real system network proxy and recovery', 'Real third-party clients and WeCom native sending require an approved target/account']}
    groups = {
        'desktop': ['tray', 'tray-assets', 'overview', 'desktop-shell', 'busy-state', 'overview-provider'],
        'extensions': ['extension-details', 'extension-icons', 'channel-install-failure', 'traffic-default', 'redesign', 'wecom', 'wecom-local-ui'],
        'proxy': ['transport', 'proxy-settings', 'system-proxy', 'http-origins'],
        'https': ['certificate', 'certificate-entries', 'tunnel', 'ipv6-tunnel'],
        'traffic-browse': ['traffic-browse', 'traffic-default', 'traffic-lifecycle', 'redesign'],
        'traffic-inspect': ['traffic-content', 'traffic-events', 'traffic-lifecycle', 'inspector-copy'],
        'providers': ['provider', 'provider-appearance', 'provider-model-path', 'overview-provider', 'provider-lifecycle', 'provider-model-state'],
        'codex': ['codex', 'extension-details'],
        'wecom': ['wecom', 'wecom-agent', 'wecom-ui', 'channel-install-failure'],
        'wecom-local': ['wecom-local', 'wecom-local-send', 'wecom-local-ui', 'wecom-local-errors'],
        'agent-access': ['agent-traffic', 'control-traffic', 'connect', 'mcp'],
    }
    matrix = {}
    for area, tests in groups.items():
        source = Path('docs/feature-map') / (area + '.md')
        content = source.read_text()
        features = re.findall(r'^- `([^`]+)`', content, re.M)
        if not features:
            features = {'wecom': ['安装与来源', '账号验证', '启停连接状态', '私聊接收', '显式发送与回执', 'Codex 自动处理', '断线与进程恢复', 'Room 日志与诊断'], 'wecom-local': ['账号与 Base64 密钥配置验证', '密钥私有存储', '扩展启停', '历史与 WAL 增量', '游标崩溃恢复', 'Room 去重与上下文', 'MCP 发送回读确认', '幂等与未知结果', '诊断状态与日志']}[area]
        matrix[area] = {'map': str(source), 'subfeatures': features, 'scenarios': tests, 'reports': [str(root / name / 'report.json') for name in tests], 'status': 'not evaluated; reports must be reviewed for each requirement'}
    (root / 'feature-matrix.json').write_text(json.dumps(matrix, ensure_ascii=False, indent=2) + '\n')
    def save():
        temporary = root / 'report.next'
        temporary.write_text(json.dumps(report, ensure_ascii=False, indent=2) + '\n')
        temporary.replace(root / 'report.json')
    save()
    for name, command in commands.items():
        if name not in selected:
            continue
        entry = {'status': 'running', 'command': command, 'log': str(root / (name + '.log')), 'startedAt': time.strftime('%Y-%m-%dT%H:%M:%S%z')}
        report['stages'][name] = entry
        clipboard = subprocess.check_output(['/usr/bin/pbpaste'])
        print('E2E: ' + name, flush=True)
        try:
            with open(entry['log'], 'w') as output:
                process = subprocess.Popen(command, stdout=output, stderr=subprocess.STDOUT, env={**os.environ, 'AICAT_E2E_BACKGROUND': '1'})
                entry['pid'] = process.pid
                save()
                entry['exitCode'] = process.wait()
        except OSError as error:
            entry.update(exitCode=None, error=str(error))
        finally:
            subprocess.run(['/usr/bin/pbcopy'], input=clipboard, check=True)
        sub = root / name / 'report.json'
        try:
            value = json.loads(sub.read_text()) if sub.exists() else {}
        except json.JSONDecodeError as error:
            value = {}
            entry['error'] = str(error)
        entry.update(status='finished', passed=entry['exitCode'] == 0 and value.get('passed') is True, report=str(sub), finishedAt=time.strftime('%Y-%m-%dT%H:%M:%S%z'))
        if sub.exists():
            entry['reportSHA256'] = digest(sub)
        print(name + ': ' + ('PASS' if entry['passed'] else 'FAIL'), flush=True)
        save()
    report['passed'] = all(entry.get('passed') for entry in report['stages'].values())
    report['status'] = 'finished'
    report['finishedAt'] = time.strftime('%Y-%m-%dT%H:%M:%S%z')
    report['sourcesUnchanged'] = all(Path(name).is_file() and digest(name) == hash for name, hash in sources.items())
    report['passed'] = report['passed'] and report['sourcesUnchanged']
    save()
    print(json.dumps({'passed': report['passed'], 'report': str(root / 'report.json')}, ensure_ascii=False), flush=True)
    raise SystemExit(0 if report['passed'] else 1)


if __name__ == '__main__':
    main()
