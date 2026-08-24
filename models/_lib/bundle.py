"""Build a portable release bundle JSON from catalog.json (no live KC required).

Public IDs are full IRIs (iriBase + iriLocal). Statement IDs are deterministic
IRI locals under statement/… so the bundle is stable across rebuilds.

Dependency catalogs (package.dependencies[].dependsOnCode → models/<code>/catalog.json)
contribute class/property IRIs for cross-package statements without embedding those objects.
"""

from __future__ import annotations

import argparse
import json
import re
from pathlib import Path
from typing import Any

MODELS_ROOT = Path(__file__).resolve().parents[1]


def iri(base: str, local: str) -> str:
    return base + local


def stmt_iri(base: str, subject_local: str, prop_local: str, suffix: str = "") -> str:
    local = f"statement/{subject_local}/{prop_local}"
    if suffix:
        local = f"{local}/{suffix}"
    return iri(base, local)


def load_catalog(path: Path) -> dict:
    return json.loads(path.read_text())


def resolve_dep_catalogs(cat: dict, catalog_path: Path) -> list[dict]:
    """Load dependency catalogs by convention: models/<dependsOnCode>/catalog.json."""
    deps: list[dict] = []
    for dep in cat.get("package", {}).get("dependencies") or []:
        code = dep.get("dependsOnCode") or ""
        if not code:
            continue
        path = MODELS_ROOT / code / "catalog.json"
        if not path.is_file():
            # allow catalog-relative override
            rel = dep.get("catalog")
            if rel:
                path = (catalog_path.parent / rel).resolve()
        if not path.is_file():
            raise SystemExit(f"dependency catalog not found for {code}: tried {path}")
        deps.append(load_catalog(path))
    return deps


def id_maps_from_catalog(cat: dict) -> tuple[dict[str, str], dict[str, str]]:
    base = cat["package"]["iriBase"]
    class_ids: dict[str, str] = {}
    prop_ids: dict[str, str] = {}
    for cls in cat.get("classes") or []:
        class_ids[cls["iriLocal"]] = iri(base, cls["iriLocal"])
    instance_of = cat.get("instanceOf") or {}
    if instance_of.get("iriLocal"):
        prop_ids[instance_of["iriLocal"]] = iri(base, instance_of["iriLocal"])
    for prop in cat.get("properties") or []:
        prop_ids[prop["iriLocal"]] = iri(base, prop["iriLocal"])
    return class_ids, prop_ids


def merge_id_maps(*maps: tuple[dict[str, str], dict[str, str]]) -> tuple[dict[str, str], dict[str, str]]:
    class_ids: dict[str, str] = {}
    prop_ids: dict[str, str] = {}
    for c, p in maps:
        class_ids.update(c)
        prop_ids.update(p)
    return class_ids, prop_ids


def constraints_for(prop: dict, class_ids: dict[str, str]) -> dict[str, Any]:
    c: dict[str, Any] = {}
    domain = [class_ids[n] for n in (prop.get("domain") or [])]
    if domain:
        c["domainClasses"] = domain
    rng = [class_ids[n] for n in (prop.get("range") or [])]
    if rng:
        c["rangeClasses"] = rng
    if "minCount" in prop:
        c["minCount"] = prop["minCount"]
    if "maxCount" in prop:
        c["maxCount"] = prop["maxCount"]
    if prop.get("severity"):
        c["severity"] = prop["severity"]
    return c


def ref(entity_id: str) -> dict[str, str]:
    return {"type": "EntityReference", "entityId": entity_id}


def sval(s: str) -> dict[str, str]:
    return {"type": "String", "string": s}


def release_dependencies(cat: dict) -> list[dict[str, str]]:
    out: list[dict[str, str]] = []
    for dep in cat.get("package", {}).get("dependencies") or []:
        code = dep.get("dependsOnCode") or ""
        ver = dep.get("releaseVersion") or ""
        if code and ver:
            out.append({"dependencyCode": code, "dependencyVersion": ver})
    return out


def build(cat: dict, dep_catalogs: list[dict] | None = None) -> dict:
    dep_catalogs = dep_catalogs or []
    pkg = cat["package"]
    code = pkg["code"]
    base = pkg["iriBase"]
    version = cat["version"]
    published_at = "2026-08-24T00:00:00.000000Z"

    ext_class: dict[str, str] = {}
    ext_prop: dict[str, str] = {}
    for dep in dep_catalogs:
        c, p = id_maps_from_catalog(dep)
        ext_class.update(c)
        ext_prop.update(p)

    class_ids: dict[str, str] = dict(ext_class)
    classes: list[dict] = []
    for cls in cat.get("classes") or []:
        local = cls["iriLocal"]
        cid = iri(base, local)
        class_ids[local] = cid
        parent = cls.get("parent")
        item: dict[str, Any] = {
            "id": cid,
            "packageCode": code,
            "iriLocal": local,
            "revisionNo": 1,
            "status": "active",
            "labels": cls.get("labels") or {"en": local},
            "descriptions": cls.get("descriptions") or {},
        }
        if parent:
            if parent not in class_ids:
                raise SystemExit(f"class {local}: unknown parent {parent}")
            item["subClassOf"] = class_ids[parent]
        classes.append(item)

    prop_ids: dict[str, str] = dict(ext_prop)
    properties: list[dict] = []

    instance_of = cat.get("instanceOf") or {}
    if instance_of.get("iriLocal"):
        local = instance_of["iriLocal"]
        pid = iri(base, local)
        prop_ids[local] = pid
        properties.append({
            "id": pid,
            "packageCode": code,
            "iriLocal": local,
            "revisionNo": 1,
            "datatype": instance_of.get("datatype") or "EntityReference",
            "status": "active",
            "labels": instance_of.get("labels") or {"en": local},
            "descriptions": instance_of.get("descriptions") or {},
            "constraints": {},
        })

    for prop in cat.get("properties") or []:
        local = prop["iriLocal"]
        pid = iri(base, local)
        prop_ids[local] = pid
        item = {
            "id": pid,
            "packageCode": code,
            "iriLocal": local,
            "revisionNo": 1,
            "datatype": prop["datatype"],
            "status": "active",
            "labels": prop.get("labels") or {"en": local},
            "descriptions": prop.get("descriptions") or {},
        }
        cons = constraints_for(prop, class_ids)
        if cons:
            item["constraints"] = cons
        properties.append(item)

    shapes: list[dict] = []
    for sh in cat.get("shapes") or []:
        required = []
        for n in sh.get("required") or []:
            if n not in prop_ids:
                raise SystemExit(f"shape {sh['code']}: unknown property {n}")
            required.append(prop_ids[n])
        if sh["class"] not in class_ids:
            raise SystemExit(f"shape {sh['code']}: unknown class {sh['class']}")
        doc: dict[str, Any] = {"requiredProperties": required, "closed": False}
        if sh.get("severity"):
            doc["severity"] = sh["severity"]
        shapes.append({
            "code": sh["code"],
            "packageCode": code,
            "classId": class_ids[sh["class"]],
            "document": doc,
            "revisionNo": 1,
        })

    entities: list[dict] = []
    statements: list[dict] = []
    instance_of_pid = prop_ids.get("instanceOf", "")

    def add_typed_entity(local: str, labels: dict, class_local: str) -> str:
        eid = iri(base, local)
        entities.append({
            "id": eid,
            "packageCode": code,
            "iriLocal": local,
            "revisionNo": 1,
            "status": "active",
            "labels": labels,
            "descriptions": {},
        })
        if instance_of_pid:
            if class_local not in class_ids:
                raise SystemExit(f"entity {local}: unknown class {class_local}")
            statements.append({
                "id": stmt_iri(base, local, "instanceOf"),
                "packageCode": code,
                "revisionNo": 1,
                "subject": eid,
                "property": instance_of_pid,
                "status": "active",
                "value": ref(class_ids[class_local]),
            })
        return eid

    def add_string_stmt(subject_local: str, subject_id: str, prop_local: str, text: str, suffix: str = "") -> None:
        if prop_local not in prop_ids:
            raise SystemExit(f"unknown property for statement: {prop_local}")
        statements.append({
            "id": stmt_iri(base, subject_local, prop_local, suffix),
            "packageCode": code,
            "revisionNo": 1,
            "subject": subject_id,
            "property": prop_ids[prop_local],
            "status": "active",
            "value": sval(text),
        })

    def add_ref_stmt(subject_local: str, subject_id: str, prop_local: str, target_id: str) -> None:
        if prop_local not in prop_ids:
            raise SystemExit(f"unknown property for statement: {prop_local}")
        statements.append({
            "id": stmt_iri(base, subject_local, prop_local),
            "packageCode": code,
            "revisionNo": 1,
            "subject": subject_id,
            "property": prop_ids[prop_local],
            "status": "active",
            "value": ref(target_id),
        })

    for cls in cat.get("classes") or []:
        local = cls["iriLocal"]
        cid = class_ids[local]
        if cls.get("layer"):
            add_string_stmt(local, cid, "archiLayer", cls["layer"])
        if cls.get("overlay"):
            add_string_stmt(local, cid, "overlay", cls["overlay"])
        if cls.get("exchangeType"):
            add_string_stmt(local, cid, "exchangeType", cls["exchangeType"])
        if cls.get("usageGuidance"):
            add_string_stmt(local, cid, "usageGuidance", cls["usageGuidance"])
        if cls.get("usageExamples"):
            add_string_stmt(local, cid, "usageExamples", cls["usageExamples"])

    for prop in cat.get("properties") or []:
        local = prop["iriLocal"]
        pid = prop_ids[local]
        if prop.get("usageGuidance"):
            add_string_stmt(local, pid, "usageGuidance", prop["usageGuidance"])
        if prop.get("usageExamples"):
            add_string_stmt(local, pid, "usageExamples", prop["usageExamples"])

    if instance_of.get("iriLocal"):
        io_local = instance_of["iriLocal"]
        io_pid = prop_ids[io_local]
        if instance_of.get("usageGuidance"):
            add_string_stmt(io_local, io_pid, "usageGuidance", instance_of["usageGuidance"])
        if instance_of.get("usageExamples"):
            add_string_stmt(io_local, io_pid, "usageExamples", instance_of["usageExamples"])

    for row in cat.get("allowedRelationships") or []:
        t, src, tgt = row["type"], row["source"], row["target"]
        local = f"allowed/{t}/{src}/{tgt}"
        eid = add_typed_entity(local, {"en": f"{t} {src} → {tgt}"}, "AllowedRelationship")
        add_ref_stmt(local, eid, "allowedRelType", class_ids[t])
        add_ref_stmt(local, eid, "allowedSourceClass", class_ids[src])
        add_ref_stmt(local, eid, "allowedTargetClass", class_ids[tgt])

    for name, values in (cat.get("enums") or {}).items():
        local = f"enum/{name}"
        eid = add_typed_entity(local, {"en": f"Enum {name}"}, "StringEnum")
        if name not in prop_ids:
            raise SystemExit(f"enums: unknown property {name}")
        add_ref_stmt(local, eid, "enumeratesProperty", prop_ids[name])
        for v in values:
            add_string_stmt(local, eid, "allowedValue", v, suffix=v)

    if cat.get("exchange") is not None or any(
        c.get("iriLocal") == "ExchangeSpec" for c in (cat.get("classes") or [])
    ):
        ex = cat.get("exchange") or {}
        ex_local = "exchange-spec"
        ex_id = add_typed_entity(ex_local, {"en": "Open Exchange mapping"}, "ExchangeSpec")
        exchange_fields = {
            "catalogVersion": cat.get("version") or "",
            "exchangeFormat": ex.get("format") or "",
            "exchangeElementXsiType": ex.get("elementXsiType") or "",
            "exchangeRelationshipXsiType": ex.get("relationshipXsiType") or "",
            "exchangeDeployedOn": ex.get("deployedOn") or "",
            "exchangeRisk": ex.get("risk") or "",
            "exchangeViews": ex.get("views") or "",
            "exchangeIdentifier": ex.get("identifier") or "",
        }
        for prop_local, text in exchange_fields.items():
            if text:
                add_string_stmt(ex_local, ex_id, prop_local, text)

    object_index: list[dict] = []
    for c in classes:
        object_index.append({"objectType": "class", "objectPublicId": c["id"], "revisionNo": c["revisionNo"]})
    for p in properties:
        object_index.append({"objectType": "property", "objectPublicId": p["id"], "revisionNo": p["revisionNo"]})
    for e in entities:
        object_index.append({"objectType": "entity", "objectPublicId": e["id"], "revisionNo": e["revisionNo"]})
    for st in statements:
        object_index.append({"objectType": "statement", "objectPublicId": st["id"], "revisionNo": st["revisionNo"]})
    for sh in shapes:
        object_index.append({"objectType": "shape", "objectPublicId": sh["code"], "revisionNo": sh.get("revisionNo") or 1})

    manifest = {
        "formatVersion": 1,
        "package": code,
        "version": version,
        "publishedAt": published_at,
        "iriBase": base,
        "lifecycle": pkg.get("lifecycle") or "continuous",
        "labels": pkg.get("labels") or {"en": code},
        "dependencies": release_dependencies(cat),
        "objectIndex": object_index,
    }

    return {
        "manifest": manifest,
        "releases": [manifest],
        "classes": classes,
        "properties": properties,
        "entities": entities,
        "statements": statements,
        "shapes": shapes,
        "references": [],
    }


def _payload_fingerprint(obj: dict, kind: str) -> str:
    skip = {"revisionNo", "id"}
    if kind == "shape":
        skip = {"revisionNo"}
    cleaned = {k: v for k, v in obj.items() if k not in skip}
    return json.dumps(cleaned, sort_keys=True, ensure_ascii=False)


def apply_baseline_revisions(bundle: dict, baseline: dict | None) -> int:
    if not baseline:
        return 0
    bumped = 0

    def index_by(items: list, key: str = "id") -> dict[str, dict]:
        return {it[key]: it for it in items}

    sections = [
        ("classes", "id"),
        ("properties", "id"),
        ("entities", "id"),
        ("statements", "id"),
        ("shapes", "code"),
    ]
    for section, key in sections:
        old_map = index_by(baseline.get(section) or [], key)
        for item in bundle.get(section) or []:
            oid = item[key]
            prev = old_map.get(oid)
            if prev is None:
                item["revisionNo"] = int(item.get("revisionNo") or 1)
                continue
            prev_rev = int(prev.get("revisionNo") or 1)
            if _payload_fingerprint(prev, section) == _payload_fingerprint(item, section):
                item["revisionNo"] = prev_rev
            else:
                item["revisionNo"] = prev_rev + 1
                bumped += 1

    object_index: list[dict] = []
    for c in bundle.get("classes") or []:
        object_index.append({"objectType": "class", "objectPublicId": c["id"], "revisionNo": c["revisionNo"]})
    for p in bundle.get("properties") or []:
        object_index.append({"objectType": "property", "objectPublicId": p["id"], "revisionNo": p["revisionNo"]})
    for e in bundle.get("entities") or []:
        object_index.append({"objectType": "entity", "objectPublicId": e["id"], "revisionNo": e["revisionNo"]})
    for st in bundle.get("statements") or []:
        object_index.append({"objectType": "statement", "objectPublicId": st["id"], "revisionNo": st["revisionNo"]})
    for sh in bundle.get("shapes") or []:
        object_index.append({"objectType": "shape", "objectPublicId": sh["code"], "revisionNo": sh.get("revisionNo") or 1})
    bundle["manifest"]["objectIndex"] = object_index
    if bundle.get("releases"):
        bundle["releases"][0] = bundle["manifest"]
    return bumped


def find_baseline(releases_dir: Path, version: str) -> Path | None:
    def parse(v: str) -> tuple[int, int, int] | None:
        m = re.fullmatch(r"(\d+)\.(\d+)\.(\d+)", v.strip())
        if not m:
            return None
        return int(m.group(1)), int(m.group(2)), int(m.group(3))

    target = parse(version)
    if not target or not releases_dir.is_dir():
        return None
    best: tuple[tuple[int, int, int], Path] | None = None
    for path in releases_dir.glob("*.bundle.json"):
        m = re.search(r"-(\d+\.\d+\.\d+)\.bundle\.json$", path.name)
        if not m:
            continue
        ver = parse(m.group(1))
        if ver is None or ver >= target:
            continue
        if best is None or ver > best[0]:
            best = (ver, path)
    return best[1] if best else None


def main(package_dir: Path | None = None) -> int:
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument(
        "--package-dir",
        type=Path,
        default=package_dir,
        help="Package directory containing catalog.json (default: caller package dir)",
    )
    ap.add_argument("--catalog", type=Path, default=None)
    ap.add_argument("--out", type=Path, default=None)
    ap.add_argument(
        "--baseline",
        type=Path,
        default=None,
        help="Previous bundle JSON for revision bumps (default: auto from releases/)",
    )
    args = ap.parse_args()
    pkg_dir = args.package_dir
    if pkg_dir is None:
        raise SystemExit("--package-dir is required")
    catalog_path = args.catalog or (pkg_dir / "catalog.json")
    cat = load_catalog(catalog_path)
    dep_catalogs = resolve_dep_catalogs(cat, catalog_path)
    bundle = build(cat, dep_catalogs)
    out = args.out
    if out is None:
        out = pkg_dir / "releases" / f"{cat['package']['code']}-{cat['version']}.bundle.json"
    baseline_path = args.baseline
    if baseline_path is None:
        baseline_path = find_baseline(pkg_dir / "releases", cat["version"])
    baseline = None
    if baseline_path and baseline_path.is_file():
        baseline = json.loads(baseline_path.read_text())
    bumped = apply_baseline_revisions(bundle, baseline)
    out.parent.mkdir(parents=True, exist_ok=True)
    out.write_text(json.dumps(bundle, ensure_ascii=False, indent=2) + "\n")
    m = bundle["manifest"]
    print(f"wrote {out}")
    if baseline_path:
        print(f"baseline={baseline_path} bumped_revisions={bumped}")
    print(
        f"package={m['package']}@{m['version']} "
        f"deps={m['dependencies']} "
        f"classes={len(bundle['classes'])} properties={len(bundle['properties'])} "
        f"entities={len(bundle['entities'])} statements={len(bundle['statements'])} "
        f"shapes={len(bundle['shapes'])}"
    )
    return 0
