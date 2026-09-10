package apihttp_test

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestAcceptanceGraphAPIExtensions(t *testing.T) {
	h := setupTestHandler(t)
	pidType := seedKcBaseInstanceOf(t, h)

	animal := doJSON(t, h, http.MethodPost, "/v1/classes", map[string]any{
		"packageCode": "test", "labels": map[string]string{"en": "Animal"}, "iriLocal": "Animal",
	}, nil)
	animalID := parseDataID(t, animal.Body)
	dog := doJSON(t, h, http.MethodPost, "/v1/classes", map[string]any{
		"packageCode": "test", "labels": map[string]string{"en": "Dog"}, "iriLocal": "Dog",
		"subClassOf": animalID,
	}, nil)
	dogID := parseDataID(t, dog.Body)

	gotClass := doJSON(t, h, http.MethodGet, "/v1/classes/"+dogID, nil, nil)
	if gotClass.StatusCode != http.StatusOK {
		t.Fatalf("get class: %d %s", gotClass.StatusCode, gotClass.Body)
	}
	var classBody map[string]any
	_ = json.Unmarshal([]byte(gotClass.Body), &classBody)
	if classBody["iriLocal"] != "Dog" {
		t.Fatalf("class iriLocal: %+v", classBody)
	}
	eff, _ := classBody["effectiveClasses"].([]any)
	if len(eff) < 2 {
		t.Fatalf("expected ancestry Animal+Dog, got %v", classBody["effectiveClasses"])
	}

	rel := doJSON(t, h, http.MethodPost, "/v1/properties", map[string]any{
		"packageCode": "test", "datatype": "EntityReference",
		"labels": map[string]string{"en": "pet"}, "iriLocal": "pet",
		"constraints": map[string]any{"rangeClasses": []string{animalID}, "severity": "error"},
	}, nil)
	pidPet := parseDataID(t, rel.Body)

	fido := doJSON(t, h, http.MethodPost, "/v1/entities", map[string]any{
		"packageCode": "test", "labels": map[string]string{"en": "Fido"}, "iriLocal": "fido",
	}, nil)
	fidoID := parseDataID(t, fido.Body)
	owner := doJSON(t, h, http.MethodPost, "/v1/entities", map[string]any{
		"packageCode": "test", "labels": map[string]string{"en": "Owner"}, "iriLocal": "owner",
	}, nil)
	ownerID := parseDataID(t, owner.Body)

	stType := doJSON(t, h, http.MethodPost, "/v1/statements", map[string]any{
		"packageCode": "test", "subject": fidoID, "property": pidType,
		"value": map[string]any{"type": "EntityReference", "entityId": dogID},
	}, nil)
	if stType.StatusCode != http.StatusCreated {
		t.Fatalf("instanceOf: %d %s", stType.StatusCode, stType.Body)
	}
	stPet := doJSON(t, h, http.MethodPost, "/v1/statements", map[string]any{
		"packageCode": "test", "subject": ownerID, "property": pidPet,
		"value": map[string]any{"type": "EntityReference", "entityId": fidoID},
	}, nil)
	if stPet.StatusCode != http.StatusCreated {
		t.Fatalf("pet stmt: %d %s", stPet.StatusCode, stPet.Body)
	}
	var petWrap struct {
		Validation struct {
			Summary struct{ Errors int } `json:"summary"`
		} `json:"validation"`
	}
	_ = json.Unmarshal([]byte(stPet.Body), &petWrap)
	if petWrap.Validation.Summary.Errors > 0 {
		t.Fatalf("range should accept Dog as Animal, body %s", stPet.Body)
	}

	listed := doJSON(t, h, http.MethodGet, "/v1/entities?iriLocal=fido&package=test", nil, nil)
	var list struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	_ = json.Unmarshal([]byte(listed.Body), &list)
	if len(list.Items) != 1 || list.Items[0].ID != fidoID {
		t.Fatalf("iriLocal lookup: %s", listed.Body)
	}

	byType := doJSON(t, h, http.MethodGet, "/v1/entities?instanceOf="+animalID+"&includeSubclasses=true", nil, nil)
	_ = json.Unmarshal([]byte(byType.Body), &list)
	found := false
	for _, it := range list.Items {
		if it.ID == fidoID {
			found = true
		}
	}
	if !found {
		t.Fatalf("instanceOf subclass list missing fido: %s", byType.Body)
	}

	inc := doJSON(t, h, http.MethodGet, "/v1/entities/"+fidoID+"/incoming?property="+pidPet, nil, nil)
	if inc.StatusCode != http.StatusOK {
		t.Fatalf("incoming: %d %s", inc.StatusCode, inc.Body)
	}
	var incBody struct {
		Statements []struct {
			Subject string `json:"subject"`
		} `json:"statements"`
	}
	_ = json.Unmarshal([]byte(inc.Body), &incBody)
	if len(incBody.Statements) != 1 || incBody.Statements[0].Subject != ownerID {
		t.Fatalf("incoming: %s", inc.Body)
	}

	graph := doJSON(t, h, http.MethodGet, "/v1/entities/"+ownerID+"/graph?depth=1", nil, nil)
	if graph.StatusCode != http.StatusOK {
		t.Fatalf("graph: %d %s", graph.StatusCode, graph.Body)
	}

	dup := doJSON(t, h, http.MethodPost, "/v1/statements", map[string]any{
		"packageCode": "test", "subject": ownerID, "property": pidPet, "upsert": true,
		"value": map[string]any{"type": "EntityReference", "entityId": fidoID},
	}, nil)
	if dup.StatusCode != http.StatusOK {
		t.Fatalf("upsert existing: %d %s", dup.StatusCode, dup.Body)
	}

	createPkg(t, h, "other", nil)
	mv := doJSON(t, h, http.MethodPost, "/v1/entities/"+fidoID+"/move", map[string]any{
		"packageCode": "other",
	}, nil)
	if mv.StatusCode != http.StatusOK {
		t.Fatalf("move: %d %s", mv.StatusCode, mv.Body)
	}

	patch := doJSON(t, h, http.MethodPatch, "/v1/properties/"+pidPet, map[string]any{
		"constraints": map[string]any{"rangeClasses": []string{dogID}},
	}, nil)
	if patch.StatusCode != http.StatusOK {
		t.Fatalf("patch property: %d %s", patch.StatusCode, patch.Body)
	}

	sh := doJSON(t, h, http.MethodPost, "/v1/shapes", map[string]any{
		"code": "animal-shape", "classId": animalID, "packageCode": "test",
		"document": map[string]any{"requiredProperties": []string{pidType}, "severity": "warning"},
	}, nil)
	if sh.StatusCode != http.StatusCreated {
		t.Fatalf("shape: %d %s", sh.StatusCode, sh.Body)
	}
	listedSh := doJSON(t, h, http.MethodGet, "/v1/shapes?package=test", nil, nil)
	if listedSh.StatusCode != http.StatusOK {
		t.Fatalf("list shapes: %d %s", listedSh.StatusCode, listedSh.Body)
	}

	gotEnt := doJSON(t, h, http.MethodGet, "/v1/entities/"+ownerID, nil, nil)
	var ent map[string]any
	_ = json.Unmarshal([]byte(gotEnt.Body), &ent)
	if _, ok := ent["effectiveClasses"]; ok {
		// owner has no instanceOf; may be empty/omitted
	}

	stmts := doJSON(t, h, http.MethodGet, "/v1/entities/"+ownerID+"/statements?property="+pidPet, nil, nil)
	_ = json.Unmarshal([]byte(stmts.Body), &incBody)
	if len(incBody.Statements) != 1 {
		t.Fatalf("filter statements: %s", stmts.Body)
	}

	gotProp := doJSON(t, h, http.MethodGet, "/v1/properties/"+pidPet, nil, nil)
	var prop map[string]any
	_ = json.Unmarshal([]byte(gotProp.Body), &prop)
	if prop["iriLocal"] != "pet" {
		t.Fatalf("property iriLocal: %s", gotProp.Body)
	}

	depPkg := doJSON(t, h, http.MethodPost, "/v1/packages", map[string]any{
		"code": "dep-sys", "lifecycle": "continuous",
		"labels": map[string]string{"en": "sys"},
		"dependencies": []map[string]string{{"dependsOnCode": "test", "versionRange": "*"}},
	}, nil)
	if depPkg.StatusCode != http.StatusCreated {
		t.Fatalf("package deps camelCase: %d %s", depPkg.StatusCode, depPkg.Body)
	}
}
