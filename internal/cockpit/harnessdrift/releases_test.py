import unittest
import releases


def registry(versions):
    def fetch(url, accept=None):
        for key, value in versions.items():
            if url.endswith(key.replace('/', '%2F')):
                if isinstance(value, Exception):
                    raise value
                if url.startswith('https://registry.npmjs.org/'):
                    latest, deprecated = value if isinstance(value, tuple) else (value, '')
                    meta = {'deprecated': deprecated} if deprecated else {}
                    return {'dist-tags': {'latest': latest}, 'versions': {latest: meta}}
                return {'version': value}
        raise AssertionError('unexpected url ' + url)
    return fetch


BASE = {'codex': '0.162.1', 'kimi': '2.2.0', 'opencode': '1.19.0', 'opencode2': '2.1.0', 'agy': '1.4.0'}
CURRENT = {'@openai/codex': '0.163.0', '@moonshot-ai/kimi-code': '2.2.0', 'opencode-ai': '1.19.3',
           '@opencode/cli': '2.1.0', 'linux_amd64.json': '1.4.0'}


class ReleaseWatchTests(unittest.TestCase):
    def levels(self, versions):
        return {(h, c): level for h, c, level, _ in releases.watch(registry(versions), dict(BASE))}

    def test_current_channels_do_not_alert(self):
        levels = self.levels(CURRENT)
        self.assertNotIn('alert', levels.values())
        self.assertEqual(levels[('opencode', '@opencode/cli')], 'ok')
        self.assertEqual(levels[('codex', '@openai/codex')], 'update')
        self.assertEqual(levels[('kimi', '@moonshot-ai/kimi-code')], 'ok')

    def test_untracked_major_alerts(self):
        levels = self.levels(dict(CURRENT, **{'@moonshot-ai/kimi-code': '3.0.0', '@opencode/cli': '3.0.0'}))
        self.assertEqual(levels[('kimi', '@moonshot-ai/kimi-code')], 'alert')
        self.assertEqual(levels[('opencode', '@opencode/cli')], 'alert')

    def test_codex_zero_minor_is_routine(self):
        self.assertEqual(releases.classify('codex', '0.162.1', '0.170.0')[0], 'update')
        self.assertEqual(releases.classify('codex', '0.162.1', '1.0.0')[0], 'alert')

    def test_deprecated_latest_alerts(self):
        levels = self.levels(dict(CURRENT, **{'opencode-ai': ('1.19.4', 'moved to @opencode/cli')}))
        self.assertEqual(levels[('opencode', 'opencode-ai')], 'alert')

    def test_registry_outage_is_skipped_not_alerted(self):
        levels = self.levels(dict(CURRENT, **{'@openai/codex': OSError('offline'), 'linux_amd64.json': TimeoutError()}))
        self.assertEqual(levels[('codex', '@openai/codex')], 'skip')
        self.assertEqual(levels[('agy', 'installer manifest')], 'skip')
        self.assertNotIn('alert', levels.values())

    def test_unparseable_version_alerts(self):
        self.assertEqual(releases.classify('agy', '1.4.0', 'nightly')[0], 'alert')

    def test_repo_baselines_cover_every_channel(self):
        table = releases.baselines()
        self.assertEqual(table['opencode2'], '2.1.0')
        for channels in releases.CHANNELS.values():
            for _, _, key in channels:
                self.assertIsNotNone(releases.major(table[key]), key)


if __name__ == '__main__':
    unittest.main()
