"""Load a model catalog into a running knowledge-core via HTTP API.

Idempotent. Loads package.dependencies first (models/<code>/catalog.json).
"""

from __future__ import annotations

import json
import os
import sys
import urllib.error
import urllib.parse
import urllib.request
from pathlib import Path

MODELS_ROOT = Path(__file__).resolve().parents[1]


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
    deps = []
    for d in pkg.get("dependencies") or []:
        code = d.get("dependsOnCode")
        if not code:
            continue
        deps.append({
            "dependsOnCode": code,
            "versionRange": d.get("versionRange") or "*",
        })
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
    payload: dict = {
        "code": pkg["code"],
        "lifecycle": pkg.get("lifecycle") or "continuous",
        "iriBase": pkg["iriBase"],
        "labels": pkg["labels"],
    }
    if pkg.get("descriptions"):
        payload["descriptions"] = pkg["descriptions"]
    if deps:
        payload["dependencies"] = deps
    status, body = kc.post("/v1/packages", payload)
    if status not in (200, 201):
        raise SystemExit(f"create package: {status} {body}")
    print(f"created package {pkg['code']}")


def ensure_package_root_meta(kc: KC, pkg_code: str, class_ids: dict[str, str], prop_ids: dict[str, str]) -> None:
    """Attach instanceOf→Package and packageCode on the package-root after vocabulary exists."""
    status, body = kc.get(f"/v1/packages/{pkg_code}")
    if status != 200:
        return
    data = unwrap(body) if isinstance(body, dict) else body
    root = (data.get("rootEntityId") or data.get("iriBase") or "").strip()
    if not root:
        print(f"package {pkg_code}: no root entity (missing iriBase?)")
        return
    package_class = class_ids.get("Package") or ""
    package_code_prop = prop_ids.get("packageCode") or ""
    instance_of = prop_ids.get("instanceOf") or ""
    if not instance_of:
        st, cfg = kc.get("/v1/admin/schema-config")
        if st == 200 and isinstance(cfg, dict):
            instance_of = (cfg.get("instanceOfProperty") or "").strip()
    if package_class and instance_of:
        ensure_upsert_statement(kc, pkg_code, root, instance_of, ref_value(package_class))
        print(f"package {pkg_code} root typed as Package")
    if package_code_prop:
        ensure_upsert_statement(kc, pkg_code, root, package_code_prop, str_value(pkg_code))
        print(f"package {pkg_code} root packageCode={pkg_code}")


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


def int_value(n: int) -> dict:
    return {"type": "Integer", "int64": int(n)}


def bool_value(b: bool) -> dict:
    return {"type": "Boolean", "bool": bool(b)}


def value_matches(stored: dict, wanted: dict) -> bool:
    if (stored or {}).get("type") != wanted.get("type"):
        return False
    t = wanted["type"]
    if t == "EntityReference":
        return stored.get("entityId") == wanted.get("entityId")
    if t == "String":
        return stored.get("string") == wanted.get("string")
    if t == "Integer":
        return stored.get("int64") == wanted.get("int64")
    if t == "Boolean":
        return stored.get("bool") == wanted.get("bool")
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
    for cls in cat.get("classes") or []:
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
        [(class_ids.get(cls["iriLocal"], ""), cls) for cls in (cat.get("classes") or [])],
        prop_ids,
    )
    print("class annotations loaded")


def load_property_usage_annotations(
    kc: KC, cat: dict, code: str, prop_ids: dict[str, str],
) -> None:
    subjects: list[tuple[str, dict]] = []
    for prop in cat.get("properties") or []:
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
    if not cat.get("allowedRelationships"):
        return
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


def iter_enums(cat: dict) -> list[tuple[str, str, list[str]]]:
    """Yield (enum_iri_local, property_iri_local, values) from catalog enums."""
    out: list[tuple[str, str, list[str]]] = []
    for name, spec in (cat.get("enums") or {}).items():
        if isinstance(spec, dict):
            prop = spec.get("property") or name
            values = list(spec.get("values") or [])
            local = spec.get("iriLocal") or name
        else:
            prop = name
            values = list(spec or [])
            local = name
        out.append((local, prop, values))
    return out


def load_enums(
    kc: KC,
    cat: dict,
    code: str,
    class_ids: dict[str, str],
    prop_ids: dict[str, str],
    by_iri: dict[str, str],
    instance_of: str,
) -> None:
    if not cat.get("enums"):
        return
    enum_cls = class_ids.get("StringEnum")
    p_prop = prop_ids.get("enumeratesProperty")
    p_val = prop_ids.get("allowedValue")
    if not all([enum_cls, p_prop, p_val]):
        raise SystemExit("missing StringEnum class or properties (expected from kc-base)")
    rows = iter_enums(cat)
    for enum_local, prop_local, values in rows:
        if prop_local not in prop_ids:
            raise SystemExit(f"enums: unknown property {prop_local}")
        iri = f"enum/{enum_local}"
        qid = ensure_typed_entity(
            kc, code, by_iri, iri,
            {"en": f"Enum {enum_local}"},
            enum_cls, instance_of,
        )
        ensure_singleton_statement(kc, code, qid, p_prop, ref_value(prop_ids[prop_local]))
        for v in values:
            ensure_upsert_statement(kc, code, qid, p_val, str_value(v))
    print(f"enums loaded ({len(rows)})")


def load_exchange_spec(
    kc: KC,
    cat: dict,
    code: str,
    class_ids: dict[str, str],
    prop_ids: dict[str, str],
    by_iri: dict[str, str],
    instance_of: str,
) -> None:
    if not any(c.get("iriLocal") == "ExchangeSpec" for c in (cat.get("classes") or [])):
        return
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


def instance_stmt_value(raw, prop: dict | None, resolve_ref) -> dict:
    if isinstance(raw, bool):
        return bool_value(raw)
    if isinstance(raw, int) and not isinstance(raw, bool):
        return int_value(raw)
    if isinstance(raw, dict):
        if "ref" in raw:
            return ref_value(resolve_ref(raw["ref"]))
        if raw.get("type") == "EntityReference" or "entityId" in raw:
            target = raw.get("entityId") or raw.get("ref")
            if target and not str(target).startswith("http"):
                target = resolve_ref(str(target))
            return ref_value(target)
        if raw.get("type") == "String" or "string" in raw:
            return str_value(str(raw.get("string") or ""))
        if raw.get("type") == "Integer":
            n = raw.get("int64")
            if n is None:
                n = raw.get("int") or 0
            return int_value(int(n))
        if raw.get("type") == "Boolean":
            return bool_value(bool(raw.get("bool")))
    dt = (prop or {}).get("datatype") or "String"
    if dt == "EntityReference":
        return ref_value(resolve_ref(str(raw)))
    if dt == "Integer":
        return int_value(int(raw))
    if dt == "Boolean":
        if isinstance(raw, str):
            return bool_value(raw.lower() in ("true", "1", "yes"))
        return bool_value(bool(raw))
    return str_value(str(raw))


def load_instances(
    kc: KC,
    cat: dict,
    code: str,
    class_ids: dict[str, str],
    prop_ids: dict[str, str],
    prop_specs: dict[str, dict],
    by_iri: dict[str, str],
    instance_of: str,
) -> None:
    rows = cat.get("instances") or []
    if not rows:
        return
    if not instance_of:
        raise SystemExit("instanceOfProperty is empty; cannot load catalog instances")

    def resolve_ref(name: str) -> str:
        if name in by_iri:
            return by_iri[name]
        if name in class_ids:
            return class_ids[name]
        if name.startswith("http://") or name.startswith("https://"):
            return name
        raise SystemExit(f"instance ref {name}: unknown iriLocal")

    for inst in rows:
        local = inst.get("iriLocal") or ""
        class_local = inst.get("class") or ""
        if not local or not class_local:
            raise SystemExit("instances: iriLocal and class are required")
        cid = class_ids.get(class_local)
        if not cid:
            raise SystemExit(f"instance {local}: unknown class {class_local}")
        ensure_typed_entity(
            kc, code, by_iri, local,
            inst.get("labels") or {"en": local},
            cid, instance_of,
        )

    n_stmt = 0
    for inst in rows:
        local = inst["iriLocal"]
        subject = by_iri[local]
        fields = inst.get("properties") or inst.get("statements") or {}
        for prop_local, raw in fields.items():
            pid = prop_ids.get(prop_local)
            if not pid:
                raise SystemExit(f"instance {local}: unknown property {prop_local}")
            value = instance_stmt_value(raw, prop_specs.get(prop_local), resolve_ref)
            ensure_upsert_statement(kc, code, subject, pid, value)
            n_stmt += 1
    print(f"instances loaded ({len(rows)} entities, {n_stmt} statements)")


def merge_dep_ids(kc: KC, cat: dict) -> tuple[dict[str, str], dict[str, str]]:
    """Resolve class/property IRIs from dependency packages already loaded in KC."""
    class_ids: dict[str, str] = {}
    prop_ids: dict[str, str] = {}
    for dep in cat.get("package", {}).get("dependencies") or []:
        code = dep.get("dependsOnCode") or ""
        if not code:
            continue
        class_ids.update(index_by_iri(list_package_entities(kc, code, kind="class")))
        prop_ids.update(index_by_iri(list_package_entities(kc, code, kind="property")))
    return class_ids, prop_ids


def load_one(kc: KC, catalog_path: Path, loaded: set[str] | None = None) -> None:
    loaded = loaded if loaded is not None else set()
    cat = json.loads(catalog_path.read_text())
    pkg = cat["package"]
    code = pkg["code"]
    if code in loaded:
        return

    for dep in pkg.get("dependencies") or []:
        dep_code = dep.get("dependsOnCode") or ""
        if not dep_code or dep_code in loaded:
            continue
        dep_path = MODELS_ROOT / dep_code / "catalog.json"
        if not dep_path.is_file():
            raise SystemExit(f"dependency catalog missing: {dep_path}")
        print(f"--- loading dependency {dep_code} ---")
        load_one(kc, dep_path, loaded)

    print(f"--- loading package {code} ---")
    ensure_package(kc, pkg)

    class_ids, prop_ids = merge_dep_ids(kc, cat)
    local_classes = index_by_iri(list_package_entities(kc, code, kind="class"))
    local_props = index_by_iri(list_package_entities(kc, code, kind="property"))
    class_ids.update(local_classes)
    prop_ids.update(local_props)

    for cls in cat.get("classes") or []:
        iri = cls["iriLocal"]
        if iri in local_classes:
            class_ids[iri] = local_classes[iri]
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
        local_classes[iri] = cid
        print(f"created class {iri} -> {cid}")

    for prop in cat.get("properties") or []:
        iri = prop["iriLocal"]
        if iri in local_props:
            prop_ids[iri] = local_props[iri]
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
        local_props[iri] = pid
        print(f"created property {iri} -> {pid}")

    ensure_instance_of(kc, cat, code, prop_ids)
    # Refresh local prop map after possible instanceOf create
    local_props = index_by_iri(list_package_entities(kc, code, kind="property"))
    prop_ids.update(local_props)
    ensure_package_root_meta(kc, code, class_ids, prop_ids)

    st, shapes_body = kc.get("/v1/shapes", package=code)
    if st != 200:
        raise SystemExit(f"list shapes: {st} {shapes_body}")
    existing_shapes = {s.get("code") for s in (shapes_body.get("items") or [])}

    for sh in cat.get("shapes") or []:
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
    needs_policy = bool(
        cat.get("allowedRelationships")
        or cat.get("enums")
        or cat.get("instances")
        or any(c.get("iriLocal") == "ExchangeSpec" for c in (cat.get("classes") or []))
        or any(c.get("usageGuidance") or c.get("usageExamples") or c.get("layer") for c in (cat.get("classes") or []))
        or any(p.get("usageGuidance") or p.get("usageExamples") for p in (cat.get("properties") or []))
        or (cat.get("instanceOf") or {}).get("usageGuidance")
    )
    if needs_policy and not instance_of:
        raise SystemExit("instanceOfProperty is empty; cannot load catalog policy entities")

    by_iri = index_by_iri(list_package_entities(kc, code))
    load_class_annotations(kc, cat, code, class_ids, prop_ids)
    load_property_usage_annotations(kc, cat, code, prop_ids)
    prop_specs = {p["iriLocal"]: p for p in (cat.get("properties") or [])}
    if instance_of:
        load_allowed_relationships(kc, cat, code, class_ids, prop_ids, by_iri, instance_of)
        load_enums(kc, cat, code, class_ids, prop_ids, by_iri, instance_of)
        load_exchange_spec(kc, cat, code, class_ids, prop_ids, by_iri, instance_of)
        load_instances(kc, cat, code, class_ids, prop_ids, prop_specs, by_iri, instance_of)

    loaded.add(code)
    print(f"done {code}: classes={len(class_ids)} properties={len(prop_ids)}")


def main(package_dir: Path | None = None) -> int:
    import argparse

    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--package-dir", type=Path, default=package_dir)
    ap.add_argument("--catalog", type=Path, default=None)
    args = ap.parse_args()
    pkg_dir = args.package_dir
    if pkg_dir is None:
        raise SystemExit("--package-dir is required")
    catalog_path = args.catalog or (pkg_dir / "catalog.json")

    base = os.environ.get("KC_BASE_URL", "http://localhost:8080")
    kc = KC(base)
    try:
        st, _ = kc.req("GET", "/healthz")
    except urllib.error.URLError as e:
        raise SystemExit(f"cannot reach {base}: {e}") from e
    if st != 200:
        print(f"warning: {base}/healthz -> {st}", file=sys.stderr)

    load_one(kc, catalog_path)
    print("done.")
    return 0
