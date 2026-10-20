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
V2_OPERATIONS = {
    "/api/session": ("post", "get"),
    "/api/session/{sessionID}": ("get", "patch"),
    "/api/session/active": ("get",),
    "/api/session/{sessionID}/agent": ("post",),
    "/api/session/{sessionID}/model": ("post",),
    "/api/session/{sessionID}/prompt": ("post",),
    "/api/session/{sessionID}/interrupt": ("post",),
    "/api/session/{sessionID}/message": ("get",),
    "/api/session/{sessionID}/permission": ("get", "post"),
    "/api/session/{sessionID}/permission/{requestID}/reply": ("post",),
    "/api/session/{sessionID}/form/{formID}": ("delete",),
    "/api/experimental/session/{sessionID}/instructions/entries/{key}": ("put",),
    "/api/experimental/session/{sessionID}/wait": ("post",),
    "/api/event": ("get",),
    "/api/model": ("get",),
    "/api/model/default": ("get",),
    "/api/integration": ("get",),
}
V2_FIELDS = {
    "Session.Info": {"id", "parentID", "agent", "model", "tokens", "cost", "permissions", "location"},
    "Model.Info": {"id", "providerID", "enabled", "variants"},
    "Model.Variant": {"id"},
    "Model.Ref": {"id", "providerID", "variant"},
    "Permission.Rule": {"action", "resource", "effect"},
    "Permission.Request": {"id", "sessionID", "action", "resources"},
    "Session.StructuredError": {"type", "message", "status", "response"},
    "Session.Message.Assistant": {"id", "type", "time", "content", "error"},
    "Session.Message.User": {"id", "type"},
    "Session.Message.Idle": {"id", "type", "outcome", "error"},
    "Session.Message.Assistant.Text": {"type", "text"},
    "Integration.Info": {"id", "connections"},
    "Connection.CredentialInfo": {"id", "type"},
    "Connection.EnvInfo": {"name", "type"},
}
# V2's raw SSE event data is only advertised as an encoded string in OpenAPI.
# Event-field drift is covered by fake-server and host golden replay tests.

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


def project(doc, version=1):
    operations, fields_map, unions = (V2_OPERATIONS, V2_FIELDS, {}) if version == 2 else (OPERATIONS, FIELDS, UNIONS)
    paths = {path: {method: clean(doc["paths"][path][method]) for method in methods}
             for path, methods in operations.items()}
    schemas = {}
    pending = set(refs(paths)) | set(fields_map) | set(unions)
    while pending:
        name = pending.pop()
        if name in schemas:
            continue
        schema = clean(doc["components"]["schemas"][name])
        if name in fields_map:
            fields = fields_map[name]
            schema["properties"] = {k: v for k, v in schema["properties"].items() if k in fields}
            if "required" in schema:
                schema["required"] = [k for k in schema["required"] if k in fields]
        if name in unions:
            schema["anyOf"] = [v for v in schema["anyOf"]
                               if v.get("$ref", "").rsplit("/", 1)[-1] in unions[name]]
        schemas[name] = schema
        pending.update(set(refs(schema)) - set(schemas))
    return {"openapi": doc["openapi"], "paths": paths, "components": {"schemas": schemas}}


def canonical(doc):
    return json.dumps(doc, indent=2, sort_keys=True) + "\n"


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("schema", type=Path)
    parser.add_argument("--v2", action="store_true", help="Check the v2 backend projection")
    parser.add_argument("--write", action="store_true", help="Update the reviewed snapshot")
    args = parser.parse_args()
    snapshot = Path(__file__).with_name("openapi-v2.json" if args.v2 else "openapi.json")
    actual = canonical(project(json.loads(args.schema.read_text()), 2 if args.v2 else 1))
    if args.write:
        snapshot.write_text(actual)
        return
    expected = snapshot.read_text()
    if actual != expected:
        print("".join(difflib.unified_diff(expected.splitlines(True), actual.splitlines(True),
                                          fromfile="reviewed v2 contract (2.1.0)" if args.v2 else "reviewed v1 contract (1.19.0)", tofile=str(args.schema))), end="")
        raise SystemExit(1)
    print("PASS: used opencode " + ("v2" if args.v2 else "v1") + " paths and schemas match the reviewed snapshot")


if __name__ == "__main__":
    main()
