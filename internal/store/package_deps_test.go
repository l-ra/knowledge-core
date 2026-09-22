package store

import (
	"testing"

	"github.com/l-ra/knowledge-core/internal/domain"
)

func TestNormalizePackageDependencies(t *testing.T) {
	t.Parallel()
	got, err := normalizePackageDependencies("org", []domain.PackageDependency{
		{DependsOnCode: "b", VersionRange: "^1.0.0"},
		{DependsOnCode: "a", VersionRange: "^2.0.0"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].DependsOnCode != "a" || got[1].DependsOnCode != "b" {
		t.Fatalf("sorted deps: %+v", got)
	}
	if _, err := normalizePackageDependencies("org", []domain.PackageDependency{
		{DependsOnCode: "org", VersionRange: "^1.0.0"},
	}); err == nil {
		t.Fatal("expected self-dep error")
	}
	if _, err := normalizePackageDependencies("org", []domain.PackageDependency{
		{DependsOnCode: "a", VersionRange: "^1.0.0"},
		{DependsOnCode: "a", VersionRange: "^2.0.0"},
	}); err == nil {
		t.Fatal("expected duplicate error")
	}
}

func TestDiffPackageDependencies(t *testing.T) {
	t.Parallel()
	before := []domain.PackageDependency{
		{DependsOnCode: "keep", VersionRange: "^1.0.0"},
		{DependsOnCode: "drop", VersionRange: "^1.0.0"},
	}
	after := []domain.PackageDependency{
		{DependsOnCode: "keep", VersionRange: "^1.0.0"},
		{DependsOnCode: "add", VersionRange: "^2.0.0"},
	}
	rec := diffPackageDependencies(before, after)
	if len(rec.Kept) != 1 || rec.Kept[0].DependsOnCode != "keep" {
		t.Fatalf("kept: %+v", rec.Kept)
	}
	if len(rec.Added) != 1 || rec.Added[0].DependsOnCode != "add" {
		t.Fatalf("added: %+v", rec.Added)
	}
	if len(rec.Removed) != 1 || rec.Removed[0].DependsOnCode != "drop" {
		t.Fatalf("removed: %+v", rec.Removed)
	}
}
