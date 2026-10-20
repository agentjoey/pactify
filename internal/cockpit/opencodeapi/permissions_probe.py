#!/usr/bin/env python3
"""Non-billable v2 permission evaluation in a fresh HOME and four XDG roots."""
import argparse
import json
from pathlib import Path
import subprocess
from probe import server

def policies(*roots):
    root = Path(__file__).resolve().parents[3]
    return json.loads(subprocess.check_output(
        ["go", "run", "./internal/cockpit/opencodeapi/cmd/permissions", *map(str, roots)], cwd=root))


def run(binary):
    with server(binary, 2) as (call, root):
        rules_by_access = policies(root)
        for access, rules, cases in [
            ("readonly", rules_by_access["readonly"], [("shell", ["git status"], "deny"), ("shell", ["ls -la"], "deny"), ("shell", ["> output"], "deny"), ("shell", ["X=value > /dev/null"], "deny"), ("shell", ["rm -rf x"], "deny"), ("shell", ["echo x > f"], "deny"), ("shell", ["git status", "sh"], "deny"), ("shell", ["sh"], "deny"), ("edit", ["f"], "deny"), ("external_directory", ["/other/*"], "deny"), ("webfetch", ["https://example.com"], "deny")]),
            ("write", rules_by_access["write"], [("edit", ["f"], "ask"), ("question", ["*"], "deny"), ("external_directory", ["/other/*"], "deny"), ("external_directory", [str(root) + "/*"], "ask")])]:
            result = json.loads(call("POST", "/api/session", dict(title="Pactify permission probe", agent="plan" if access == "readonly" else "build", location=dict(directory=str(root)), permissions=rules)))
            sid = result["data"]["id"]
            try:
                for action, resources, expected in cases:
                    effect = json.loads(call("POST", f"/api/session/{sid}/permission", dict(action=action, resources=resources)))["data"]["effect"]
                    print(f"{access}: {action} {resources}: {effect} (expected {expected})")
                    if effect != expected:
                        raise AssertionError("permission mismatch")
            finally:
                call("DELETE", f"/api/session/{sid}")


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--binary", default="opencode")
    run(parser.parse_args().binary)
