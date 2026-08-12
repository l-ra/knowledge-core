package store

import (
	"context"
	"fmt"
	"strings"

	"github.com/l-ra/knowledge-core/internal/datatype"
	"github.com/l-ra/knowledge-core/internal/domain"
	"github.com/l-ra/knowledge-core/internal/rdf"
)

func (s *Store) AnalyzeRDF(ctx context.Context, ntriples, turtlePrefixes string) (*domain.RDFAnalyzeResult, error) {
	out := &domain.RDFAnalyzeResult{}
	var triples []rdf.Triple
	if strings.TrimSpace(ntriples) != "" {
		parsed, err := rdf.ParseNTriples(ntriples)
		if err != nil {
			out.Errors = []string{err.Error()}
			return out, nil
		}
		triples = parsed
		for _, tr := range triples {
			if tr.Subject.Kind == rdf.TermBlank || tr.Object.Kind == rdf.TermBlank {
				out.Errors = append(out.Errors, fmt.Sprintf("line %d: blank nodes are not supported in v1", tr.Line))
			}
		}
		if len(out.Errors) > 0 {
			return out, nil
		}
	}

	pkgs, err := s.ListPackages(ctx)
	if err != nil {
		return nil, err
	}
	for _, p := range pkgs {
		label := ""
		if p.Labels != nil {
			label = p.Labels["en"]
		}
		out.Packages = append(out.Packages, domain.PackageBrief{
			Code: p.Code, IRIBase: p.IRIBase, Label: label,
		})
	}

	byBase := map[string]domain.Package{}
	byCode := map[string]domain.Package{}
	for _, p := range pkgs {
		byCode[p.Code] = p
		if p.IRIBase != "" {
			byBase[p.IRIBase] = p
		}
	}

	enrich := func(item *domain.RDFPrefixCandidate) {
		if p, ok := byBase[item.IRIBase]; ok {
			item.SuggestedAction = "use"
			item.ExistingCode = p.Code
			item.ExistingIRIBase = p.IRIBase
			item.SuggestedCode = p.Code
			return
		}
		item.SuggestedAction = "create"
		if p, ok := byCode[item.SuggestedCode]; ok && p.IRIBase == "" {
			item.SuggestedAction = "update"
			item.ExistingCode = p.Code
			item.SuggestedCode = p.Code
			return
		}
		code := item.SuggestedCode
		for i := 2; ; i++ {
			if _, exists := byCode[code]; !exists {
				break
			}
			code = fmt.Sprintf("%s-%d", item.SuggestedCode, i)
			if i > 50 {
				break
			}
		}
		item.SuggestedCode = code
	}

	seen := map[string]int{} // iriBase → index in out.Candidates

	if len(triples) > 0 {
		cands := rdf.DiscoverPrefixes(triples)
		for _, c := range cands {
			item := domain.RDFPrefixCandidate{
				IRIBase: c.IRIBase, SuggestedCode: c.SuggestedCode,
				IRICount: c.IRICount, TripleCount: c.TripleCount, SampleIRIs: c.SampleIRIs,
				Source: "detected",
			}
			enrich(&item)
			seen[item.IRIBase] = len(out.Candidates)
			out.Candidates = append(out.Candidates, item)
		}
	}

	if strings.TrimSpace(turtlePrefixes) != "" {
		decls, warns, err := rdf.ParseTurtlePrefixes(turtlePrefixes)
		out.Warnings = append(out.Warnings, warns...)
		if err != nil {
			out.Warnings = append(out.Warnings, err.Error())
		}
		for _, d := range decls {
			codeHint := d.Prefix
			if codeHint == "" {
				codeHint = rdf.SuggestPackageCode(d.IRIBase)
			} else {
				codeHint = strings.ToLower(strings.ReplaceAll(codeHint, "_", "-"))
			}
			if idx, ok := seen[d.IRIBase]; ok {
				out.Candidates[idx].TurtlePrefix = d.Prefix
				if out.Candidates[idx].Source == "detected" {
					out.Candidates[idx].Source = "detected+turtle"
				}
				continue
			}
			item := domain.RDFPrefixCandidate{
				IRIBase: d.IRIBase, SuggestedCode: codeHint,
				Source: "turtle", TurtlePrefix: d.Prefix,
			}
			enrich(&item)
			seen[item.IRIBase] = len(out.Candidates)
			out.Candidates = append(out.Candidates, item)
		}
	}

	if len(out.Candidates) == 0 && len(out.Errors) == 0 {
		out.Warnings = append(out.Warnings, "no prefix candidates (provide N-Triples and/or @prefix header)")
	}

	if len(triples) > 0 {
		bases := make([]string, 0, len(out.Candidates))
		for _, c := range out.Candidates {
			bases = append(bases, c.IRIBase)
		}
		for _, tr := range triples {
			if tr.Subject.Kind != rdf.TermIRI {
				continue
			}
			if rdf.IsWellKnownVocabIRI(tr.Subject.Value) {
				continue
			}
			if rdf.LongestMatchingBase(tr.Subject.Value, bases) == "" {
				out.Warnings = append(out.Warnings, fmt.Sprintf("line %d: subject not under any discovered prefix: %s", tr.Line, tr.Subject.Value))
			}
		}
	}
	return out, nil
}

func (s *Store) ParseTurtlePrefixes(_ context.Context, turtlePrefixes string) (*domain.RDFTurtlePrefixResult, error) {
	out := &domain.RDFTurtlePrefixResult{}
	decls, warns, err := rdf.ParseTurtlePrefixes(turtlePrefixes)
	out.Warnings = warns
	if err != nil {
		out.Errors = []string{err.Error()}
		return out, nil
	}
	for _, d := range decls {
		code := d.Prefix
		if code == "" {
			code = rdf.SuggestPackageCode(d.IRIBase)
		} else {
			code = strings.ToLower(strings.ReplaceAll(code, "_", "-"))
		}
		out.Prefixes = append(out.Prefixes, domain.RDFTurtlePrefix{
			Prefix: d.Prefix, IRIBase: d.IRIBase, SuggestedCode: code,
		})
	}
	return out, nil
}

func (s *Store) ImportRDFGlobal(ctx context.Context, meta domain.WriteMeta, in domain.RDFGlobalImportInput) (*domain.RDFGlobalImportResult, error) {
	out := &domain.RDFGlobalImportResult{DryRun: in.DryRun}
	if strings.TrimSpace(in.NTriples) == "" {
		out.Errors = []string{"ntriples required"}
		return out, nil
	}
	if len(in.Assignments) == 0 {
		out.Errors = []string{"assignments required"}
		return out, nil
	}

	triples, err := rdf.ParseNTriples(in.NTriples)
	if err != nil {
		out.Errors = []string{err.Error()}
		return out, nil
	}
	for _, tr := range triples {
		if tr.Subject.Kind == rdf.TermBlank || tr.Object.Kind == rdf.TermBlank {
			out.Errors = append(out.Errors, fmt.Sprintf("line %d: blank nodes are not supported in v1", tr.Line))
		}
	}
	if len(out.Errors) > 0 {
		return out, nil
	}

	bases := make([]string, 0, len(in.Assignments))
	seenBase := map[string]bool{}
	seenCode := map[string]bool{}
	for i, a := range in.Assignments {
		base, err := datatype.NormalizeIRIBase(a.IRIBase)
		if err != nil {
			out.Errors = append(out.Errors, fmt.Sprintf("assignment[%d] iriBase: %v", i, err))
			continue
		}
		a.IRIBase = base
		in.Assignments[i] = a
		if base == "" {
			out.Errors = append(out.Errors, fmt.Sprintf("assignment[%d]: iriBase required", i))
			continue
		}
		if strings.TrimSpace(a.PackageCode) == "" {
			out.Errors = append(out.Errors, fmt.Sprintf("assignment[%d]: packageCode required", i))
			continue
		}
		if seenBase[base] {
			out.Errors = append(out.Errors, fmt.Sprintf("duplicate iriBase %s", base))
		}
		seenBase[base] = true
		if seenCode[a.PackageCode] {
			out.Errors = append(out.Errors, fmt.Sprintf("duplicate packageCode %s", a.PackageCode))
		}
		seenCode[a.PackageCode] = true
		bases = append(bases, base)
	}
	if len(out.Errors) > 0 {
		return out, nil
	}

	// Ensure packages exist / iri_base set (unless dry-run — then only simulate).
	for _, a := range in.Assignments {
		pr := domain.RDFGlobalPackageResult{
			IRIBase: a.IRIBase, PackageCode: a.PackageCode,
		}
		existing, err := s.GetPackageByCode(ctx, a.PackageCode)
		if err != nil && !isNotFound(err) {
			return nil, err
		}
		exists := err == nil && existing != nil

		switch {
		case !exists && a.Create:
			pr.Action = "create"
			if !in.DryRun {
				label := a.Label
				if label == "" {
					label = a.PackageCode
				}
				_, err := s.CreatePackage(ctx, meta, domain.CreatePackageInput{
					Code: a.PackageCode, Lifecycle: domain.PackageReleased,
					IRIBase: a.IRIBase, Labels: map[string]string{"en": label},
				})
				if err != nil {
					pr.Error = err.Error()
					out.Packages = append(out.Packages, pr)
					continue
				}
			}
		case !exists && !a.Create:
			pr.Action = "skip"
			pr.Error = "package does not exist (create=false)"
			out.Packages = append(out.Packages, pr)
			continue
		case exists && (a.SetIRIBase || existing.IRIBase == "" || existing.IRIBase != a.IRIBase):
			if existing.IRIBase == a.IRIBase {
				pr.Action = "use"
			} else {
				pr.Action = "update"
				if !in.DryRun && a.SetIRIBase {
					base := a.IRIBase
					_, err := s.UpdatePackage(ctx, meta, a.PackageCode, domain.UpdatePackageInput{IRIBase: &base})
					if err != nil {
						pr.Error = err.Error()
						out.Packages = append(out.Packages, pr)
						continue
					}
				} else if !in.DryRun && existing.IRIBase != "" && existing.IRIBase != a.IRIBase && !a.SetIRIBase {
					out.Warnings = append(out.Warnings, fmt.Sprintf(
						"package %s keeps iriBase %q (assignment %q); set setIriBase to overwrite",
						a.PackageCode, existing.IRIBase, a.IRIBase))
					pr.Action = "use"
				} else if in.DryRun && a.SetIRIBase && existing.IRIBase != a.IRIBase {
					pr.Action = "update"
				} else {
					pr.Action = "use"
				}
			}
		default:
			pr.Action = "use"
		}

		subset := rdf.FilterTriplesBySubjectBase(triples, a.IRIBase, bases)
		if len(subset) == 0 {
			out.Warnings = append(out.Warnings, fmt.Sprintf("no triples for %s (%s)", a.PackageCode, a.IRIBase))
			out.Packages = append(out.Packages, pr)
			continue
		}
		nt := rdf.SerializeNTriples(subset)

		// Dry-run for packages that do not exist yet: plan without writing.
		if in.DryRun && !exists && a.Create {
			pkg := &domain.Package{Code: a.PackageCode, IRIBase: a.IRIBase}
			plan, err := s.buildRDFImportPlan(ctx, pkg, subset)
			if err != nil {
				pr.Error = err.Error()
			} else {
				plan.Result.DryRun = true
				pr.Import = &plan.Result
			}
			out.Packages = append(out.Packages, pr)
			continue
		}

		imp, err := s.ImportRDF(ctx, meta, a.PackageCode, domain.RDFImportInput{
			NTriples: nt, DryRun: in.DryRun,
		})
		if err != nil {
			pr.Error = err.Error()
		} else {
			pr.Import = imp
			if len(imp.Errors) > 0 {
				pr.Error = strings.Join(imp.Errors, "; ")
			}
		}
		out.Packages = append(out.Packages, pr)
	}
	return out, nil
}

func isNotFound(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "no rows") || strings.Contains(s, "not found")
}
