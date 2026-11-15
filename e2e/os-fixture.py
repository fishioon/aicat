import fcntl
import json
import os
from pathlib import Path
import sys
import tempfile

path = Path(sys.argv[1])
updates = json.load(sys.stdin)
with open(str(path) + '.lock', 'a') as lock:
    os.chmod(lock.name, 0o600)
    fcntl.flock(lock, fcntl.LOCK_EX)
    state = json.loads(path.read_text())
    if updates:
        state.update(updates)
        descriptor, temporary = tempfile.mkstemp(prefix='.fixture-', dir=path.parent)
        try:
            with os.fdopen(descriptor, 'w') as output:
                json.dump(state, output)
            os.replace(temporary, path)
        finally:
            if os.path.exists(temporary):
                os.unlink(temporary)
    json.dump(state, sys.stdout)
