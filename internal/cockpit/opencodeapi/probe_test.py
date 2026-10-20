import json
import unittest
import contextlib
import fnmatch
from pathlib import Path
from unittest.mock import patch
import permissions_probe
from probe import listening_url
from permissions_probe import policies


class ProbeTest(unittest.TestCase):
    def test_startup_formats(self):
        self.assertEqual(listening_url(b'opencode server listening on http://127.0.0.1:123\n', 1), 'http://127.0.0.1:123')
        self.assertEqual(listening_url(b'{"url":"http://127.0.0.1:456"}\n', 2), 'http://127.0.0.1:456')
        self.assertIsNone(listening_url(b'unrelated line\n', 1))

    def test_v2_rejects_untrusted_addresses_and_non_json(self):
        for line in [b'{"url":"https://example.com"}', b'{"url":"http://127.0.0.1:42/extra"}', b'opencode server listening on http://127.0.0.1:123']:
            with self.assertRaises((ValueError, json.JSONDecodeError)):
                listening_url(line, 2)

    def test_probe_uses_exported_policy(self):
        policy = policies()
        self.assertEqual(policy['readonly'], [dict(action=a, resource='*', effect=e) for a, e in [('*', 'deny'), ('read', 'allow'), ('glob', 'allow'), ('grep', 'allow'), ('shell', 'deny')]])
        self.assertEqual(policy['write'], [dict(action=a, resource='*', effect=e) for a, e in [('edit', 'ask'), ('shell', 'ask'), ('question', 'deny'), ('external_directory', 'deny')]])

    def test_write_probe_checks_external_directory_order(self):
        root = Path('/pactify-probe')
        rules = []
        checks = []
        access = ''
        def call(method, path, body=None):
            nonlocal rules, access
            if method == 'DELETE':
                return '{}'
            if path == '/api/session':
                rules = body['permissions']
                access = body['agent']
                return json.dumps({'data': {'id': 'ses_probe'}})
            action, resources = body['action'], body['resources']
            effect = 'ask'
            for rule in rules:
                if fnmatch.fnmatchcase(action, rule['action']) and all(fnmatch.fnmatchcase(r, rule['resource']) for r in resources):
                    effect = rule['effect']
            if access == 'build':
                checks.append((action, resources, effect))
            return json.dumps({'data': {'effect': effect}})
        @contextlib.contextmanager
        def server(binary, major):
            yield call, root
        with patch.object(permissions_probe, 'server', server):
            permissions_probe.run('synthetic')
        self.assertIn(('external_directory', ['/other/*'], 'deny'), checks)
        self.assertIn(('external_directory', [str(root) + '/*'], 'ask'), checks)
