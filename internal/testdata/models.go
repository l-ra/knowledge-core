package testdata

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// ModelsRoot returns the knowledge-models checkout used by tests.
// Resolution order:
//  1. KNOWLEDGE_MODELS_PATH
//  2. ../knowledge-models relative to the knowledge-core repo root
func ModelsRoot() (string, error) {
	if p := os.Getenv("KNOWLEDGE_MODELS_PATH"); p != "" {
		abs, err := filepath.Abs(p)
		if err != nil {
			return "", err
		}
		if err := assertModelsRoot(abs); err != nil {
			return "", err
		}
		return abs, nil
	}
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "", fmt.Errorf("testdata: runtime.Caller failed")
	}
	// internal/testdata -> repo root
	kcRoot := filepath.Clean(filepath.Join(filepath.Dir(file), "../.."))
	sibling := filepath.Clean(filepath.Join(kcRoot, "..", "knowledge-models"))
	if err := assertModelsRoot(sibling); err != nil {
		return "", fmt.Errorf("%w (set KNOWLEDGE_MODELS_PATH or checkout sibling ../knowledge-models)", err)
	}
	return sibling, nil
}

// MustModelsRoot is ModelsRoot for tests that call t.Fatal on error.
func MustModelsRoot(fatalf func(string, ...any)) string {
	root, err := ModelsRoot()
	if err != nil {
		fatalf("%v", err)
	}
	return root
}

func assertModelsRoot(root string) error {
	marker := filepath.Join(root, "kc-base", "releases", "kc-base-1.1.0.bundle.json")
	if _, err := os.Stat(marker); err != nil {
		return fmt.Errorf("knowledge-models not found at %s: %w", root, err)
	}
	return nil
}

// BundlePath joins ModelsRoot with a path relative to the models repo
// (e.g. "kc-base/releases/kc-base-1.1.0.bundle.json").
func BundlePath(rel string) (string, error) {
	root, err := ModelsRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, filepath.FromSlash(rel)), nil
}
