#!/usr/bin/env python3
"""Build acceptance failure modes: generation starts services/touches Home,
stale bindings, untyped RPC/DTO drift, dev URL overrides production, or preview
code/source assets ship. No OS adapters or running gateway are needed here.
"""
import hashlib
import json
import os
from pathlib import Path
import socket
import subprocess
import tempfile
import time
import zipfile


def main():
    root = Path(tempfile.mkdtemp(prefix='build-e2e-', dir=Path('build').resolve()))
    binary = Path('bin/aicat').resolve()
    report = {'passed': False, 'checks': [], 'startedAt': time.strftime('%Y-%m-%dT%H:%M:%S%z')}
    try:
        with socket.socket() as occupied:
            occupied.bind(('127.0.0.1', 0))
            occupied.listen()
            generated = root / 'generated.ts'
            home = root / 'generation-home-must-not-exist'
            preview = root / 'preview-must-not-exist'
            result = subprocess.run([str(binary), '--home', str(home), '--addr', f'127.0.0.1:{occupied.getsockname()[1]}'], env={**os.environ, 'MYGO_GENERATE': str(generated), 'AICAT_E2E_PREVIEW': str(preview)}, capture_output=True, text=True, timeout=20)
            (root / 'generation.log').write_text(result.stdout + result.stderr)
            assert result.returncode == 0, result.stderr
            assert not home.exists(), 'generation initialized application data'
            assert not preview.exists(), 'production enabled preview'
            # trimpath strips Go source locations; production generation loses
            # parameter names/docs, while the reflected wire types stay exact.
            reflected = generated.read_text().split('// ---- Services ----')[0]
            expected = Path('ui/src/generated/mygo.ts').read_text().split('// ---- Services ----')[0]
            assert reflected == expected, 'stale DTO bindings'
        report['checks'].append('production binary generates current bindings without Home, listeners, preview or GUI startup')
        info = subprocess.check_output(['go', 'version', '-m', str(binary)], text=True)
        (root / 'dependencies.txt').write_text(info)
        assert 'mygo_noinspector' in info
        report['checks'].append('inspector exclusion recorded in release binary; production mode is asserted by desktop E2E')
        assets = [p for p in Path('ui/dist').rglob('*') if p.is_file()]
        assert assets and Path('ui/dist/index.html').is_file()
        total = 0
        report['assets'] = {}
        for asset in assets:
            assert asset.suffix in ('.html', '.js', '.css'), str(asset)
            data = asset.read_bytes()
            assert not any(marker in data for marker in (b'X-Aicat-Preview', b'PreviewChannel', b'__aicatPreview', b'/rpc', b'desktopapi:')), str(asset)
            total += len(data)
            report['assets'][str(asset)] = hashlib.sha256(data).hexdigest()
        report['checks'].append('production embeds bundled assets only and excludes E2E preview transport')
        snippet = Path('ui') / f'.contract-{os.getpid()}.ts'
        try:
            snippet.write_text('''import { Desktop, type DesktopState } from './src/generated/mygo';
declare const state: DesktopState;
Desktop.setRecording(true);
Desktop.record(1);
const trusted: boolean = state.certificate.trusted;
// @ts-expect-error a typed ID cannot be a string
Desktop.record('1');
// @ts-expect-error boolean arguments must remain typed
Desktop.setRecording('yes');
// @ts-expect-error no string-command RPC API
Desktop.command('recording', {});
// @ts-expect-error JSON field casing is part of the generated contract
state.certificate.Trusted;
''')
            result = subprocess.run(['node', 'ui/node_modules/typescript/bin/tsc', '--strict', '--skipLibCheck', '--noEmit', '--module', 'esnext', '--moduleResolution', 'bundler', '--target', 'es2022', str(snippet)], capture_output=True, text=True)
            (root / 'types.log').write_text(result.stdout + result.stderr)
            assert result.returncode == 0, result.stdout + result.stderr
        finally:
            snippet.unlink(missing_ok=True)
        report['checks'].append('generated methods reject wrong arguments, ad-hoc commands and old DTO casing')
        bundle = Path('build/desktop/aicat.app')
        archive = root / 'aicat-macos.zip'
        with zipfile.ZipFile(archive, 'w', zipfile.ZIP_DEFLATED) as out:
            for file in bundle.rglob('*'):
                if file.is_file(): out.write(file, file.relative_to(bundle.parent))
        report['sizes'] = {'frontendBytes': total, 'binaryBytes': binary.stat().st_size, 'appBytes': sum(p.stat().st_size for p in bundle.rglob('*') if p.is_file()), 'zipBytes': archive.stat().st_size}
        report['binarySHA256'] = hashlib.sha256(binary.read_bytes()).hexdigest()
        report['passed'] = True
    finally:
        (root / 'report.json').write_text(json.dumps(report, ensure_ascii=False, indent=2))
        print(json.dumps({'passed': report['passed'], 'artifacts': str(root)}, ensure_ascii=False))


if __name__ == '__main__':
    main()
