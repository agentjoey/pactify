#!/usr/bin/env python3
"""Compare only the v1 operations and schema fields consumed by Pactify."""
import argparse
import difflib
import json
from pathlib import Path

OPERATIONS = {
    "/session": ("post",),
    "/session/{sessionID}": ("get", "patch"),
    "/session/status": ("get",),
    "/session/{sessionID}/children": ("get",),
    "/session/{sessionID}/message": ("get", "post"),
    "/session/{sessionID}/prompt_async": ("post",),
    "/session/{sessionID}/abort": ("post",),
    "/permission": ("get",),
    "/permission/{requestID}/reply": ("post",),
    "/event": ("get",),
    "/config/providers": ("get",),
}
FIELDS = {
    "Session": {"id", "parentID", "permission", "tokens", "cost"},
    "AssistantMessage": {"id", "sessionID", "role", "providerID", "modelID", "error", "finish"},
    "Provider": {"id", "models"},
    "Model": {"id", "name", "variants"},
    "TextPart": {"id", "sessionID", "messageID", "type", "text"},
    "ToolPart": {"id", "sessionID", "messageID", "type", "tool", "state"},
    "APIError": {"name", "data"},
}
UNIONS = {
    "Event": {"EventSessionCreated", "EventSessionUpdated", "EventSessionError", "EventSessionIdle", "EventSessionStatus", "EventMessageUpdated", "EventMessagePartUpdated", "EventMessagePartDelta", "EventPermissionAsked"},
    "Part": {"TextPart", "ToolPart"},
}
IGNORED = {"description", "summary", "title", "examples", "example", "default", "tags"}


def clean(value):
    if isinstance(value, dict):
        return {k: clean(v) for k, v in value.items() if k not in IGNORED}
    if isinstance(value, list):
        return [clean(v) for v in value]
    return value


def refs(value):
    if isinstance(value, dict):
        if "$ref" in value:
            yield value["$ref"].rsplit("/", 1)[-1]
        for v in value.values():
            yield from refs(v)
    elif isinstance(value, list):
        for v in value:
            yield from refs(v)


def project(doc):
    paths = {path: {method: clean(doc["paths"][path][method]) for method in methods}
             for path, methods in OPERATIONS.items()}
    schemas = {}
    pending = set(refs(paths)) | set(FIELDS) | set(UNIONS)
    while pending:
        name = pending.pop()
        if name in schemas:
            continue
        schema = clean(doc["components"]["schemas"][name])
        if name in FIELDS:
            fields = FIELDS[name]
            schema["properties"] = {k: v for k, v in schema["properties"].items() if k in fields}
            if "required" in schema:
                schema["required"] = [k for k in schema["required"] if k in fields]
        if name in UNIONS:
            schema["anyOf"] = [v for v in schema["anyOf"]
                               if v.get("$ref", "").rsplit("/", 1)[-1] in UNIONS[name]]
        schemas[name] = schema
        pending.update(set(refs(schema)) - set(schemas))
    return {"openapi": doc["openapi"], "paths": paths, "components": {"schemas": schemas}}


def canonical(doc):
    return json.dumps(doc, indent=2, sort_keys=True) + "\n"


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("schema", type=Path)
    parser.add_argument("--write", action="store_true", help="Update the reviewed snapshot")
    args = parser.parse_args()
    snapshot = Path(__file__).with_name("openapi.json")
    actual = canonical(project(json.loads(args.schema.read_text())))
    if args.write:
        snapshot.write_text(actual)
        return
    expected = snapshot.read_text()
    if actual != expected:
        print("".join(difflib.unified_diff(expected.splitlines(True), actual.splitlines(True),
                                          fromfile="reviewed v1 contract (1.19.0)", tofile=str(args.schema))), end="")
        raise SystemExit(1)
    print("PASS: used opencode v1 paths and schemas match the reviewed snapshot")


if __name__ == "__main__":
    main()
