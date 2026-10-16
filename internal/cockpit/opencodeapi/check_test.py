import copy
import json
import unittest
from pathlib import Path
from check import project


class ContractTest(unittest.TestCase):
    def setUp(self):
        self.snapshot = json.loads(Path(__file__).with_name("openapi.json").read_text())

    def test_roundtrip(self):
        self.assertEqual(project(self.snapshot), self.snapshot)

    def test_used_field_drift(self):
        altered = copy.deepcopy(self.snapshot)
        altered["components"]["schemas"]["Session"]["properties"]["tokens"]["properties"]["cache"]["properties"]["write"]["type"] = "string"
        self.assertNotEqual(project(altered), self.snapshot)

    def test_v2_and_unused_fields_do_not_cause_drift(self):
        altered = copy.deepcopy(self.snapshot)
        altered["paths"]["/api/session"] = {"post": {"description": "unused"}}
        altered["components"]["schemas"]["Session"]["properties"]["unused"] = {"type": "boolean"}
        self.assertEqual(project(altered), self.snapshot)

    def test_missing_v1_endpoint_fails(self):
        del self.snapshot["paths"]["/permission/{requestID}/reply"]
        with self.assertRaises(KeyError):
            project(self.snapshot)


if __name__ == "__main__":
    unittest.main()

class WorkflowTest(unittest.TestCase):
    def test_latest_excluded_from_pull_requests(self):
        workflow = Path(__file__).parents[3] / '.github/workflows/opencode-api.yml'
        content = workflow.read_text()
        self.assertIn("github.event_name == 'pull_request'", content)
        self.assertIn("'[\"1.19\"]'", content)
