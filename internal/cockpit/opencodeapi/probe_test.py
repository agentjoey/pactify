import json
import unittest
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
        self.assertEqual(policy['write'], [dict(action=a, resource='*', effect=e) for a, e in [('edit', 'ask'), ('shell', 'ask'), ('question', 'deny')]])
