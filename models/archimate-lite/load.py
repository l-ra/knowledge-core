#!/usr/bin/env python3
"""Load ArchiMate Lite metamodel into a running knowledge-core via HTTP API.

Does not modify knowledge-core source. Idempotent: skips existing package
objects matched by iriLocal, existing shapes matched by code, and upserts
policy statements (class annotations, usage guidance, allowed pairs, enums, exchange-spec).

Auth (first match):
  KC_TOKEN          Bearer token (bootstrap password or OIDC access token)
  KC_ADMIN_PASSWORD same, sent as Bearer and X-Admin-Password
  KC_SUBJECT + KC_ROLES  dev mode (default subject=admin, roles=admin)

Usage:
  export KC_BASE_URL=http://localhost:8080
  export KC_TOKEN=...
  python3 models/archimate-lite/load.py
"""

from __future__ import annotations

import json
import os
import sys
import urllib.error
import urllib.parse
import urllib.request
from pathlib import Path

CATALOG = Path(__file__).resolve().parent / "catalog.json"


def headers() -> dict[str, str]:
    h = {
        "Accept": "application/json",
        "Content-Type": "application/json",
        "X-Validation-Mode": "relaxed",
    }
    token = os.environ.get("KC_TOKEN") or os.environ.get("KC_ADMIN_PASSWORD")
    if token:
        h["Authorization"] = f"Bearer {token}"
        h["X-Admin-Password"] = token
    else:
        h["X-Subject"] = os.environ.get("KC_SUBJECT", "admin")
        h["X-Roles"] = os.environ.get("KC_ROLES", "admin")
    return h


def unwrap(body: dict) -> dict:
    if isinstance(body.get("data"), dict):
        return body["data"]
    return body


def id_path(public_id: str) -> str:
    """Encode a public id (often a full IRI) for use in a single URL path segment."""
    return urllib.parse.quote(public_id, safe="")


class KC:
    def __init__(self, base: str):
        self.base = base.rstrip("/")
        self.h = headers()

    def req(self, method: str, path: str, payload=None, query=None):
        url = self.base + path
        if query:
            url += "?" + urllib.parse.urlencode({k: v for k, v in query.items() if v is not None})
        data = None if payload is None else json.dumps(payload).encode()
        r = urllib.request.Request(url, data=data, headers=self.h, method=method)
        try:
            with urllib.request.urlopen(r) as resp:
                raw = resp.read()
                return resp.status, json.loads(raw) if raw else {}
        except urllib.error.HTTPError as e:
            raw = e.read()
            try:
                body = json.loads(raw) if raw else {}
            except json.JSONDecodeError:
                body = {"error": raw.decode("utf-8", "replace")}
            if e.code >= 400 and e.code != 404:
                raise SystemExit(f"{method} {path} -> {e.code}: {body}")
            return e.code, body

    def get(self, path: str, **query):
        return self.req("GET", path, query=query or None)

    def post(self, path: str, payload: dict):
        return self.req("POST", path, payload)

    def put(self, path: str, payload: dict):
        return self.req("PUT", path, payload)

    def patch(self, path: str, payload: dict):
        return self.req("PATCH", path, payload)


def list_package_entities(kc: KC, code: str, kind: str = "") -> list[dict]:
    items: list[dict] = []
    cursor = ""
    while True:
        q = {"package": code, "limit": "200"}
        if kind:
            q["kind"] = kind
        if cursor:
            q["cursor"] = cursor
        status, body = kc.get("/v1/entities", **q)
        if status == 404:
            return []
        if status != 200:
            raise SystemExit(f"list entities: {status} {body}")
        batch = body.get("items") or []
        items.extend(batch)
        cursor = body.get("nextCursor") or ""
        if not cursor:
            break
    return items


def index_by_iri(entities: list[dict]) -> dict[str, str]:
    out = {}
    for e in entities:
        local = e.get("iriLocal") or ""
        pid = e.get("id") or ""
        if local and pid:
            out[local] = pid
    return out


def constraints_payload(prop: dict, class_ids: dict[str, str]) -> dict:
    c: dict = {}
    domain = []
    for name in prop.get("domain") or []:
        cid = class_ids.get(name)
        if not cid:
            raise SystemExit(f"property {prop['iriLocal']}: unknown domain class {name}")
        domain.append(cid)
    if domain:
        c["domainClasses"] = domain
    rng = []
    for name in prop.get("range") or []:
        cid = class_ids.get(name)
        if not cid:
            raise SystemExit(f"property {prop['iriLocal']}: unknown range class {name}")
        rng.append(cid)
    if rng:
        c["rangeClasses"] = rng
    if "minCount" in prop:
        c["minCount"] = prop["minCount"]
    if "maxCount" in prop:
        c["maxCount"] = prop["maxCount"]
    if prop.get("severity"):
        c["severity"] = prop["severity"]
    return c


def ensure_package(kc: KC, pkg: dict) -> None:
    status, body = kc.get(f"/v1/packages/{pkg['code']}")
    if status == 200:
        data = unwrap(body) if isinstance(body, dict) else body
        existing_base = (data.get("iriBase") or "").strip()
        want_base = (pkg.get("iriBase") or "").strip()
        if want_base and not existing_base:
            st, b = kc.patch(f"/v1/packages/{pkg['code']}", {"iriBase": want_base})
            if st not in (200, 201):
                raise SystemExit(f"set package iriBase: {st} {b}")
            print(f"package {pkg['code']} exists; set iriBase={want_base}")
        else:
            print(f"package {pkg['code']} exists")
        return
    status, body = kc.post("/v1/packages", {
        "code": pkg["code"],
        "lifecycle": pkg.get("lifecycle") or "continuous",
        "iriBase": pkg["iriBase"],
        "labels": pkg["labels"],
    })
    if status not in (200, 201):
        raise SystemExit(f"create package: {status} {body}")
    print(f"created package {pkg['code']}")


def ensure_instance_of(kc: KC, cat: dict, pkg_code: str, prop_ids: dict[str, str]) -> None:
    spec = cat.get("instanceOf") or {}
    if not spec.get("createIfSchemaConfigEmpty"):
        return
    status, cfg = kc.get("/v1/admin/schema-config")
    if status != 200:
        raise SystemExit(f"schema-config: {status} {cfg}")
    existing = (cfg.get("instanceOfProperty") or "").strip()
    if existing:
        print(f"schema-config instanceOfProperty={existing} (unchanged)")
        return
    iri = spec["iriLocal"]
    if iri not in prop_ids:
        payload = {
            "packageCode": pkg_code,
            "datatype": spec.get("datatype") or "EntityReference",
            "labels": spec["labels"],
            "descriptions": spec.get("descriptions") or {},
            "iriLocal": iri,
        }
        status, body = kc.post("/v1/properties", payload)
        if status not in (200, 201):
            raise SystemExit(f"create instanceOf: {status} {body}")
        pid = unwrap(body)["id"]
        prop_ids[iri] = pid
        print(f"created property {iri} -> {pid}")
    pid = prop_ids[iri]
    status, body = kc.put("/v1/admin/schema-config", {
        "instanceOfProperty": pid,
        "modelProperties": cfg.get("modelProperties") or [],
    })
    if status != 200:
        raise SystemExit(f"put schema-config: {status} {body}")
    print(f"set instanceOfProperty={pid}")


def ref_value(entity_id: str) -> dict:
    return {"type": "EntityReference", "entityId": entity_id}


def str_value(s: str) -> dict:
    return {"type": "String", "string": s}


def value_matches(stored: dict, wanted: dict) -> bool:
    if (stored or {}).get("type") != wanted.get("type"):
        return False
    t = wanted["type"]
    if t == "EntityReference":
        return stored.get("entityId") == wanted.get("entityId")
    if t == "String":
        return stored.get("string") == wanted.get("string")
    return stored == wanted


def ensure_upsert_statement(kc: KC, code: str, subject: str, prop: str, value: dict) -> None:
    status, body = kc.post("/v1/statements", {
        "packageCode": code,
        "subject": subject,
        "property": prop,
        "value": value,
        "upsert": True,
    })
    if status not in (200, 201):
        raise SystemExit(f"statement {subject} {prop}: {status} {body}")


def ensure_singleton_statement(kc: KC, code: str, subject: str, prop: str, value: dict) -> None:
    st, body = kc.get(f"/v1/entities/{id_path(subject)}/statements", property=prop)
    if st != 200:
        raise SystemExit(f"list statements {subject} {prop}: {st} {body}")
    stmts = body.get("statements") or []
    for s in stmts:
        if value_matches(s.get("value") or {}, value):
            return
    if not stmts:
        ensure_upsert_statement(kc, code, subject, prop, value)
        return
    s0 = stmts[0]
    status, body = kc.post(f"/v1/statements/{id_path(s0['id'])}/revise", {
        "expectedRevision": s0.get("revisionNo") or 1,
        "value": value,
    })
    if status not in (200, 201):
        raise SystemExit(f"revise {s0.get('id')}: {status} {body}")


def ensure_typed_entity(
    kc: KC,
    code: str,
    by_iri: dict[str, str],
    iri: str,
    labels: dict,
    class_id: str,
    instance_of: str,
) -> str:
    if iri in by_iri:
        qid = by_iri[iri]
    else:
        status, body = kc.post("/v1/entities", {
            "packageCode": code,
            "labels": labels,
            "iriLocal": iri,
        })
        if status not in (200, 201):
            raise SystemExit(f"create entity {iri}: {status} {body}")
        qid = unwrap(body)["id"]
        by_iri[iri] = qid
        print(f"created entity {iri} -> {qid}")
    ensure_upsert_statement(kc, code, qid, instance_of, ref_value(class_id))
    return qid


def load_usage_annotations(
    kc: KC,
    code: str,
    subjects: list[tuple[str, dict]],
    prop_ids: dict[str, str],
) -> None:
    guidance_p = prop_ids.get("usageGuidance")
    examples_p = prop_ids.get("usageExamples")
    if not guidance_p and not examples_p:
        return
    for subject_id, spec in subjects:
        if not subject_id:
            continue
        if guidance_p and spec.get("usageGuidance"):
            ensure_singleton_statement(kc, code, subject_id, guidance_p, str_value(spec["usageGuidance"]))
        if examples_p and spec.get("usageExamples"):
            ensure_singleton_statement(kc, code, subject_id, examples_p, str_value(spec["usageExamples"]))


def load_class_annotations(
    kc: KC, cat: dict, code: str, class_ids: dict[str, str], prop_ids: dict[str, str],
) -> None:
    layer_p = prop_ids.get("archiLayer")
    overlay_p = prop_ids.get("overlay")
    xtype_p = prop_ids.get("exchangeType")
    for cls in cat["classes"]:
        cid = class_ids.get(cls["iriLocal"])
        if not cid:
            continue
        if layer_p and cls.get("layer"):
            ensure_singleton_statement(kc, code, cid, layer_p, str_value(cls["layer"]))
        if overlay_p and cls.get("overlay"):
            ensure_singleton_statement(kc, code, cid, overlay_p, str_value(cls["overlay"]))
        if xtype_p and cls.get("exchangeType"):
            ensure_singleton_statement(kc, code, cid, xtype_p, str_value(cls["exchangeType"]))
    load_usage_annotations(
        kc, code,
        [(class_ids.get(cls["iriLocal"], ""), cls) for cls in cat["classes"]],
        prop_ids,
    )
    print("class annotations loaded")


def load_property_usage_annotations(
    kc: KC, cat: dict, code: str, prop_ids: dict[str, str],
) -> None:
    subjects: list[tuple[str, dict]] = []
    for prop in cat["properties"]:
        subjects.append((prop_ids.get(prop["iriLocal"], ""), prop))
    instance_of = cat.get("instanceOf") or {}
    if instance_of.get("iriLocal"):
        subjects.append((prop_ids.get(instance_of["iriLocal"], ""), instance_of))
    load_usage_annotations(kc, code, subjects, prop_ids)
    print("property usage annotations loaded")


def load_allowed_relationships(
    kc: KC,
    cat: dict,
    code: str,
    class_ids: dict[str, str],
    prop_ids: dict[str, str],
    by_iri: dict[str, str],
    instance_of: str,
) -> None:
    rule_cls = class_ids.get("AllowedRelationship")
    p_type = prop_ids.get("allowedRelType")
    p_src = prop_ids.get("allowedSourceClass")
    p_tgt = prop_ids.get("allowedTargetClass")
    if not all([rule_cls, p_type, p_src, p_tgt]):
        raise SystemExit("missing AllowedRelationship class or properties")
    n = 0
    for row in cat.get("allowedRelationships") or []:
        t, src, tgt = row["type"], row["source"], row["target"]
        for name in (t, src, tgt):
            if name not in class_ids:
                raise SystemExit(f"allowedRelationships: unknown class {name}")
        iri = f"allowed/{t}/{src}/{tgt}"
        qid = ensure_typed_entity(
            kc, code, by_iri, iri,
            {"en": f"{t} {src} → {tgt}"},
            rule_cls, instance_of,
        )
        ensure_singleton_statement(kc, code, qid, p_type, ref_value(class_ids[t]))
        ensure_singleton_statement(kc, code, qid, p_src, ref_value(class_ids[src]))
        ensure_singleton_statement(kc, code, qid, p_tgt, ref_value(class_ids[tgt]))
        n += 1
    print(f"allowedRelationships loaded ({n})")


def load_enums(
    kc: KC,
    cat: dict,
    code: str,
    class_ids: dict[str, str],
    prop_ids: dict[str, str],
    by_iri: dict[str, str],
    instance_of: str,
) -> None:
    enum_cls = class_ids.get("StringEnum")
    p_prop = prop_ids.get("enumeratesProperty")
    p_val = prop_ids.get("allowedValue")
    if not all([enum_cls, p_prop, p_val]):
        raise SystemExit("missing StringEnum class or properties")
    for name, values in (cat.get("enums") or {}).items():
        if name not in prop_ids:
            raise SystemExit(f"enums: unknown property {name}")
        iri = f"enum/{name}"
        qid = ensure_typed_entity(
            kc, code, by_iri, iri,
            {"en": f"Enum {name}"},
            enum_cls, instance_of,
        )
        ensure_singleton_statement(kc, code, qid, p_prop, ref_value(prop_ids[name]))
        for v in values:
            ensure_upsert_statement(kc, code, qid, p_val, str_value(v))
    print(f"enums loaded ({len(cat.get('enums') or {})})")


def load_exchange_spec(
    kc: KC,
    cat: dict,
    code: str,
    class_ids: dict[str, str],
    prop_ids: dict[str, str],
    by_iri: dict[str, str],
    instance_of: str,
) -> None:
    spec_cls = class_ids.get("ExchangeSpec")
    if not spec_cls:
        raise SystemExit("missing ExchangeSpec class")
    qid = ensure_typed_entity(
        kc, code, by_iri, "exchange-spec",
        {"en": "Open Exchange mapping"},
        spec_cls, instance_of,
    )
    ex = cat.get("exchange") or {}
    fields = {
        "catalogVersion": cat.get("version") or "",
        "exchangeFormat": ex.get("format") or "",
        "exchangeElementXsiType": ex.get("elementXsiType") or "",
        "exchangeRelationshipXsiType": ex.get("relationshipXsiType") or "",
        "exchangeDeployedOn": ex.get("deployedOn") or "",
        "exchangeRisk": ex.get("risk") or "",
        "exchangeViews": ex.get("views") or "",
        "exchangeIdentifier": ex.get("identifier") or "",
    }
    for iri, text in fields.items():
        pid = prop_ids.get(iri)
        if not pid:
            raise SystemExit(f"missing property {iri}")
        if text:
            ensure_singleton_statement(kc, code, qid, pid, str_value(text))
    print("exchange-spec loaded")


def main() -> int:
    base = os.environ.get("KC_BASE_URL", "http://localhost:8080")
    cat = json.loads(CATALOG.read_text())
    kc = KC(base)
    pkg = cat["package"]
    code = pkg["code"]

    try:
        st, _ = kc.req("GET", "/healthz")
    except urllib.error.URLError as e:
        raise SystemExit(f"cannot reach {base}: {e}") from e
    if st != 200:
        print(f"warning: {base}/healthz -> {st}", file=sys.stderr)

    ensure_package(kc, pkg)
    # Public IDs are full IRIs (iriBase + iriLocal); classify by kind, not Q/C/P prefixes.
    class_ids = index_by_iri(list_package_entities(kc, code, kind="class"))
    prop_ids = index_by_iri(list_package_entities(kc, code, kind="property"))
    by_iri = index_by_iri(list_package_entities(kc, code))

    for cls in cat["classes"]:
        iri = cls["iriLocal"]
        if iri in class_ids:
            print(f"class {iri} exists {class_ids[iri]}")
            continue
        parent = cls.get("parent")
        sub = ""
        if parent:
            sub = class_ids.get(parent, "")
            if not sub:
                raise SystemExit(f"class {iri}: parent {parent} not created yet")
        payload = {
            "packageCode": code,
            "labels": cls["labels"],
            "descriptions": cls.get("descriptions") or {},
            "iriLocal": iri,
        }
        if sub:
            payload["subClassOf"] = sub
        status, body = kc.post("/v1/classes", payload)
        if status not in (200, 201):
            raise SystemExit(f"create class {iri}: {status} {body}")
        cid = unwrap(body)["id"]
        class_ids[iri] = cid
        print(f"created class {iri} -> {cid}")

    for prop in cat["properties"]:
        iri = prop["iriLocal"]
        if iri in prop_ids:
            cons = constraints_payload(prop, class_ids)
            if cons:
                st, body = kc.patch(f"/v1/properties/{id_path(prop_ids[iri])}", {"constraints": cons})
                if st not in (200, 201):
                    raise SystemExit(f"patch property {iri}: {st} {body}")
            print(f"property {iri} exists {prop_ids[iri]}")
            continue
        payload = {
            "packageCode": code,
            "datatype": prop["datatype"],
            "labels": prop["labels"],
            "descriptions": prop.get("descriptions") or {},
            "iriLocal": iri,
            "constraints": constraints_payload(prop, class_ids),
        }
        status, body = kc.post("/v1/properties", payload)
        if status not in (200, 201):
            raise SystemExit(f"create property {iri}: {status} {body}")
        pid = unwrap(body)["id"]
        prop_ids[iri] = pid
        print(f"created property {iri} -> {pid}")

    ensure_instance_of(kc, cat, code, prop_ids)

    st, shapes_body = kc.get("/v1/shapes", package=code)
    if st != 200:
        raise SystemExit(f"list shapes: {st} {shapes_body}")
    existing_shapes = {s.get("code") for s in (shapes_body.get("items") or [])}

    for sh in cat["shapes"]:
        if sh["code"] in existing_shapes:
            print(f"shape {sh['code']} exists")
            continue
        cls_id = class_ids.get(sh["class"])
        if not cls_id:
            raise SystemExit(f"shape {sh['code']}: missing class {sh['class']}")
        required = []
        for name in sh.get("required") or []:
            pid = prop_ids.get(name)
            if not pid:
                raise SystemExit(f"shape {sh['code']}: missing property {name}")
            required.append(pid)
        doc = {"requiredProperties": required, "closed": False}
        if sh.get("severity"):
            doc["severity"] = sh["severity"]
        status, body = kc.post("/v1/shapes", {
            "code": sh["code"],
            "classId": cls_id,
            "packageCode": code,
            "document": doc,
        })
        if status not in (200, 201):
            raise SystemExit(f"create shape {sh['code']}: {status} {body}")
        print(f"created shape {sh['code']}")

    st, cfg = kc.get("/v1/admin/schema-config")
    if st != 200:
        raise SystemExit(f"schema-config: {st} {cfg}")
    instance_of = (cfg.get("instanceOfProperty") or "").strip()
    if not instance_of:
        raise SystemExit("instanceOfProperty is empty; cannot load catalog policy entities")

    by_iri = index_by_iri(list_package_entities(kc, code))
    load_class_annotations(kc, cat, code, class_ids, prop_ids)
    load_property_usage_annotations(kc, cat, code, prop_ids)
    load_allowed_relationships(kc, cat, code, class_ids, prop_ids, by_iri, instance_of)
    load_enums(kc, cat, code, class_ids, prop_ids, by_iri, instance_of)
    load_exchange_spec(kc, cat, code, class_ids, prop_ids, by_iri, instance_of)

    print("done.")
    print(f"classes={len(class_ids)} properties={len(prop_ids)}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
