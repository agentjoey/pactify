import unittest
import check
import releases

BUILD = '2026.11.17-f532c45'


class CursorContractTests(unittest.TestCase):
    def test_handshake_needs_cursor_login(self):
        ok = {'id': 1, 'result': {'protocolVersion': 1, 'agentCapabilities': {}, 'authMethods': [{'id': 'cursor_login'}]}}
        check.check_cursor_handshake(ok)
        ok['result']['authMethods'] = [{'id': 'other'}]
        with self.assertRaises(ValueError):
            check.check_cursor_handshake(ok)

    def test_baseline_is_a_dated_build(self):
        self.assertRegex(check.VERSIONS['cursor'], r'^\d{4}\.\d{1,2}\.\d{1,2}-[0-9a-f]{6,}$')


class CursorReleaseTests(unittest.TestCase):
    def test_latest_build_comes_from_the_install_script(self):
        script = f'DOWNLOAD_URL="https://downloads.cursor.com/lab/{BUILD}/${{OS}}/${{ARCH}}/agent-cli-package.tar.gz"'
        self.assertEqual(releases.latest('installer', releases.CURSOR_INSTALLER, lambda url: {'text': script}), (BUILD, ''))

    def test_script_without_a_build_is_an_error(self):
        with self.assertRaises(ValueError):
            releases.latest('installer', releases.CURSOR_INSTALLER, lambda url: {'text': 'echo hi'})

    def test_dated_builds_never_look_like_a_new_major_line(self):
        self.assertEqual(releases.classify('cursor', BUILD, BUILD)[0], 'ok')
        self.assertEqual(releases.classify('cursor', BUILD, '2027.01.05-abcdef1')[0], 'update')
        self.assertEqual(releases.classify('cursor', BUILD, '2026.10.08-abcdef1')[0], 'ok')
        self.assertEqual(releases.classify('cursor', BUILD, 'weird')[0], 'alert')
        self.assertEqual(releases.classify('cursor', BUILD, BUILD, 'broken')[0], 'alert')
