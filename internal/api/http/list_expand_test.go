package apihttp_test

import (
	"encoding/json"
	"net/http"
	"net/url"
	"testing"
)

func TestListExpandFacetsBatchRead(t *testing.T) {
	h := setupTestHandler(t)
	pidType := seedKcBaseInstanceOf(t, h)

	animal := doJSON(t, h, http.MethodPost, "/v1/classes", map[string]any{
		"packageCode": "test", "labels": map[string]string{"en": "Animal"}, "iriLocal": "Animal",
	}, nil)
	animalID := parseDataID(t, animal.Body)

	kindProp := doJSON(t, h, http.MethodPost, "/v1/properties", map[string]any{
		"packageCode": "test", "datatype": "String",
		"labels": map[string]string{"en": "kind"}, "iriLocal": "kind",
	}, nil)
	kindPID := parseDataID(t, kindProp.Body)

	fido := doJSON(t, h, http.MethodPost, "/v1/entities", map[string]any{
		"packageCode": "test", "labels": map[string]string{"en": "Fido"}, "iriLocal": "fido-expand",
	}, nil)
	fidoID := parseDataID(t, fido.Body)
	stType := doJSON(t, h, http.MethodPost, "/v1/statements", map[string]any{
		"packageCode": "test", "subject": fidoID, "property": pidType,
		"value": map[string]any{"type": "EntityReference", "entityId": animalID},
	}, nil)
	if stType.StatusCode != http.StatusCreated {
		t.Fatalf("instanceOf: %d %s", stType.StatusCode, stType.Body)
	}
	stKind := doJSON(t, h, http.MethodPost, "/v1/statements", map[string]any{
		"packageCode": "test", "subject": fidoID, "property": kindPID,
		"value": map[string]any{"type": "String", "string": "dog"},
	}, nil)
	if stKind.StatusCode != http.StatusCreated {
		t.Fatalf("kind stmt: %d %s", stKind.StatusCode, stKind.Body)
	}

	// B1-c: statements without properties → 400
	bad := doJSON(t, h, http.MethodGet, "/v1/entities?package=test&include=statements&limit=10", nil, nil)
	if bad.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 without properties, got %d %s", bad.StatusCode, bad.Body)
	}

	// B1-a + B1-b
	listed := doJSON(t, h, http.MethodGet,
		"/v1/entities?package=test&iriLocal=fido-expand&include=effectiveClasses,statements&properties="+url.QueryEscape(kindPID),
		nil, nil)
	if listed.StatusCode != http.StatusOK {
		t.Fatalf("list expand: %d %s", listed.StatusCode, listed.Body)
	}
	var listBody struct {
		Items []map[string]any `json:"items"`
	}
	_ = json.Unmarshal([]byte(listed.Body), &listBody)
	if len(listBody.Items) != 1 {
		t.Fatalf("expected 1 item: %s", listed.Body)
	}
	item := listBody.Items[0]
	eff, _ := item["effectiveClasses"].([]any)
	if len(eff) < 1 {
		t.Fatalf("expected effectiveClasses: %s", listed.Body)
	}
	stmts, _ := item["statements"].([]any)
	if len(stmts) != 1 {
		t.Fatalf("expected 1 whitelisted statement, got %v", item["statements"])
	}

	// B2 facets
	facets := doJSON(t, h, http.MethodGet, "/v1/entities/facets?package=test&groupBy=instanceOf", nil, nil)
	if facets.StatusCode != http.StatusOK {
		t.Fatalf("facets: %d %s", facets.StatusCode, facets.Body)
	}
	var facetBody struct {
		Facets []struct {
			ClassID string `json:"classId"`
			Count   int    `json:"count"`
		} `json:"facets"`
	}
	_ = json.Unmarshal([]byte(facets.Body), &facetBody)
	found := false
	for _, f := range facetBody.Facets {
		if f.ClassID == animalID && f.Count >= 1 {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected animal facet: %s", facets.Body)
	}

	byType := doJSON(t, h, http.MethodGet, "/v1/entities?package=test&instanceOf="+animalID+"&includeSubclasses=false&limit=200", nil, nil)
	var byTypeBody struct {
		Items []any `json:"items"`
	}
	_ = json.Unmarshal([]byte(byType.Body), &byTypeBody)
	var animalCount int
	for _, f := range facetBody.Facets {
		if f.ClassID == animalID {
			animalCount = f.Count
		}
	}
	if animalCount != len(byTypeBody.Items) {
		t.Fatalf("facet count %d != list instanceOf %d", animalCount, len(byTypeBody.Items))
	}

	// B3 batch-read
	batch := doJSON(t, h, http.MethodPost, "/v1/entities/batch-read", map[string]any{
		"ids":        []string{fidoID, "Q999999999"},
		"include":    []string{"effectiveClasses", "statements"},
		"properties": []string{kindPID},
	}, nil)
	if batch.StatusCode != http.StatusOK {
		t.Fatalf("batch-read: %d %s", batch.StatusCode, batch.Body)
	}
	var batchBody struct {
		Results []map[string]any `json:"results"`
	}
	_ = json.Unmarshal([]byte(batch.Body), &batchBody)
	if len(batchBody.Results) != 2 {
		t.Fatalf("expected 2 results: %s", batch.Body)
	}
	if batchBody.Results[0]["entity"] == nil {
		t.Fatalf("expected entity for fido: %s", batch.Body)
	}
	if batchBody.Results[1]["error"] != "not_found" {
		t.Fatalf("expected not_found for missing id: %s", batch.Body)
	}
}
