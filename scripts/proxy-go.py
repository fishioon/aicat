import hashlib
import json
import os
import pathlib
import shlex
import shutil
import subprocess
import sys
import tempfile


root = pathlib.Path(__file__).resolve().parent.parent
module = json.loads(subprocess.check_output(
    ['go', 'mod', 'download', '-json', 'github.com/lqqyt2423/go-mitmproxy@v1.9.4'], cwd=root))
source = pathlib.Path(module['Dir'])
patch = root / 'patches/go-mitmproxy-v1.9.4-websocket.patch'
digest = hashlib.sha256(patch.read_bytes()).hexdigest()
cache = root / 'build/proxy-websocket-fix'
cache.mkdir(parents=True, exist_ok=True)
prepared = cache / digest
if not prepared.exists():
    temporary = pathlib.Path(tempfile.mkdtemp(prefix='prepare-', dir=cache))
    try:
        shutil.copytree(source, temporary / 'module')
        (temporary / 'module').chmod(0o755)
        for target in (temporary / 'module').rglob('*'):
            if target.is_file():
                target.chmod(0o644)
            elif target.is_dir():
                target.chmod(0o755)
        module_dir = temporary / 'module'
        subprocess.run(['patch', '--batch', '-p1', '-i', str(patch)], cwd=module_dir, check=True, stdout=sys.stderr)
        if 'func (wsData *WebSocketData) Snapshot()' not in (module_dir / 'proxy/flow.go').read_text():
            raise RuntimeError('WebSocket patch was not applied')
        try:
            temporary.rename(prepared)
        except FileExistsError:
            shutil.rmtree(temporary)
    except BaseException:
        shutil.rmtree(temporary, ignore_errors=True)
        raise
if not (prepared / 'module/go.mod').is_file():
    raise SystemExit('Remove the incomplete prepared dependency directory: ' + str(prepared))
contents = (root / 'go.mod').read_text() + '\nreplace github.com/lqqyt2423/go-mitmproxy => ' + json.dumps(str(prepared / 'module')) + '\n'
mod_digest = hashlib.sha256(contents.encode() + (root / 'go.sum').read_bytes()).hexdigest()
modfile = cache / ('go-' + mod_digest + '.mod')
if not modfile.exists():
    modfile.write_text(contents)
    shutil.copyfile(root / 'go.sum', modfile.with_suffix('.sum'))
if sys.argv[1:] == ['--print-modfile']:
    print(modfile)
else:
    env = dict(os.environ)
    flags = shlex.split(env.get('GOFLAGS', ''))
    if any(flag.startswith('-modfile') for flag in flags):
        raise SystemExit('GOFLAGS already selects a Go modfile')
    env['GOFLAGS'] = shlex.join(flags + ['-modfile=' + str(modfile)])
    raise SystemExit(subprocess.call(['go', *sys.argv[1:]], cwd=root, env=env))
