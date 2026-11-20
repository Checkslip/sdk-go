#!/usr/bin/env python3
"""Compare what an SDK's models demand against what the API document promises.

Two customer-facing bugs on 20 Nov 2026 had the same shape: a hand-written model
required a field the published API reference does not promise, so a call that
worked in three clients failed in one.

- `Balance.threshold_basis` left the customer API on 9 Nov. Rust declared it as a
  plain value and PHP read it with a helper that throws on a missing key, so
  reading a credit balance failed for eleven days in both.
- `ExportManifest.hash_algorithm` and `canonicalization` were never in any
  contract, so every READY evidence export failed to decode in Rust.

Neither was caught by a test, because the fixtures were written by hand from the
shape we believed the API sends: the tests confirmed our belief rather than the
API (checkslip/proofreader h-mwp3s28b226).

What this reads, per language:

  go      a struct field is demanded never — a missing JSON key is the zero
          value — so Go is reported on, not blocked: a field the document does
          not have shows up as an empty value instead of an error.
  php     a key read through a non-OrNull Arr helper is demanded.
  rust    a field that is neither Option<_> nor #[serde(default)] is demanded.
  js      generated from the document, so nothing can drift; skipped.

Exit code 1 when a model demands something the document does not promise.
`--prove` is the control: it removes a field the document requires, runs the
check against that altered document, and fails if the check stayed silent. A
checker that cannot go red is not a checker.

Usage:
  check-models.py --repo <path>                      read spec/openapi.json, or --document
  check-models.py --repo <path> --url <openapi url>  read the document the site serves
  check-models.py --repo <path> --prove              run the control instead
"""

from __future__ import annotations

import argparse
import json
import re
import sys
import time
import urllib.request
from pathlib import Path

DEFAULT_URL = "https://checkslip.com/openapi.json"

# A model whose name is not the schema's name. Kept small on purpose: a new
# entry here is a decision that two names mean the same thing.
ALIASES = {
    "ApiRequest": "ApiRequestRecord",
    "AuditEvent": "AuditEventRecord",
    "SlipCheckRecordBase": "SlipCheckRecord",
}

# Fields that left the customer API and are kept in a model on purpose, with
# the version that retired them. They are read as "always absent", so they are
# neither demanded nor missing.
RETIRED = {"threshold_basis"}


def load_document(path: str | None, url: str | None) -> dict:
    if path:
        return json.loads(Path(path).read_text())
    # The site answers 403 to urllib's default User-Agent, which turned this
    # whole job into a green light that measured nothing the first time it ran
    # in CI. A plain tool name is enough to be served.
    request = urllib.request.Request(url or DEFAULT_URL, headers={"User-Agent": "checkslip-sdk-model-check"})
    # One reset connection should not fail a release, and this check going red
    # does block one: a flaky fetch took go 0.9.1 off the module proxy once.
    last: OSError | None = None
    for attempt in range(3):
        try:
            with urllib.request.urlopen(request, timeout=30) as answer:
                return json.loads(answer.read().decode())
        except OSError as error:
            last = error
            if attempt < 2:
                time.sleep(2 * (attempt + 1))
    raise last if last else OSError("the document could not be read")


def schema_fields(schemas: dict, node: object, seen: tuple[str, ...] = ()) -> tuple[dict, set]:
    """Every property and every required name of a schema, with $ref and allOf resolved."""
    properties: dict = {}
    required: set = set()
    if not isinstance(node, dict):
        return properties, required
    if "$ref" in node:
        name = str(node["$ref"]).split("/")[-1]
        if name in seen:
            return properties, required
        return schema_fields(schemas, schemas.get(name, {}), seen + (name,))
    for branch in node.get("allOf", []):
        inner_properties, inner_required = schema_fields(schemas, branch, seen)
        properties.update(inner_properties)
        required |= inner_required
    for branch in node.get("oneOf", []) + node.get("anyOf", []):
        # A union of shapes: every member's properties can arrive, but none of
        # them is promised, so nothing is added to required.
        inner_properties, _ = schema_fields(schemas, branch, seen)
        properties.update(inner_properties)
    properties.update(node.get("properties") or {})
    required |= set(node.get("required") or [])
    return properties, required


def php_models(repo: Path) -> dict[str, tuple[set, set]]:
    """{model: (demanded, known)} from src/Model/*.php."""
    out = {}
    # objects() and strings() read a missing key as an empty list, so they
    # demand nothing; obj(), str(), int(), bool() and date() throw.
    demanding = r"Arr::(?:str|int|bool|date|obj)\(\s*\$d,\s*'([a-z0-9_]+)'"
    reading = r"Arr::\w+\(\s*\$d,\s*'([a-z0-9_]+)'"
    for file in sorted((repo / "src" / "Model").glob("*.php")):
        text = file.read_text()
        demanded = set(re.findall(demanding, text))
        known = set(re.findall(reading, text))
        if known:
            out[file.stem] = (demanded, known)
    return out


def rust_models(repo: Path) -> dict[str, tuple[set, set]]:
    """{model: (demanded, known)} from src/models/*.rs."""
    out = {}
    for file in sorted((repo / "src" / "models").glob("*.rs")):
        text = file.read_text()
        for struct in re.finditer(r"pub struct (\w+) \{(.*?)\n\}", text, re.S):
            name, body = struct.group(1), struct.group(2)
            demanded, known = set(), set()
            for field in re.finditer(
                r"((?:[ \t]*#\[[^\]]*\]\n|[ \t]*///[^\n]*\n)*)[ \t]*pub (\w+):\s*([^,\n]+),",
                body,
            ):
                attributes, field_name, field_type = field.group(1), field.group(2), field.group(3)
                if "flatten" in attributes:
                    continue
                renamed = re.search(r'rename\s*=\s*"([^"]+)"', attributes)
                wire = renamed.group(1) if renamed else field_name
                known.add(wire)
                if "Option<" not in field_type and "default" not in attributes:
                    demanded.add(wire)
            if known:
                out[name] = (demanded, known)
    return out


def go_models(repo: Path) -> dict[str, tuple[set, set]]:
    """{model: (demanded, known)} from the struct tags. Go demands nothing."""
    out = {}
    source = "\n".join(
        file.read_text() for file in sorted(repo.glob("*.go")) if not file.name.endswith("_test.go")
    )
    for struct in re.finditer(r"type (\w+) struct \{(.*?)\n\}", source, re.S):
        name, body = struct.group(1), struct.group(2)
        known = set(re.findall(r'json:"([a-z0-9_]+)', body))
        if known:
            out[name] = (set(), known)
    return out


def read_models(repo: Path) -> tuple[str, dict[str, tuple[set, set]]]:
    if (repo / "src" / "Model").is_dir():
        return "php", php_models(repo)
    if (repo / "src" / "models").is_dir():
        return "rust", rust_models(repo)
    if (repo / "go.mod").is_file():
        return "go", go_models(repo)
    return "js", {}


def check(document: dict, repo: Path) -> tuple[list[str], list[str], int]:
    """(blocking, reported, models compared)"""
    schemas = document.get("components", {}).get("schemas", {})
    language, models = read_models(repo)
    blocking: list[str] = []
    reported: list[str] = []
    compared = 0
    for model, (demanded, known) in sorted(models.items()):
        schema_name = ALIASES.get(model, model)
        if schema_name not in schemas:
            continue
        properties, required = schema_fields(schemas, {"$ref": f"#/components/schemas/{schema_name}"})
        if not properties:
            continue
        compared += 1
        # A schema built with allOf is a base plus an extension, and each
        # language composes that differently: the base fields may live in
        # another model, or be embedded. Only the half this file can see is
        # compared, so the "promised but missing" report is left out for those.
        composed = isinstance(schemas.get(schema_name), dict) and "allOf" in schemas[schema_name]
        for field in sorted(demanded - RETIRED):
            if field not in properties:
                blocking.append(f"{language} {model}.{field} is demanded and the document has no such field")
            elif field not in required:
                blocking.append(f"{language} {model}.{field} is demanded and the document does not promise it")
        if composed:
            continue
        for field in sorted(required - known - RETIRED):
            reported.append(f"{language} {model}.{field} is promised by the document and the model has no such field")
    return blocking, reported, compared


def prove(document: dict, repo: Path) -> int:
    """Remove a promise the models rely on, and fail if the check stays silent."""
    language, models = read_models(repo)
    if language == "go":
        print("⏭  go demands no field, so there is nothing to prove here")
        return 0
    if language == "js":
        print("⏭  the JavaScript types are generated, so there is nothing to prove here")
        return 0
    schemas = document.get("components", {}).get("schemas", {})
    for model, (demanded, _) in sorted(models.items()):
        schema_name = ALIASES.get(model, model)
        schema = schemas.get(schema_name)
        if not isinstance(schema, dict) or not isinstance(schema.get("required"), list):
            continue
        for field in sorted(demanded):
            if field in schema["required"]:
                altered = json.loads(json.dumps(document))
                altered_schema = altered["components"]["schemas"][schema_name]
                altered_schema["required"] = [r for r in altered_schema["required"] if r != field]
                altered_schema["properties"].pop(field, None)
                blocking, _, _ = check(altered, repo)
                wanted = f"{language} {model}.{field}"
                if any(line.startswith(wanted) for line in blocking):
                    print(f"✅ control: dropping {schema_name}.{field} from the document turns the check red")
                    return 0
                print(f"🔴 control: dropping {schema_name}.{field} left the check green, so it is not looking")
                return 1
    print("🔴 control: no demanded field is promised by the document, so nothing could be dropped")
    return 1


# A fixture file's name against the schema its body is. A fixture with no entry
# here is reported as uncovered rather than silently skipped.
FIXTURE_SCHEMAS = {
    "api-request": "ApiRequestRecord",
    "audit-event": "AuditEventRecord",
    "balance": "Balance",
    "delivery": "Delivery",
    "delivery-detail": "DeliveryDetail",
    "endpoint": "Endpoint",
    "endpoint-with-secret": "EndpointWithSecret",
    "evidence-export": "EvidenceExport",
    "lookup-duplicate": "SlipCheck",
    "lookup-found": "SlipCheck",
    "lookup-found-bill-payment": "SlipCheck",
    "lookup_duplicate": "SlipCheck",
    "lookup_found": "SlipCheck",
    "lookup_found_bill_payment": "SlipCheck",
    "order": "Order",
    "packages": "PackageList",
    "receipt": "Receipt",
    "slip-check-record": "SlipCheckRecord",
    "slip-check-record-invalid": "SlipCheckRecord",
    "slip-counts": "SlipCounts",
    "slip-export": "SlipExport",
    "usage": "Usage",
    # "<schema>@<member>" checks that member of the fixture instead of the whole
    # file: a webhook fixture is an envelope (type, timestamp, data) that the
    # public document does not describe, while its data is a documented schema.
    "webhook-slip-checked": "SlipCheckedData@data",
}

# Fixtures that carry a withdrawn field on purpose, to prove the SDK drops it.
KEEPS_WITHDRAWN = {"api-request", "audit-event", "slip-check-record"}
WITHDRAWN = {
    "upstream_ms",
    "recorded_by",
    "chain",
    "trace_id",
    "checked_elsewhere",
    "latency_ms",
    "build",
    "source_domain",
    "redactions",
    "causation_id",
}


def fixture_files(repo: Path) -> list[Path]:
    return sorted(
        list((repo / "tests" / "Fixtures").glob("*.json"))
        + list((repo / "tests" / "fixtures").glob("*.json"))
    )


def lookup_examples(document: dict) -> dict:
    """The lookup's own examples from the document, by name."""
    lookup = (document.get("paths") or {}).get("/api/slips/lookup") or {}
    content = (
        ((lookup.get("post") or {}).get("responses") or {}).get("200", {}).get("content") or {}
    ).get("application/json") or {}
    return {
        name: body.get("value")
        for name, body in (content.get("examples") or {}).items()
        if isinstance(body, dict)
    }


def check_fixtures(document: dict, repo: Path) -> int:
    """The fixtures against the document: shape, and the lookup against its own examples."""
    schemas = document.get("components", {}).get("schemas", {})
    files = fixture_files(repo)
    if not files:
        print("⏭  this SDK keeps no fixture files; its test bodies are written inline")
        return 0

    examples = lookup_examples(document)
    blocking: list[str] = []
    reported: list[str] = []
    checked = 0
    for file in files:
        body = json.loads(file.read_text())
        if not isinstance(body, dict):
            continue
        schema_name = FIXTURE_SCHEMAS.get(file.stem)
        if not schema_name:
            reported.append(f"{file.name} is not mapped to a schema, so its shape is not checked")
            continue
        if "@" in schema_name:
            schema_name, member = schema_name.split("@", 1)
            inner = body.get(member)
            if not isinstance(inner, dict):
                reported.append(f"{file.name} has no {member} object to check")
                continue
            body = inner
        properties, required = schema_fields(schemas, {"$ref": f"#/components/schemas/{schema_name}"})
        if not properties:
            reported.append(f"{file.name}: the document has no schema {schema_name}")
            continue
        checked += 1
        allowed = set(properties) | (WITHDRAWN if file.stem in KEEPS_WITHDRAWN else set())
        for key in sorted(set(body) - allowed):
            blocking.append(f"{file.name} carries {key}, which {schema_name} does not have")
        for key in sorted(required - set(body) - RETIRED):
            reported.append(f"{file.name} has no {key}, which {schema_name} always sends")

        # The lookup is the one route whose answers the document shows, so our
        # fixtures can be held against them.
        name = file.stem.replace("-", "_").removeprefix("lookup_")
        if file.stem.startswith(("lookup-", "lookup_")) and name in examples:
            example = examples[name] or {}
            for key in sorted(set(example) & set(body)):
                if body[key] != example[key]:
                    reported.append(
                        f"{file.name} differs from the document's own {name!r} example at {key}"
                    )

    for line in reported:
        print(f"⚠️  {line}")
    for line in blocking:
        print(f"🔴 {line}")
    print(
        f"fixtures checked: {checked} of {len(files)} · shapes the document does not have:"
        f" {len(blocking)} · notes: {len(reported)}"
    )
    if blocking:
        print("A fixture that carries a field the API cannot send tests a shape nobody will receive.")
        return 1
    print("✅ every fixture is a shape the document describes")
    return 0


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--repo", default=".", help="the SDK to read (default: the working directory)")
    parser.add_argument("--document", help="a published openapi.json on disk")
    parser.add_argument("--url", help=f"fetch the document instead (default {DEFAULT_URL})")
    parser.add_argument("--prove", action="store_true", help="run the control and exit")
    parser.add_argument("--fixtures", action="store_true", help="check the fixture files instead")
    parser.add_argument(
        "--allow-unreachable",
        action="store_true",
        help="pass instead of failing when the document cannot be read (for an offline run)",
    )
    arguments = parser.parse_args()

    repo = Path(arguments.repo).resolve()
    if not arguments.document and not arguments.url and (repo / "spec" / "openapi.json").is_file():
        arguments.document = str(repo / "spec" / "openapi.json")
    try:
        document = load_document(arguments.document, arguments.url)
    except OSError as error:
        if arguments.allow_unreachable:
            print(f"⏭  the API document could not be read ({error}); nothing was checked")
            return 0
        print(f"🔴 the API document could not be read ({error}), so nothing was checked")
        print("A check that cannot reach the document has to go red, or it is a green light for nothing.")
        return 1

    if arguments.prove:
        return prove(document, repo)

    if arguments.fixtures:
        return check_fixtures(document, repo)

    language, models = read_models(repo)
    if language == "js":
        print("⏭  the JavaScript types are generated from this document, so they cannot drift")
        return 0

    blocking, reported, compared = check(document, repo)
    for line in reported:
        print(f"⚠️  {line}")
    for line in blocking:
        print(f"🔴 {line}")
    print(
        f"models compared: {compared} of {len(models)} · demanded but not promised: {len(blocking)}"
        f" · promised but missing: {len(reported)}"
    )
    if blocking:
        print("A field a model demands has to be promised by the document, or a real answer fails to parse.")
        return 1
    print("✅ every field these models demand is one the document promises")
    return 0


if __name__ == "__main__":
    sys.exit(main())
