#!/usr/bin/env python3
"""Probe an isolated authenticated server. Never sends prompts or reads credentials."""
import argparse
import base64
from contextlib import contextmanager
import json
import os
from pathlib import Path
import re
import secrets
import selectors
import signal
import subprocess
import tempfile
import threading
import time
import urllib.request


def listening_url(output, version):
    line = output.split(b"\n", 1)[0]
    if version == 2:
        url = json.loads(line)["url"]
    else:
        match = re.search(rb"http://127\.0\.0\.1:\d+", output)
        if not match:
            return None
        url = match[0].decode()
    if not re.fullmatch(r"http://127\.0\.0\.1:\d+", url):
        raise ValueError("non-loopback server address")
    return url


@contextmanager
def server(binary="opencode", version=1):
    with tempfile.TemporaryDirectory(prefix="opencode-api-") as scratch:
        root = Path(scratch)
        env = {k: v for k, v in os.environ.items()
               if k in {"PATH", "SYSTEMROOT", "TMPDIR", "TMP", "TEMP", "SSL_CERT_FILE", "SSL_CERT_DIR"}}
        for key in ("HOME", "XDG_DATA_HOME", "XDG_CONFIG_HOME", "XDG_CACHE_HOME", "XDG_STATE_HOME"):
            target = root / key
            target.mkdir()
            env[key] = str(target)
        password = secrets.token_hex(32)
        env.update(OPENCODE_DISABLE_AUTOUPDATE="1", OPENCODE_DISABLE_CLAUDE_CODE="1",
                   OPENCODE_DISABLE_PROJECT_CONFIG="1", OPENCODE_DISABLE_EXTERNAL_SKILLS="1", OPENCODE_PURE="1")
        env["OPENCODE_PASSWORD" if version == 2 else "OPENCODE_SERVER_PASSWORD"] = password
        args = [binary, "serve", "--hostname", "127.0.0.1", "--port", "0"]
        if version == 2:
            args.append("--stdio")
        with (root / "stderr").open("w") as stderr:
            process = subprocess.Popen(args, cwd=root, env=env, stdin=subprocess.PIPE,
                                       stdout=subprocess.PIPE, stderr=stderr, start_new_session=True)
            selector = selectors.DefaultSelector()
            try:
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
                    if b"\n" not in output:
                        continue
                    url = listening_url(output, version)
                    if url is None:
                        continue
                    auth = base64.b64encode(("opencode:" + password).encode()).decode()
                    def call(method, path, body=None):
                        data = None if body is None else json.dumps(body).encode()
                        req = urllib.request.Request(url + path, data=data, method=method,
                                                     headers={"Authorization": "Basic " + auth, "Content-Type": "application/json"})
                        with urllib.request.urlopen(req, timeout=30) as response:
                            return response.read()
                    drain = threading.Thread(target=lambda: drain_stdout(process.stdout), daemon=True)
                    drain.start()
                    yield call, root
                    break
                else:
                    raise TimeoutError("opencode serve startup timed out (local listening may be blocked)")
            finally:
                selector.close()
                process.stdin.close()
                if version == 2:
                    try:
                        process.wait(timeout=5)
                    except subprocess.TimeoutExpired:
                        os.killpg(process.pid, signal.SIGKILL)
                        process.wait()
                        raise RuntimeError("stdio lease did not shut down the server")
                elif process.poll() is None:
                    os.killpg(process.pid, signal.SIGKILL)
                process.wait()
                process.stdout.close()


def drain_stdout(stdout):
    while stdout.read(65536):
        pass


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("output", type=Path)
    parser.add_argument("--v2", action="store_true")
    parser.add_argument("--binary", default="opencode")
    args = parser.parse_args()
    with server(args.binary, 2 if args.v2 else 1) as (call, _):
        path = "/openapi.json" if args.v2 else "/doc"
        args.output.write_bytes(call("GET", path))
        print("PASS: fetched authenticated " + path + " without a model request")


if __name__ == "__main__":
    main()
