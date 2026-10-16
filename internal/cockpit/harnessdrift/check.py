#!/usr/bin/env python3
"""Non-billable CLI contract checks. --static never launches a vendor CLI."""
import argparse
import json
import os
from pathlib import Path
import re
import selectors
import shutil
import signal
import subprocess
import tempfile
import time

HERE = Path(__file__).resolve().parent
COCKPIT = HERE.parent
VERSIONS = json.loads((HERE / 'versions.json').read_text())
FLAGS = {
    'kimi': ['--version', '--help'],
    'agy': ['--input-format', '--output-format', '--mode', '--conversation',
            '--model', '--effort', '--print-timeout', '--dangerously-skip-permissions'],
}


def check_help(harness, text):
    missing = [flag for flag in FLAGS[harness]
               if not re.search(re.escape(flag) + r'(?=[\s=,<\[]|$)', text)]
    if missing:
        raise ValueError(f'{harness} help lost consumed options: {missing}')


def check_initialize(response):
    result = response.get('result', {})
    if response.get('id') != 1 or response.get('error') or result.get('protocolVersion') != 1 or not isinstance(result.get('agentCapabilities'), dict):
        raise ValueError(f'ACP initialize contract changed: {response}')


def version_message(harness, installed):
    verified = VERSIONS[harness]
    if installed != verified:
        return f'{harness}: installed {installed}, pactify contract baseline {verified}; run pactify sub doctor'
    return f'{harness}: installed {installed}, matches pactify contract baseline'


def agy_events():
    fixture = json.loads((COCKPIT / 'golden/testdata/agy-contract.json').read_text())
    return sorted({frame['event'] for frame in fixture['stdout']})


def check_constants(binary):
    missing = [event for event in agy_events()
               if not re.search(rb'(?<![\w])' + event.encode() + rb'(?![\w])', binary)]
    if missing:
        raise ValueError(f'agy binary lost stream-json event constants: {missing}')


def static_check():
    source = (COCKPIT / 'agy.go').read_text()
    for event in agy_events():
        if f'case "{event}":' not in source:
            raise ValueError(f'agy adapter no longer consumes recorded event {event}')
    for flag in FLAGS['agy']:
        if f'"{flag}"' not in source:
            raise ValueError(f'agy option {flag} contract is stale')
    if '[]string{"acp"}' not in (COCKPIT / 'acp.go').read_text():
        raise ValueError('kimi ACP command changed; update handshake probe')
    if not re.fullmatch(r'\d+\.\d+\.\d+', VERSIONS['kimi']):
        raise ValueError('invalid kimi contract baseline')


def summary(message):
    print(message)
    path = os.environ.get('GITHUB_STEP_SUMMARY')
    if path:
        with open(path, 'a') as out:
            out.write(message + '\n\n')


def run(args, env, cwd):
    return subprocess.check_output(args, env=env, cwd=cwd, stderr=subprocess.STDOUT, timeout=25, text=True)


def initialize(command, env, cwd):
    process = subprocess.Popen([command, 'acp'], stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                               stderr=subprocess.DEVNULL, env=env, cwd=cwd, start_new_session=True)
    try:
        request = {'jsonrpc': '2.0', 'id': 1, 'method': 'initialize',
                   'params': {'protocolVersion': 1, 'clientCapabilities': {},
                              'clientInfo': {'name': 'pactify-drift', 'version': '1'}}}
        process.stdin.write((json.dumps(request) + '\n').encode())
        process.stdin.flush()
        with selectors.DefaultSelector() as selector:
            selector.register(process.stdout, selectors.EVENT_READ)
            deadline = time.monotonic() + 25
            while time.monotonic() < deadline:
                if not selector.select(max(0, deadline - time.monotonic())):
                    break
                line = process.stdout.readline()
                if not line:
                    break
                response = json.loads(line)
                if response.get('id') == 1:
                    check_initialize(response)
                    return
        raise ValueError('ACP initialize did not return a matching response within 25 seconds')
    finally:
        os.killpg(process.pid, signal.SIGTERM)
        try:
            process.wait(timeout=3)
        except subprocess.TimeoutExpired:
            os.killpg(process.pid, signal.SIGKILL)
            process.wait()
        process.stdin.close()
        process.stdout.close()


def live_check(harness):
    command = shutil.which(harness)
    if command is None:
        summary(f'SKIP {harness}: CLI unavailable on this runner; install step failed or no supported distribution. Live drift was not checked.')
        return
    # No real HOME, vendor credentials or repository integrations are inherited.
    with tempfile.TemporaryDirectory(prefix='pactify-drift-') as root:
        env = {k: v for k, v in os.environ.items()
               if k in ('PATH', 'LANG', 'LC_ALL', 'SYSTEMROOT', 'TMPDIR')}
        env.update(HOME=root, KIMI_CODE_HOME=root + '/kimi', PACTIFY_HOME=root + '/pactify',
                   XDG_CONFIG_HOME=root + '/config', XDG_DATA_HOME=root + '/data',
                   KIMI_CODE_NO_AUTO_UPDATE='1', CHOKIDAR_USEPOLLING='1')
        # Agy version/help must not select its default non-flash model.
        extra = ['--model', 'gemini-3.8-flash-low'] if harness == 'agy' else []
        version = run([command, '--version'] + extra, env, root).strip()
        match = re.search(r'\b\d+\.\d+\.\d+(?:[-+][\w.-]+)?\b', version)
        if not match:
            raise ValueError(f'unrecognized {harness} version: {version}')
        summary(version_message(harness, match.group()))
        check_help(harness, run([command, '--help'] + extra, env, root))
        if harness == 'kimi':
            if not re.search(r'\bacp\b', run([command, '--help'], env, root)):
                raise ValueError('kimi help no longer advertises acp')
            session_help = run([command, 'session', 'list', '--help'], env, root)
            for flag in ('--cwd', '--json'):
                if flag not in session_help:
                    raise ValueError(f'kimi session list lost {flag}')
            initialize(command, env, root)
            summary('PASS kimi: help and unauthenticated ACP initialize; no session or prompt created.')
        else:
            check_constants(Path(command).resolve().read_bytes())
            summary('PASS agy: help and stream-json constants from the AJ-711 contract fixture; no inference or new recording.')


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--static', action='store_true')
    parser.add_argument('--harness', choices=('kimi', 'agy'))
    args = parser.parse_args()
    static_check()
    if args.harness:
        live_check(args.harness)
    elif not args.static:
        parser.error('select --static or --harness')
