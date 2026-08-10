package apihttp_test

import (
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
)

func TestConcurrencyOptimisticLock(t *testing.T) {
	h := setupTestHandler(t)
	qid := createEntity(t, h, "Concurrent Entity")
	pid := createProperty(t, h, "value")
	sid := createStatement(t, h, qid, pid, "v1")

	const workers = 8
	var wg sync.WaitGroup
	var conflicts atomic.Int32
	var successes atomic.Int32

	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func(i int) {
			defer wg.Done()
			res := doJSON(t, h, http.MethodPost, "/v1/statements/"+sid+"/revise", map[string]any{
				"expectedRevision": 1,
				"value":            map[string]any{"type": "String", "string": "race"},
			}, nil)
			switch res.StatusCode {
			case http.StatusOK:
				successes.Add(1)
			case http.StatusConflict:
				conflicts.Add(1)
			default:
				t.Errorf("unexpected status %d: %s", res.StatusCode, res.Body)
			}
		}(i)
	}
	wg.Wait()

	if successes.Load() != 1 {
		t.Fatalf("expected exactly 1 success, got %d successes and %d conflicts", successes.Load(), conflicts.Load())
	}
	if conflicts.Load() != workers-1 {
		t.Fatalf("expected %d conflicts, got %d", workers-1, conflicts.Load())
	}
}
