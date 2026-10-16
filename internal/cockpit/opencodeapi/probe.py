#!/usr/bin/env python3
"""Fetch /doc from an isolated, authenticated server without credentials or prompts."""
import base64
import os
from pathlib import Path
import re
import secrets
import selectors
import signal
import subprocess
import sys
import tempfile
import time
import urllib.request

with tempfile.TemporaryDirectory(prefix="opencode-api-") as scratch:
    root = Path(scratch)
    env = {k: v for k, v in os.environ.items()
           if k in {"PATH", "SYSTEMROOT", "TMPDIR", "TMP", "TEMP", "SSL_CERT_FILE", "SSL_CERT_DIR"}}
    for key in ("HOME", "XDG_DATA_HOME", "XDG_CONFIG_HOME", "XDG_CACHE_HOME", "XDG_STATE_HOME"):
        target = root / key
        target.mkdir()
        env[key] = str(target)
    password = secrets.token_hex(32)
    env.update(OPENCODE_SERVER_PASSWORD=password, OPENCODE_SERVER_USERNAME="opencode",
               OPENCODE_DISABLE_AUTOUPDATE="1", OPENCODE_DISABLE_CLAUDE_CODE="1",
               OPENCODE_DISABLE_EXTERNAL_SKILLS="1", OPENCODE_PURE="1")
    with (root / "stderr").open("w") as stderr:
        process = subprocess.Popen(["opencode", "serve", "--hostname", "127.0.0.1", "--port", "0"],
                                   cwd=root, env=env, stdout=subprocess.PIPE,
                                   stderr=stderr, start_new_session=True)
        try:
            selector = selectors.DefaultSelector()
            selector.register(process.stdout, selectors.EVENT_READ)
            deadline = time.monotonic() + 60
            output = b""
            while time.monotonic() < deadline:
                if not selector.select(timeout=1):
                    continue
                chunk = os.read(process.stdout.fileno(), 65536)
                if not chunk:
                    raise RuntimeError("opencode serve exited before becoming ready")
                output += chunk
                match = re.search(rb"http://127\.0\.0\.1:\d+", output)
                if match:
                    auth = base64.b64encode(("opencode:" + password).encode()).decode()
                    req = urllib.request.Request(match[0].decode() + "/doc", headers={"Authorization": "Basic " + auth})
                    with urllib.request.urlopen(req, timeout=30) as response:
                        Path(sys.argv[1]).write_bytes(response.read())
                    print("Fetched authenticated opencode /doc without a model request")
                    break
            else:
                raise TimeoutError("opencode serve startup timed out")
        finally:
            selector.close()
            os.killpg(process.pid, signal.SIGKILL)
            process.wait()
