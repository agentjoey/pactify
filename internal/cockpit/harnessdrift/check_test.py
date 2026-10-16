import unittest
import check

class ContractTests(unittest.TestCase):
    def test_missing_flag_fails(self):
        flags = check.FLAGS['agy']
        check.check_help('agy', ' '.join(flags))
        with self.assertRaises(ValueError):
            check.check_help('agy', ' '.join(flags).replace('--effort', '--efforz'))

    def test_handshake_is_typed(self):
        check.check_initialize({'id': 1, 'result': {'protocolVersion': 1, 'agentCapabilities': {}}})
        for response in [{'id': 1, 'result': {}}, {'id': 1, 'error': {'code': -1}}, {'id': 2, 'result': {'protocolVersion': 1, 'agentCapabilities': {}}}]:
            with self.assertRaises(ValueError):
                check.check_initialize(response)

    def test_version_drift_prompts_doctor(self):
        self.assertIn('run pactify sub doctor', check.version_message('kimi', '9.9.9'))
        self.assertNotIn('run pactify sub doctor', check.version_message('kimi', check.VERSIONS['kimi']))

    def test_renamed_event_is_detected(self):
        events = check.agy_events()
        check.check_constants((' '.join('"' + e + '"' for e in events)).encode())
        with self.assertRaises(ValueError):
            check.check_constants('"init" "step_updatz" "result"'.encode())

    def test_static_adapter_contract(self):
        check.static_check()

if __name__ == '__main__':
    unittest.main()
