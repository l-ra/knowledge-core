package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/l-ra/knowledge-core/internal/datatype"
	"github.com/l-ra/knowledge-core/internal/domain"
)

const (
	kcBasePackageCode       = "kc-base"
	kcBasePackageClassLocal = "Package"
	kcBasePackageCodeLocal  = "packageCode"
)

var (
	ErrPackageRootProtected = errors.New("package root cannot be deprecated or deleted")
	ErrManagedPackageCode   = errors.New("packageCode statement is managed by the package")
)

func (s *Store) createPackageRootInTx(
	ctx context.Context,
	tx pgx.Tx,
	cs *changeSetTx,
	meta domain.WriteMeta,
	pkg domain.Package,
	descriptions map[string]string,
) (string, error) {
	if pkg.IRIBase == "" {
		return "", nil
	}
	labels := pkg.Labels
	if labels == nil {
		labels = map[string]string{}
	}
	if err := datatype.RequireLabelEN(labels); err != nil {
		return "", err
	}
	descs := descriptions
	if descs == nil {
		descs = map[string]string{}
	}

	var existing string
	err := tx.QueryRow(ctx, `
		SELECT public_id FROM entity
		WHERE package_id = $1 AND iri_local = $2 AND status <> 'deleted'
		LIMIT 1
	`, pkg.ID, datatype.PackageRootIRILocal).Scan(&existing)
	if err == nil {
		return existing, s.ensurePackageRootStatementsTx(ctx, tx, cs, meta, pkg, existing)
	}
	if err != nil && err != pgx.ErrNoRows {
		return "", err
	}

	id := datatype.NewUUID()
	publicID := pkg.IRIBase
	iriLocal := datatype.PackageRootIRILocal
	now := time.Now().UTC()
	_, err = tx.Exec(ctx, `
		INSERT INTO entity (id, public_id, status, current_revision_no, package_id, iri_local, created_at, updated_at)
		VALUES ($1,$2,'active',1,$3,$4,$5,$5)
	`, id, publicID, pkg.ID, iriLocal, now)
	if err != nil {
		return "", fmt.Errorf("create package root: %w", err)
	}
	for lang, text := range labels {
		if _, err := tx.Exec(ctx, `INSERT INTO entity_label (entity_id, lang, text) VALUES ($1,$2,$3)`, id, lang, text); err != nil {
			return "", err
		}
	}
	for lang, text := range descs {
		if _, err := tx.Exec(ctx, `INSERT INTO entity_description (entity_id, lang, text) VALUES ($1,$2,$3)`, id, lang, text); err != nil {
			return "", err
		}
	}
	labelsJSON, _ := labelsToJSON(labels)
	descJSON, _ := labelsToJSON(descs)
	_, err = tx.Exec(ctx, `
		INSERT INTO entity_revision (id, entity_id, revision_no, status, labels, descriptions, change_set_id, actor, created_at)
		VALUES ($1,$2,1,'active',$3,$4,$5,$6,$7)
	`, datatype.NewUUID(), id, labelsJSON, descJSON, cs.id, meta.Actor, now)
	if err != nil {
		return "", err
	}
	if err := cs.addItem(ctx, tx, "entity", id, publicID, "create", nil); err != nil {
		return "", err
	}
	if err := s.ensurePackageRootStatementsTx(ctx, tx, cs, meta, pkg, publicID); err != nil {
		return "", err
	}
	return publicID, nil
}

func (s *Store) ensurePackageRootStatementsTx(
	ctx context.Context,
	tx pgx.Tx,
	cs *changeSetTx,
	meta domain.WriteMeta,
	pkg domain.Package,
	rootPublicID string,
) error {
	if rootPublicID == "" {
		return nil
	}
	classID, err := s.lookupKCBaseObjectTx(ctx, tx, "class", kcBasePackageClassLocal)
	if err != nil {
		return err
	}
	if classID != "" {
		cfg, err := s.getSchemaConfigTx(ctx, tx)
		if err != nil {
			return err
		}
		if cfg != nil && cfg.InstanceOfProperty != "" {
			if _, err := s.createStatementInTx(ctx, tx, cs, meta, domain.CreateStatementInput{
				PackageCode:      pkg.Code,
				SubjectPublicID:  rootPublicID,
				PropertyPublicID: cfg.InstanceOfProperty,
				Value:            datatype.Value{Type: datatype.EntityReference, EntityID: &classID},
				Upsert:           true,
			}); err != nil {
				return fmt.Errorf("package root instanceOf: %w", err)
			}
		}
	}
	propID, err := s.lookupKCBaseObjectTx(ctx, tx, "property", kcBasePackageCodeLocal)
	if err != nil {
		return err
	}
	if propID != "" {
		code := pkg.Code
		if _, err := s.createStatementInTx(ctx, tx, cs, meta, domain.CreateStatementInput{
			PackageCode:      pkg.Code,
			SubjectPublicID:  rootPublicID,
			PropertyPublicID: propID,
			Value:            datatype.Value{Type: datatype.String, String: &code},
			Upsert:           true,
		}); err != nil {
			return fmt.Errorf("package root packageCode: %w", err)
		}
	}
	return nil
}

func (s *Store) lookupKCBaseObjectTx(ctx context.Context, tx pgx.Tx, kind, iriLocal string) (string, error) {
	var q string
	switch kind {
	case "class":
		q = `
			SELECT e.public_id FROM entity e
			JOIN package p ON p.id = e.package_id
			JOIN class_profile cp ON cp.entity_id = e.id
			WHERE p.code = $1 AND e.iri_local = $2 AND e.status <> 'deleted'
			LIMIT 1`
	case "property":
		q = `
			SELECT e.public_id FROM entity e
			JOIN package p ON p.id = e.package_id
			JOIN property_profile pp ON pp.entity_id = e.id
			WHERE p.code = $1 AND e.iri_local = $2 AND e.status <> 'deleted'
			LIMIT 1`
	default:
		return "", fmt.Errorf("unknown kind %q", kind)
	}
	var pub string
	err := tx.QueryRow(ctx, q, kcBasePackageCode, iriLocal).Scan(&pub)
	if err == pgx.ErrNoRows {
		return "", nil
	}
	return pub, err
}

func (s *Store) getSchemaConfigTx(ctx context.Context, tx pgx.Tx) (*domain.ModelSchemaConfig, error) {
	var instanceOf string
	var modelProps []byte
	err := tx.QueryRow(ctx, `
		SELECT COALESCE(instance_of_property,''), COALESCE(model_properties,'[]'::jsonb)
		FROM model_schema_config WHERE id = 1
	`).Scan(&instanceOf, &modelProps)
	if err == pgx.ErrNoRows {
		return &domain.ModelSchemaConfig{}, nil
	}
	if err != nil {
		return nil, err
	}
	cfg := &domain.ModelSchemaConfig{InstanceOfProperty: instanceOf}
	_ = json.Unmarshal(modelProps, &cfg.ModelProperties)
	return cfg, nil
}

func (s *Store) fillPackageRoot(ctx context.Context, pkg *domain.Package) error {
	if pkg == nil || pkg.IRIBase == "" {
		return nil
	}
	var publicID string
	var labelsJSON, descJSON []byte
	err := s.pool.QueryRow(ctx, `
		SELECT e.public_id, er.labels, er.descriptions
		FROM entity e
		JOIN entity_revision er ON er.entity_id = e.id AND er.revision_no = e.current_revision_no
		WHERE e.package_id = $1 AND e.iri_local = $2 AND e.status <> 'deleted'
		LIMIT 1
	`, pkg.ID, datatype.PackageRootIRILocal).Scan(&publicID, &labelsJSON, &descJSON)
	if err == pgx.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	pkg.RootEntityID = publicID
	descs, _ := jsonToLabels(descJSON)
	pkg.Descriptions = descs
	return nil
}

func (s *Store) syncPackageLabelsFromRootTx(ctx context.Context, tx pgx.Tx, entityID uuid.UUID, labels map[string]string) error {
	if labels == nil {
		return nil
	}
	now := time.Now().UTC()
	labelsJSON, _ := json.Marshal(labels)
	_, err := tx.Exec(ctx, `
		UPDATE package p
		SET labels = $2, updated_at = $3
		FROM entity e
		WHERE e.id = $1 AND e.package_id = p.id AND e.iri_local = $4
	`, entityID, labelsJSON, now, datatype.PackageRootIRILocal)
	return err
}

func (s *Store) syncRootLabelsFromPackageTx(ctx context.Context, tx pgx.Tx, cs *changeSetTx, meta domain.WriteMeta, pkg domain.Package) error {
	if pkg.IRIBase == "" || pkg.Labels == nil {
		return nil
	}
	var entityID uuid.UUID
	var publicID string
	var rev int
	err := tx.QueryRow(ctx, `
		SELECT id, public_id, current_revision_no FROM entity
		WHERE package_id = $1 AND iri_local = $2 AND status <> 'deleted'
		LIMIT 1
	`, pkg.ID, datatype.PackageRootIRILocal).Scan(&entityID, &publicID, &rev)
	if err == pgx.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	_, err = s.updateEntityInTx(ctx, tx, cs, meta, publicID, domain.UpdateEntityInput{
		Labels:           pkg.Labels,
		ExpectedRevision: rev,
	})
	return err
}

func (s *Store) isPackageRootEntityTx(ctx context.Context, tx pgx.Tx, publicID string) (bool, error) {
	var iriLocal string
	err := tx.QueryRow(ctx, `SELECT COALESCE(iri_local,'') FROM entity WHERE public_id = $1`, publicID).Scan(&iriLocal)
	if err != nil {
		return false, err
	}
	return datatype.IsPackageRootIRILocal(iriLocal), nil
}

func (s *Store) assertNotPackageRootTx(ctx context.Context, tx pgx.Tx, publicID string) error {
	ok, err := s.isPackageRootEntityTx(ctx, tx, publicID)
	if err != nil {
		return err
	}
	if ok {
		return ErrPackageRootProtected
	}
	return nil
}

func (s *Store) assertNotManagedPackageCodeStatementTx(ctx context.Context, tx pgx.Tx, statementPublicID string) error {
	var subjectLocal, propLocal, propPkg string
	err := tx.QueryRow(ctx, `
		SELECT COALESCE(sub.iri_local,''), COALESCE(prop.iri_local,''), COALESCE(ppkg.code,'')
		FROM statement st
		JOIN entity sub ON sub.id = st.subject_id
		JOIN entity prop ON prop.id = st.property_id
		LEFT JOIN package ppkg ON ppkg.id = prop.package_id
		WHERE st.public_id = $1
	`, statementPublicID).Scan(&subjectLocal, &propLocal, &propPkg)
	if err != nil {
		return err
	}
	if datatype.IsPackageRootIRILocal(subjectLocal) && propPkg == kcBasePackageCode && propLocal == kcBasePackageCodeLocal {
		return ErrManagedPackageCode
	}
	return nil
}

func (s *Store) relocatePackageRootTx(ctx context.Context, tx pgx.Tx, pkg domain.Package, oldBase, newBase string) error {
	if newBase == "" {
		return nil
	}
	var entityID uuid.UUID
	var publicID string
	err := tx.QueryRow(ctx, `
		SELECT id, public_id FROM entity
		WHERE package_id = $1 AND iri_local = $2 AND status <> 'deleted'
		LIMIT 1
	`, pkg.ID, datatype.PackageRootIRILocal).Scan(&entityID, &publicID)
	if err == pgx.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	if publicID == newBase {
		return nil
	}
	var conflict int
	if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM entity WHERE public_id = $1`, newBase).Scan(&conflict); err != nil {
		return err
	}
	if conflict > 0 {
		return fmt.Errorf("%w: entity already exists at iriBase %s", ErrConflict, newBase)
	}
	_, err = tx.Exec(ctx, `UPDATE entity SET public_id = $2, updated_at = $3 WHERE id = $1`, entityID, newBase, time.Now().UTC())
	return err
}

// EnsurePackageRootTyping attaches instanceOf/packageCode to an existing root when vocabulary becomes available.
func (s *Store) EnsurePackageRootTyping(ctx context.Context, meta domain.WriteMeta, packageCode string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	pkg, err := s.GetPackageByCode(ctx, packageCode)
	if err != nil {
		return err
	}
	if pkg.IRIBase == "" {
		return nil
	}
	var rootPublicID string
	err = tx.QueryRow(ctx, `
		SELECT public_id FROM entity
		WHERE package_id = $1 AND iri_local = $2 AND status <> 'deleted'
		LIMIT 1
	`, pkg.ID, datatype.PackageRootIRILocal).Scan(&rootPublicID)
	if err == pgx.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	cs, err := s.beginChangeSetTx(ctx, tx, meta)
	if err != nil {
		return err
	}
	if err := s.ensurePackageRootStatementsTx(ctx, tx, cs, meta, *pkg, rootPublicID); err != nil {
		return err
	}
	if err := s.finalizeChangeSet(ctx, tx, cs, map[string]string{"package": packageCode, "root": rootPublicID}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
