package store

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/l-ra/knowledge-core/internal/datatype"
	"github.com/l-ra/knowledge-core/internal/domain"
	"github.com/l-ra/knowledge-core/internal/rdf"
)

type rdfCreateNode struct {
	IRI           string
	IRILocal      string
	Labels        map[string]string
	Datatype      datatype.Type
	SubClassOfIRI string
}

type rdfAliasAdd struct {
	NodeIRI  string
	PublicID string
	AliasIRI string
	Kind     string
}

type rdfStmtPlan struct {
	SubjectIRI  string
	PropertyIRI string
	Value       datatype.Value
}

type rdfImportPlan struct {
	Result           domain.RDFImportResult
	resolved         map[string]string
	createProps      []rdfCreateNode
	createClasses    []rdfCreateNode
	createEntities   []rdfCreateNode
	addAliases       []rdfAliasAdd
	createStatements []rdfStmtPlan
}

func (s *Store) ImportRDF(ctx context.Context, meta domain.WriteMeta, packageCode string, in domain.RDFImportInput) (*domain.RDFImportResult, error) {
	if strings.TrimSpace(packageCode) == "" {
		return nil, fmt.Errorf("packageCode required")
	}
	triples, err := rdf.ParseNTriples(in.NTriples)
	if err != nil {
		return &domain.RDFImportResult{DryRun: in.DryRun, Errors: []string{err.Error()}}, nil
	}
	for _, tr := range triples {
		if tr.Subject.Kind == rdf.TermBlank || tr.Object.Kind == rdf.TermBlank {
			return &domain.RDFImportResult{
				DryRun: in.DryRun,
				Errors: []string{fmt.Sprintf("line %d: blank nodes are not supported in v1", tr.Line)},
			}, nil
		}
	}

	pkg, err := s.GetPackageByCode(ctx, packageCode)
	if err != nil {
		return nil, err
	}

	plan, err := s.buildRDFImportPlan(ctx, pkg, triples)
	if err != nil {
		return nil, err
	}
	plan.Result.DryRun = in.DryRun
	if in.DryRun || len(plan.Result.Errors) > 0 {
		return &plan.Result, nil
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	cs, err := s.beginChangeSetTx(ctx, tx, meta)
	if err != nil {
		return nil, err
	}

	iriToPublic := map[string]string{}
	for iri, pub := range plan.resolved {
		iriToPublic[iri] = pub
	}

	for _, c := range plan.createProps {
		p, err := s.createPropertyInTx(ctx, tx, cs, meta, domain.CreatePropertyInput{
			PackageCode: packageCode, Datatype: c.Datatype, Labels: c.Labels, IRILocal: c.IRILocal,
		})
		if err != nil {
			return nil, err
		}
		iriToPublic[c.IRI] = p.PublicID
		if err := s.addImportedAliasTx(ctx, tx, p.ID, c.IRI, c.IRILocal, pkg.IRIBase); err != nil {
			return nil, err
		}
		plan.Result.Created = append(plan.Result.Created, domain.RDFImportAction{
			Kind: "property", IRI: c.IRI, PublicID: p.PublicID, Detail: string(c.Datatype),
		})
	}
	for _, c := range plan.createClasses {
		cl, err := s.createClassInTx(ctx, tx, cs, meta, domain.CreateClassInput{
			PackageCode: packageCode, Labels: c.Labels, IRILocal: c.IRILocal,
			SubClassOf: resolvePlanned(iriToPublic, c.SubClassOfIRI),
		})
		if err != nil {
			return nil, err
		}
		var id uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT id FROM entity WHERE public_id = $1`, cl.PublicID).Scan(&id); err != nil {
			return nil, err
		}
		iriToPublic[c.IRI] = cl.PublicID
		if err := s.addImportedAliasTx(ctx, tx, id, c.IRI, c.IRILocal, pkg.IRIBase); err != nil {
			return nil, err
		}
		plan.Result.Created = append(plan.Result.Created, domain.RDFImportAction{
			Kind: "class", IRI: c.IRI, PublicID: cl.PublicID,
		})
	}
	for _, c := range plan.createEntities {
		ent, err := s.createEntityInTx(ctx, tx, cs, meta, domain.CreateEntityInput{
			PackageCode: packageCode, Labels: c.Labels, IRILocal: c.IRILocal,
		})
		if err != nil {
			return nil, err
		}
		iriToPublic[c.IRI] = ent.PublicID
		if err := s.addImportedAliasTx(ctx, tx, ent.ID, c.IRI, c.IRILocal, pkg.IRIBase); err != nil {
			return nil, err
		}
		plan.Result.Created = append(plan.Result.Created, domain.RDFImportAction{
			Kind: "entity", IRI: c.IRI, PublicID: ent.PublicID,
		})
	}

	for _, a := range plan.addAliases {
		pub := iriToPublic[a.NodeIRI]
		if pub == "" {
			pub = a.PublicID
		}
		if pub == "" {
			continue
		}
		var id uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT id FROM entity WHERE public_id = $1`, pub).Scan(&id); err != nil {
			return nil, err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO entity_iri_alias (entity_id, iri, kind) VALUES ($1,$2,$3)
			ON CONFLICT DO NOTHING
		`, id, a.AliasIRI, a.Kind); err != nil {
			return nil, err
		}
		plan.Result.Created = append(plan.Result.Created, domain.RDFImportAction{
			Kind: "alias", IRI: a.AliasIRI, PublicID: pub, Detail: a.Kind,
		})
	}

	for _, st := range plan.createStatements {
		subj := iriToPublic[st.SubjectIRI]
		prop := iriToPublic[st.PropertyIRI]
		if subj == "" || prop == "" {
			plan.Result.Skipped = append(plan.Result.Skipped, domain.RDFImportAction{
				Kind: "statement", IRI: st.SubjectIRI, Detail: "unresolved subject/property",
			})
			continue
		}
		val := st.Value
		if val.Type == datatype.EntityReference && val.EntityID != nil {
			ref := iriToPublic[*val.EntityID]
			if ref == "" {
				ref = *val.EntityID
			}
			val.EntityID = &ref
		}
		exists, err := s.statementValueExistsTx(ctx, tx, subj, prop, val)
		if err != nil {
			return nil, err
		}
		if exists {
			plan.Result.Skipped = append(plan.Result.Skipped, domain.RDFImportAction{
				Kind: "statement", PublicID: subj, Detail: "duplicate skipped",
			})
			continue
		}
		var dt string
		if err := tx.QueryRow(ctx, `
			SELECT pp.datatype FROM property_profile pp
			JOIN entity e ON e.id = pp.entity_id WHERE e.public_id = $1
		`, prop).Scan(&dt); err != nil {
			return nil, err
		}
		if err := datatype.Validate(datatype.Type(dt), val); err != nil {
			plan.Result.Skipped = append(plan.Result.Skipped, domain.RDFImportAction{
				Kind: "statement", PublicID: subj, Detail: "datatype mismatch: " + err.Error(),
			})
			plan.Result.Warnings = append(plan.Result.Warnings, fmt.Sprintf("skip statement %s %s: %v", subj, prop, err))
			continue
		}
		created, err := s.createStatementInTx(ctx, tx, cs, meta, domain.CreateStatementInput{
			PackageCode: packageCode, SubjectPublicID: subj, PropertyPublicID: prop, Value: val,
		})
		if err != nil {
			plan.Result.Warnings = append(plan.Result.Warnings, fmt.Sprintf("statement %s/%s: %v", subj, prop, err))
			plan.Result.Skipped = append(plan.Result.Skipped, domain.RDFImportAction{
				Kind: "statement", PublicID: subj, Detail: err.Error(),
			})
			continue
		}
		plan.Result.Created = append(plan.Result.Created, domain.RDFImportAction{
			Kind: "statement", PublicID: created.PublicID, Detail: subj + " " + prop,
		})
	}

	summary := map[string]any{
		"created": len(plan.Result.Created), "skipped": len(plan.Result.Skipped), "warnings": plan.Result.Warnings,
	}
	if err := s.finalizeChangeSet(ctx, tx, cs, summary); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	plan.Result.ChangeSetID = cs.publicID
	plan.Result.DryRun = false
	return &plan.Result, nil
}

func (s *Store) buildRDFImportPlan(ctx context.Context, pkg *domain.Package, triples []rdf.Triple) (*rdfImportPlan, error) {
	plan := &rdfImportPlan{resolved: map[string]string{}}

	type nodeAcc struct {
		types    []string
		labels   map[string]string
		sameAs   []string
		subClass []string
		domains  []string
		ranges   []string
	}
	nodes := map[string]*nodeAcc{}
	ensure := func(iri string) *nodeAcc {
		n, ok := nodes[iri]
		if !ok {
			n = &nodeAcc{labels: map[string]string{}}
			nodes[iri] = n
		}
		return n
	}

	type stmtAcc struct {
		subj, pred string
		obj        rdf.Term
		line       int
	}
	var stmts []stmtAcc
	observed := map[string][]datatype.Type{}
	usedAsType := map[string]bool{}

	for _, tr := range triples {
		if tr.Predicate.Kind != rdf.TermIRI || tr.Subject.Kind != rdf.TermIRI {
			plan.Result.Errors = append(plan.Result.Errors, fmt.Sprintf("line %d: subject/predicate must be IRI", tr.Line))
			continue
		}
		sIRI, pIRI := tr.Subject.Value, tr.Predicate.Value
		ensure(sIRI)
		ensure(pIRI)
		if tr.Object.Kind == rdf.TermIRI {
			ensure(tr.Object.Value)
		}

		switch pIRI {
		case rdf.IRIType:
			if tr.Object.Kind == rdf.TermIRI {
				ensure(sIRI).types = append(ensure(sIRI).types, tr.Object.Value)
				usedAsType[tr.Object.Value] = true
			}
		case rdf.IRILabel:
			if tr.Object.Kind == rdf.TermLiteral {
				lang := tr.Object.Lang
				if lang == "" {
					lang = "en"
				}
				ensure(sIRI).labels[lang] = tr.Object.Value
			}
		case rdf.IRISameAs:
			if tr.Object.Kind == rdf.TermIRI {
				ensure(sIRI).sameAs = append(ensure(sIRI).sameAs, tr.Object.Value)
			}
		case rdf.IRISubClass:
			if tr.Object.Kind == rdf.TermIRI {
				ensure(sIRI).subClass = append(ensure(sIRI).subClass, tr.Object.Value)
			}
		case rdf.IRIDomain:
			if tr.Object.Kind == rdf.TermIRI {
				ensure(sIRI).domains = append(ensure(sIRI).domains, tr.Object.Value)
				plan.Result.Warnings = append(plan.Result.Warnings,
					fmt.Sprintf("rdfs:domain on %s noted only (%s)", sIRI, tr.Object.Value))
			}
		case rdf.IRIRange:
			if tr.Object.Kind == rdf.TermIRI {
				ensure(sIRI).ranges = append(ensure(sIRI).ranges, tr.Object.Value)
			}
		default:
			stmts = append(stmts, stmtAcc{subj: sIRI, pred: pIRI, obj: tr.Object, line: tr.Line})
			if tr.Object.Kind == rdf.TermLiteral {
				_, dt, _ := rdf.LiteralToValue(tr.Object)
				observed[pIRI] = append(observed[pIRI], dt)
			} else if tr.Object.Kind == rdf.TermIRI {
				observed[pIRI] = append(observed[pIRI], datatype.EntityReference)
			}
		}
	}
	if len(plan.Result.Errors) > 0 {
		return plan, nil
	}

	for iri := range nodes {
		pub, err := s.resolveIRIToPublicID(ctx, pkg, iri)
		if err != nil {
			return nil, err
		}
		if pub != "" {
			plan.resolved[iri] = pub
			plan.Result.Matched = append(plan.Result.Matched, domain.RDFImportAction{
				Kind: "match", IRI: iri, PublicID: pub,
			})
		}
	}

	kinds := map[string]rdf.NodeKind{}
	for iri, n := range nodes {
		kinds[iri] = rdf.ClassifyNode(n.types)
	}
	for _, st := range stmts {
		if kinds[st.pred] == rdf.KindEntity && len(nodes[st.pred].types) == 0 {
			kinds[st.pred] = rdf.KindProperty
		}
	}
	for iri := range usedAsType {
		if rdf.IsWellKnownVocabIRI(iri) {
			continue
		}
		if kinds[iri] == rdf.KindEntity && len(nodes[iri].types) == 0 {
			kinds[iri] = rdf.KindClass
		}
	}

	labelFor := func(iri string) map[string]string {
		n := nodes[iri]
		labels := map[string]string{}
		for k, v := range n.labels {
			labels[k] = v
		}
		if strings.TrimSpace(labels["en"]) == "" {
			if len(labels) > 0 {
				for _, v := range labels {
					labels["en"] = v
					break
				}
			} else {
				labels["en"] = rdf.LocalNameFromIRI(iri)
			}
		}
		return labels
	}
	iriLocalFor := func(iri string) string {
		if pkg.IRIBase != "" && strings.HasPrefix(iri, pkg.IRIBase) {
			local := strings.TrimPrefix(iri, pkg.IRIBase)
			local, _ = datatype.NormalizeIRILocal(local)
			return local
		}
		return ""
	}

	for iri, kind := range kinds {
		if _, ok := plan.resolved[iri]; ok {
			continue
		}
		if rdf.IsWellKnownVocabIRI(iri) {
			continue
		}
		switch kind {
		case rdf.KindProperty:
			dt := rdf.InferPropertyDatatype(nodes[iri].ranges, observed[iri])
			plan.createProps = append(plan.createProps, rdfCreateNode{
				IRI: iri, IRILocal: iriLocalFor(iri), Labels: labelFor(iri), Datatype: dt,
			})
		case rdf.KindClass:
			sub := ""
			if len(nodes[iri].subClass) > 0 {
				sub = nodes[iri].subClass[0]
			}
			plan.createClasses = append(plan.createClasses, rdfCreateNode{
				IRI: iri, IRILocal: iriLocalFor(iri), Labels: labelFor(iri), SubClassOfIRI: sub,
			})
		default:
			plan.createEntities = append(plan.createEntities, rdfCreateNode{
				IRI: iri, IRILocal: iriLocalFor(iri), Labels: labelFor(iri),
			})
		}
	}

	for iri, n := range nodes {
		for _, a := range n.sameAs {
			plan.addAliases = append(plan.addAliases, rdfAliasAdd{
				NodeIRI: iri, PublicID: plan.resolved[iri], AliasIRI: a, Kind: "sameAs",
			})
		}
		if plan.resolved[iri] == "" && iriLocalFor(iri) == "" {
			plan.addAliases = append(plan.addAliases, rdfAliasAdd{
				NodeIRI: iri, AliasIRI: iri, Kind: "imported",
			})
		}
	}

	for _, st := range stmts {
		var val datatype.Value
		if st.obj.Kind == rdf.TermLiteral {
			v, _, _ := rdf.LiteralToValue(st.obj)
			val = v
		} else {
			objIRI := st.obj.Value
			val = datatype.Value{Type: datatype.EntityReference, EntityID: &objIRI}
		}
		plan.createStatements = append(plan.createStatements, rdfStmtPlan{
			SubjectIRI: st.subj, PropertyIRI: st.pred, Value: val,
		})
	}

	for _, c := range plan.createProps {
		plan.Result.WouldCreate = append(plan.Result.WouldCreate, domain.RDFImportAction{
			Kind: "property", IRI: c.IRI, Detail: string(c.Datatype),
		})
	}
	for _, c := range plan.createClasses {
		plan.Result.WouldCreate = append(plan.Result.WouldCreate, domain.RDFImportAction{Kind: "class", IRI: c.IRI})
	}
	for _, c := range plan.createEntities {
		plan.Result.WouldCreate = append(plan.Result.WouldCreate, domain.RDFImportAction{Kind: "entity", IRI: c.IRI})
	}
	for range plan.createStatements {
		plan.Result.WouldCreate = append(plan.Result.WouldCreate, domain.RDFImportAction{Kind: "statement"})
	}
	return plan, nil
}

func resolvePlanned(m map[string]string, iri string) string {
	if iri == "" {
		return ""
	}
	if pub, ok := m[iri]; ok {
		return pub
	}
	return ""
}

func (s *Store) resolveIRIToPublicID(ctx context.Context, pkg *domain.Package, iri string) (string, error) {
	var pub string
	err := s.pool.QueryRow(ctx, `
		SELECT e.public_id FROM entity_iri_alias a
		JOIN entity e ON e.id = a.entity_id
		WHERE a.iri = $1 AND e.status <> 'deleted'
		LIMIT 1
	`, iri).Scan(&pub)
	if err == nil {
		return pub, nil
	}
	if err != nil && err != pgx.ErrNoRows {
		return "", err
	}
	if pkg.IRIBase == "" || !strings.HasPrefix(iri, pkg.IRIBase) {
		return "", nil
	}
	if iri == pkg.IRIBase {
		err = s.pool.QueryRow(ctx, `
			SELECT e.public_id FROM entity e
			WHERE e.package_id = $1 AND e.status <> 'deleted'
			  AND (e.iri_local = $2 OR e.public_id = $3)
			LIMIT 1
		`, pkg.ID, datatype.PackageRootIRILocal, pkg.IRIBase).Scan(&pub)
		if err == pgx.ErrNoRows {
			return "", nil
		}
		return pub, err
	}
	local := strings.TrimPrefix(iri, pkg.IRIBase)
	err = s.pool.QueryRow(ctx, `
		SELECT e.public_id FROM entity e
		WHERE e.package_id = $1 AND e.status <> 'deleted'
		  AND (e.iri_local = $2 OR (e.iri_local = '' AND e.public_id = $2))
		LIMIT 1
	`, pkg.ID, local).Scan(&pub)
	if err == pgx.ErrNoRows {
		return "", nil
	}
	return pub, err
}

func (s *Store) addImportedAliasTx(ctx context.Context, tx pgx.Tx, entityID uuid.UUID, iri, iriLocal, iriBase string) error {
	if iriBase != "" && iriLocal != "" && iriBase+iriLocal == iri {
		return nil
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO entity_iri_alias (entity_id, iri, kind) VALUES ($1,$2,'imported')
		ON CONFLICT DO NOTHING
	`, entityID, iri)
	return err
}

func (s *Store) statementValueExistsTx(ctx context.Context, tx pgx.Tx, subject, property string, val datatype.Value) (bool, error) {
	var exists bool
	switch val.Type {
	case datatype.String, datatype.URI:
		text := ""
		if val.Type == datatype.String && val.String != nil {
			text = *val.String
		}
		if val.Type == datatype.URI && val.URI != nil {
			text = *val.URI
		}
		err := tx.QueryRow(ctx, `
			SELECT EXISTS(
				SELECT 1 FROM statement st
				JOIN entity sub ON sub.id = st.subject_id
				JOIN entity prop ON prop.id = st.property_id
				JOIN statement_current sc ON sc.statement_id = st.id
				WHERE sub.public_id = $1 AND prop.public_id = $2 AND st.status = 'active'
				  AND sc.value_type = $3 AND sc.value_text = $4
			)`, subject, property, string(val.Type), text).Scan(&exists)
		return exists, err
	case datatype.Boolean:
		err := tx.QueryRow(ctx, `
			SELECT EXISTS(
				SELECT 1 FROM statement st
				JOIN entity sub ON sub.id = st.subject_id
				JOIN entity prop ON prop.id = st.property_id
				JOIN statement_current sc ON sc.statement_id = st.id
				WHERE sub.public_id = $1 AND prop.public_id = $2 AND st.status = 'active'
				  AND sc.value_type = 'Boolean' AND sc.value_bool = $3
			)`, subject, property, val.Bool).Scan(&exists)
		return exists, err
	case datatype.Integer:
		err := tx.QueryRow(ctx, `
			SELECT EXISTS(
				SELECT 1 FROM statement st
				JOIN entity sub ON sub.id = st.subject_id
				JOIN entity prop ON prop.id = st.property_id
				JOIN statement_current sc ON sc.statement_id = st.id
				WHERE sub.public_id = $1 AND prop.public_id = $2 AND st.status = 'active'
				  AND sc.value_type = 'Integer' AND sc.value_int64 = $3
			)`, subject, property, val.Int64).Scan(&exists)
		return exists, err
	case datatype.EntityReference:
		eid := ""
		if val.EntityID != nil {
			eid = *val.EntityID
		}
		err := tx.QueryRow(ctx, `
			SELECT EXISTS(
				SELECT 1 FROM statement st
				JOIN entity sub ON sub.id = st.subject_id
				JOIN entity prop ON prop.id = st.property_id
				JOIN statement_current sc ON sc.statement_id = st.id
				JOIN entity ref ON ref.id = sc.value_entity_id
				WHERE sub.public_id = $1 AND prop.public_id = $2 AND st.status = 'active'
				  AND sc.value_type = 'EntityReference' AND ref.public_id = $3
			)`, subject, property, eid).Scan(&exists)
		return exists, err
	default:
		return false, nil
	}
}
