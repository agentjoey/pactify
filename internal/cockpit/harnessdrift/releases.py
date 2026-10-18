#!/usr/bin/env python3
"""Weekly release watch: compare each harness's release channels with the
pactify contract baseline. Reads public registry metadata only; never installs
or runs a vendor CLI. See docs/harness-updates.md."""
import json
import os
from pathlib import Path
import re
import sys
import urllib.request

HERE = Path(__file__).resolve().parent
COCKPIT = HERE.parent
AGY_MANIFEST = 'https://antigravity-cli-auto-updater-974169037036.us-central1.run.app/manifests/linux_amd64.json'
# Every channel an official installer can deliver, including successor
# packages: opencode 2.x ships as @opencode/cli while opencode-ai stays on 1.x.
CHANNELS = {
    'codex': [('npm', '@openai/codex', 'codex')],
    'kimi': [('npm', '@moonshot-ai/kimi-code', 'kimi')],
    'opencode': [('npm', 'opencode-ai', 'opencode'), ('npm', '@opencode/cli', 'opencode2')],
    'agy': [('manifest', AGY_MANIFEST, 'agy')],
}
# Major lines already triaged. A tracked line is reported, not alerted; remove
# the entry once the adapter supports that line and the baseline moves.
TRACKED = {}


def baselines():
    table = json.loads((HERE / 'versions.json').read_text())
    table['codex'] = (COCKPIT / 'codexschema' / 'CODEX_VERSION').read_text().strip()
    return table


def major(version):
    match = re.match(r'v?(\d+)\.\d+\.\d+', version or '')
    return int(match.group(1)) if match else None


def fetch_json(url, accept='application/json'):
    request = urllib.request.Request(url, headers={'Accept': accept, 'User-Agent': 'pactify-release-watch'})
    with urllib.request.urlopen(request, timeout=30) as response:
        return json.load(response)


def latest(kind, source, fetch=fetch_json):
    """Returns (version, deprecated message or '')."""
    if kind == 'manifest':
        return fetch(source)['version'], ''
    doc = fetch('https://registry.npmjs.org/' + source.replace('/', '%2F'))
    version = doc['dist-tags']['latest']
    return version, doc.get('versions', {}).get(version, {}).get('deprecated', '') or ''


def classify(harness, baseline, version, deprecated=''):
    """Returns (level, message); level is ok, update, tracked or alert."""
    if deprecated:
        return 'alert', f'latest {version} is deprecated: {deprecated}'
    have, want = major(baseline), major(version)
    if want is None:
        return 'alert', f'unrecognized version {version!r}'
    if have is not None and want < have:
        return 'ok', f'{version} (older line; baseline {baseline})'
    if want != have:
        note = TRACKED.get((harness, want))
        if note:
            return 'tracked', f'{version} — new major line, tracked: {note}'
        return 'alert', f'{version} — new major line (baseline {baseline}); follow docs/harness-updates.md'
    if version != baseline:
        return 'update', f'{version} (baseline {baseline}); weekly drift jobs cover this line'
    return 'ok', version


def watch(fetch=fetch_json, table=None):
    table = table or baselines()
    rows = []
    for harness, channels in CHANNELS.items():
        for kind, source, baseline_key in channels:
            name = source if kind == 'npm' else 'installer manifest'
            try:
                version, deprecated = latest(kind, source, fetch)
            except Exception as err:  # Registry outages must not page anyone.
                rows.append((harness, name, 'skip', f'unavailable: {type(err).__name__}: {err}'))
                continue
            rows.append((harness, name) + classify(harness, table[baseline_key], version, deprecated))
    return rows


def report(rows):
    lines = ['| harness | channel | level | detail |', '| --- | --- | --- | --- |']
    lines += [f'| {h} | `{c}` | {level} | {detail} |' for h, c, level, detail in rows]
    return '\n'.join(lines)


def main():
    rows = watch()
    text = '## Harness release watch\n\n' + report(rows)
    print(text)
    path = os.environ.get('GITHUB_STEP_SUMMARY')
    if path:
        with open(path, 'a') as out:
            out.write(text + '\n\n')
    alerts = [r for r in rows if r[2] == 'alert']
    if alerts:
        print(f'{len(alerts)} release alert(s); triage per docs/harness-updates.md', file=sys.stderr)
        return 1
    return 0


if __name__ == '__main__':
    sys.exit(main())
