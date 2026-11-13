import argparse
import hashlib
import json
import os
import platform
import sys
from pathlib import Path
import subprocess
import tarfile
import urllib.request

VERSION = '2.7.0'
SHA256 = 'd27e77851d4750dd3716cccab6e13e62e092a11d3adee7e3dc30aedd2e34d9dd'
URL = 'https://codeload.github.com/utelle/SQLite3MultipleCiphers/tar.gz/refs/tags/v' + VERSION


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--output', required=True)
    parser.add_argument('--archive', default=os.environ.get('AICAT_SQLITE3MC_SOURCE'))
    args = parser.parse_args()
    root = Path(args.output).resolve()
    root.mkdir(parents=True, exist_ok=True)
    if sys.platform != 'darwin':
        raise RuntimeError('Local WeCom SQLite runtime requires macOS')
    library = root / 'libsqlite3mc.dylib'
    manifest_file = root / 'manifest.json'
    identity = {'version': VERSION, 'archiveSHA256': SHA256, 'platform': sys.platform, 'arch': platform.machine(), 'compiler': subprocess.check_output(['clang', '--version'], text=True).splitlines()[0]}
    if library.exists() and manifest_file.exists() and (root / 'LICENSE').exists():
        previous = json.loads(manifest_file.read_text())
        if previous.get('identity') == identity and previous.get('librarySHA256') == hashlib.sha256(library.read_bytes()).hexdigest():
            print(library)
            return
    archive = Path(args.archive).resolve() if args.archive else root / 'source.tar.gz'
    if not archive.exists():
        with urllib.request.urlopen(URL, timeout=45) as response:
            archive.write_bytes(response.read(64 << 20))
    actual = hashlib.sha256(archive.read_bytes()).hexdigest()
    if actual != SHA256:
        raise ValueError('source archive hash mismatch')
    with tarfile.open(archive) as tar:
        for member in tar.getmembers():
            if not (root / member.name).resolve().is_relative_to(root) or member.issym() or member.islnk():
                raise ValueError('unexpected archive path')
        tar.extractall(root)
    source = root / ('SQLite3MultipleCiphers-' + VERSION)
    library = root / 'libsqlite3mc.dylib'
    command = ['clang', '-O2', '-dynamiclib', '-DSQLITE_ENABLE_COLUMN_METADATA', '-DSQLITE_USE_URI=1', '-DHAVE_CIPHER_AEGIS=0', '-DHAVE_CIPHER_ASCON=0', '-DHAVE_CIPHER_CHACHA20=0', '-DHAVE_CIPHER_SQLCIPHER=0', '-DHAVE_CIPHER_AES_256_CBC=0', '-DHAVE_CIPHER_RC4=0', '-Wl,-install_name,@rpath/libsqlite3mc.dylib', '-o', str(library), str(source / 'src/sqlite3mc.c'), '-framework', 'Security']
    with (root / 'build.log').open('w') as log:
        subprocess.run(command, stdout=log, stderr=subprocess.STDOUT, check=True)
    (root / 'LICENSE').write_bytes((source / 'LICENSE').read_bytes())
    manifest = {'identity': identity, 'version': VERSION, 'sourceURL': URL, 'archiveSHA256': actual, 'librarySHA256': hashlib.sha256(library.read_bytes()).hexdigest(), 'command': command, 'upstreamModified': False}
    (root / 'manifest.json').write_text(json.dumps(manifest, indent=2) + '\n')
    print(library)


if __name__ == '__main__':
    main()
