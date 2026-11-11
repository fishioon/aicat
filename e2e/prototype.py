#!/usr/bin/env python3
"""Unified acceptance: real WebView + browser UI, CLI HTTP/TLS, rules/plugins/MCP.
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
    parser.add_argument('--cli', default='bin/aicat-rules-e2e')
    parser.add_argument('--output')
    parser.add_argument('--only', choices=('desktop', 'transport', 'rules'))
    args = parser.parse_args()
    root = Path(args.output).resolve() if args.output else Path(tempfile.mkdtemp(prefix='full-e2e-', dir=Path('build').resolve()))
    root.mkdir(parents=True, exist_ok=True, mode=0o700)
    report = {'passed': False, 'stages': {}, 'arguments': vars(args), 'startedAt': time.strftime('%Y-%m-%dT%H:%M:%S%z'), 'scope': 'complete WebView desktop, HTTP/TLS transport, rules/plugins/MCP'}
    stages = {
        'desktop': ['node', 'e2e/desktop.cjs', args.binary, str(root / 'desktop')],
        'transport': ['python3', 'e2e/listener.py', '--binary', args.cli, '--transport-only', '--output', str(root / 'transport')],
        'rules': ['python3', 'e2e/rules.py', '--binary', args.cli, '--output', str(root / 'rules')],
    }
    try:
        for name, command in stages.items():
            if args.only and name != args.only:
                continue
            print(f'E2E: {name}', flush=True)
            log = root / f'{name}.log'
            with log.open('w') as output:
                completed = subprocess.run(command, stdout=output, stderr=subprocess.STDOUT)
            sub = root / name / 'report.json'
            result = json.loads(sub.read_text()) if sub.exists() else {'passed': False}
            report['stages'][name] = {'passed': completed.returncode == 0 and result.get('passed', False), 'report': str(sub), 'log': str(log), 'command': command}
            if not report['stages'][name]['passed']:
                print(log.read_text()[-5000:])
                raise RuntimeError(f'{name} failed: {sub}')
        report['passed'] = True
    finally:
        report['sources'] = {str(p): hashlib.sha256(p.read_bytes()).hexdigest() for folder in ('cmd', 'internal', 'ui', 'e2e') for p in Path(folder).rglob('*') if p.is_file() and p.suffix in ('.go', '.js', '.ts', '.vue', '.cjs', '.css', '.html', '.py', '.json') and not {'node_modules', 'dist', 'dist-e2e'}.intersection(p.parts)}
        for name, binary in [('desktop', args.binary), ('cli', args.cli)]:
            path = Path(binary)
            if path.exists():
                (root / f'{name}-dependencies.txt').write_bytes(subprocess.check_output(['go', 'version', '-m', str(path)]))
        (root / 'report.json').write_text(json.dumps(report, ensure_ascii=False, indent=2))
        print(json.dumps({'passed': report['passed'], 'artifacts': str(root)}, ensure_ascii=False), flush=True)


if __name__ == '__main__':
    main()
