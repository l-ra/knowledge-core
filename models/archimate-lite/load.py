#!/usr/bin/env python3
"""Load ArchiMate Lite metamodel into a running knowledge-core via HTTP API.

Does not modify knowledge-core source. Idempotent: skips existing package
objects matched by iriLocal, and existing shapes matched by code.

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


def list_package_entities(kc: KC, code: str) -> list[dict]:
    items: list[dict] = []
    cursor = ""
    while True:
        q = {"package": code, "limit": "200"}
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
    by_iri = index_by_iri(list_package_entities(kc, code))
    class_ids = {k: v for k, v in by_iri.items() if str(v).startswith("C")}
    prop_ids = {k: v for k, v in by_iri.items() if str(v).startswith("P")}

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
                st, body = kc.patch(f"/v1/properties/{prop_ids[iri]}", {"constraints": cons})
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

    print("done.")
    print(f"classes={len(class_ids)} properties={len(prop_ids)}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
