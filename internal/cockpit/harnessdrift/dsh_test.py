import unittest
import releases


class DSHReleaseTests(unittest.TestCase):
    def test_prerelease_tag(self):
        def fetch(url):
            self.assertTrue(url.endswith('@deepseek-ai%2Fdsh'))
            return {'dist-tags': {'latest': '0.2.1-rc.5'}}
        self.assertEqual(releases.latest('npm', '@deepseek-ai/dsh', fetch), ('0.2.1-rc.5', ''))

    def test_unsupported_zero_patch_tracked(self):
        self.assertEqual(releases.classify('dsh', '0.2.1-rc.5', '0.2.2')[0], 'tracked')

    def test_supported_prerelease(self):
        self.assertEqual(releases.classify('dsh', '0.2.1-rc.5', '0.2.1-rc.6')[0], 'update')
