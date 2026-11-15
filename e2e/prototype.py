#!/usr/bin/env python3
"""Unified acceptance: real WebView + browser UI, CLI HTTP/TLS, traffic/MCP.
Each stage uses an isolated Home, local fixtures, fake OS adapters and artifacts.
Run only after the migration is complete; --only scopes later focused work.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import subprocess
import tempfile
import time


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--binary', default='bin/aicat-e2e')
    parser.add_argument('--cli', default='bin/aicat-cli-e2e')
    parser.add_argument('--output')
    parser.add_argument('--only', choices=('desktop', 'transport', 'mcp'))
    parser.add_argument('--background', action='store_true')
    parser.add_argument('--scenarios', nargs='+')
    args = parser.parse_args()
    if args.scenarios and not args.background:
        parser.error('--scenarios requires --background')
    if args.background and args.only:
        parser.error('--background runs the complete background suite; do not combine with --only')
    root = Path(args.output).resolve() if args.output else Path(tempfile.mkdtemp(prefix='full-e2e-', dir=Path('build').resolve()))
    root.mkdir(parents=True, exist_ok=True, mode=0o700)
    report = {'passed': False, 'stages': {}, 'arguments': vars(args), 'startedAt': time.strftime('%Y-%m-%dT%H:%M:%S%z'), 'scope': 'complete WebView desktop, HTTP/TLS transport, traffic/MCP'}
    stages = {
        'desktop': ['node', 'e2e/desktop.cjs', args.binary, str(root / 'desktop')],
        'transport': ['python3', 'e2e/listener.py', '--binary', args.cli, '--transport-only', '--output', str(root / 'transport')],
        'mcp': ['python3', 'e2e/mcp.py', '--binary', args.cli, '--output', str(root / 'mcp')],
    }
    if args.background:
        report['scope'] = 'headless Chromium with hidden desktop app, HTTP/TLS, MCP, extensions and message fixtures'
        report['excluded'] = ['native window and WKWebView interaction', 'real Keychain and system proxy authorization', 'live accounts and real message delivery']
        stages.pop('desktop')
        for name, script in (
            ('tray', 'tray'), ('traffic-default', 'traffic-default'), ('tunnel', 'tunnel'),
            ('shell', 'redesign'), ('provider', 'provider'), ('codex', 'codex'),
            ('branding', 'branding'), ('titlebar-navigation', 'titlebar-navigation'),
            ('extension-icons', 'extension-icons'), ('extension-categories', 'extension-categories'),
            ('extension-lifecycle', 'extension-lifecycle'), ('settings-navigation', 'settings-navigation'),
            ('extension-source', 'extension-source'),
            ('wecom', 'wecom'), ('wecom-agent', 'wecom'),
            ('rooms', 'rooms'), ('messages', 'messages'), ('schedules', 'schedules'),
        ):
            stages[name] = ['node', f'e2e/{script}.cjs', args.binary, str(root / name)]
        for name in ('overview', 'connect', 'provider-lifecycle', 'provider-model-state', 'inspector-copy', 'proxy-settings', 'certificate', 'certificate-entries', 'traffic-browse', 'traffic-content', 'traffic-events', 'traffic-lifecycle', 'http-origins', 'agent-traffic', 'control-traffic', 'busy-state', 'desktop-shell', 'navigation', 'extension-details', 'ipv6-tunnel', 'system-proxy'):
            stages[name] = ['node', f'e2e/{name}.cjs', args.binary, str(root / name)]
        for name in ('app-tree', 'list-selection'):
            stages[name] = ['node', f'e2e/{name}-headless.cjs', args.binary, str(root / name)]
        for name, flag in (('provider-appearance', '--appearance'), ('provider-model-path', '--model-path'), ('overview-provider', '--overview-provider')):
            stages[name] = ['node', 'e2e/provider.cjs', args.binary, str(root / name), flag]
        stages['wecom-agent'].append('agent')
        stages['tray-assets'] = ['node', 'e2e/tray-assets.cjs', str(root / 'tray-assets')]
        stages['websocket'] = [str(Path(args.binary).resolve().parent / 'websocket-e2e'), str(root / 'websocket')]
        stages['channel-install-failure'] = ['node', 'e2e/channel-install-failure.cjs', args.binary, str(root / 'channel-install-failure')]
    if args.scenarios:
        unknown = set(args.scenarios) - stages.keys()
        if unknown:
            parser.error('Unknown scenarios: ' + ', '.join(sorted(unknown)))
        stages = {name: command for name, command in stages.items() if name in args.scenarios}
    try:
        for name, command in stages.items():
            if args.only and name != args.only:
                continue
            print(f'E2E: {name}', flush=True)
            log = root / f'{name}.log'
            clipboard = subprocess.check_output(['/usr/bin/pbpaste']) if args.background else None
            try:
                with log.open('w') as output:
                    completed = subprocess.run(command, stdout=output, stderr=subprocess.STDOUT, env={**os.environ, **({'AICAT_E2E_BACKGROUND': '1'} if args.background else {})})
            finally:
                if clipboard is not None:
                    subprocess.run(['/usr/bin/pbcopy'], input=clipboard, check=True)
            sub = root / name / 'report.json'
            result = json.loads(sub.read_text()) if sub.exists() else {'passed': False}
            report['stages'][name] = {'passed': completed.returncode == 0 and result.get('passed', False), 'report': str(sub), 'log': str(log), 'command': command}
            if not report['stages'][name]['passed']:
                print(log.read_text()[-5000:])
                if not args.background:
                    raise RuntimeError(f'{name} failed: {sub}')
        report['passed'] = all(stage['passed'] for stage in report['stages'].values())
    finally:
        report['sources'] = {str(p): hashlib.sha256(p.read_bytes()).hexdigest() for folder in ('cmd', 'internal', 'ui', 'e2e', 'runtime', 'assets', 'scripts', 'patches') for p in Path(folder).rglob('*') if p.is_file() and p.suffix in ('.go', '.js', '.ts', '.vue', '.cjs', '.css', '.html', '.py', '.json', '.mjs', '.patch') and not {'node_modules', 'dist', 'dist-e2e'}.intersection(p.parts)}
        for p in map(Path, ('Makefile', 'go.mod', 'go.sum')):
            report['sources'][str(p)] = hashlib.sha256(p.read_bytes()).hexdigest()
        report['binaries'] = {}
        for name, binary in [('desktop', args.binary), ('cli', args.cli)]:
            path = Path(binary)
            if path.exists():
                report['binaries'][name] = {'path': str(path.resolve()), 'sha256': hashlib.sha256(path.read_bytes()).hexdigest()}
                (root / f'{name}-dependencies.txt').write_bytes(subprocess.check_output(['go', 'version', '-m', str(path)]))
        (root / 'report.json').write_text(json.dumps(report, ensure_ascii=False, indent=2))
        print(json.dumps({'passed': report['passed'], 'artifacts': str(root)}, ensure_ascii=False), flush=True)
    if not report['passed']:
        raise SystemExit(1)


if __name__ == '__main__':
    main()
