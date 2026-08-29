package resolve

import (
	"testing"

	"github.com/deatherick/cartograph/internal/model"
)

func entity(repo string, kind model.Kind, qualified, name string) model.Entity {
	return model.Entity{
		ID:        model.NewEntityID(repo, kind, qualified, ""),
		Kind:      kind,
		Repo:      repo,
		Qualified: qualified,
		Name:      name,
	}
}

func TestResolve_SameFile(t *testing.T) {
	idx := NewIndex("repo")
	fn := entity("repo", model.KindFunction, "a.ts#helper", "helper")
	facts := &model.FileFacts{
		File:     "a.ts",
		Entities: []model.Entity{fn},
		Refs: []model.Ref{
			{Kind: model.RefCall, Target: model.RefTarget{Scope: model.ScopeUnqualified, Name: "helper"}},
		},
	}
	idx.AddFile(facts)

	got := idx.Resolve([]*model.FileFacts{facts})
	if len(got) != 1 {
		t.Fatalf("expected 1 resolved ref, got %d", len(got))
	}
	if got[0].Disposition != model.DispositionResolved {
		t.Fatalf("expected Resolved, got %s (%s)", got[0].Disposition, got[0].Reason)
	}
	if got[0].Edge.Dst != fn.ID {
		t.Fatalf("resolved to wrong entity: got %s want %s", got[0].Edge.Dst, fn.ID)
	}
	if got[0].Edge.Provenance != model.ProvenanceDeterministic {
		t.Fatalf("expected deterministic provenance, got %s", got[0].Edge.Provenance)
	}
}

func TestResolve_ImportTable(t *testing.T) {
	idx := NewIndex("repo")

	repoEntity := entity("repo", model.KindClass, "repositories/userRepository.ts#UserRepository", "UserRepository")
	repoFacts := &model.FileFacts{File: "repositories/userRepository.ts", Entities: []model.Entity{repoEntity}}

	svcFacts := &model.FileFacts{
		File: "services/userService.ts",
		Imports: []model.ImportBinding{
			{LocalName: "UserRepository", Source: "../repositories/userRepository", ImportedName: "UserRepository"},
		},
		Refs: []model.Ref{
			{Kind: model.RefTypeUse, Target: model.RefTarget{Scope: model.ScopeUnqualified, Name: "UserRepository"}},
		},
	}

	idx.AddFile(repoFacts)
	idx.AddFile(svcFacts)

	got := idx.Resolve([]*model.FileFacts{repoFacts, svcFacts})
	var resolved *model.ResolvedRef
	for i := range got {
		if got[i].Disposition == model.DispositionResolved {
			resolved = &got[i]
		}
	}
	if resolved == nil {
		t.Fatalf("expected a resolved ref via import table, got: %+v", got)
	}
	if resolved.Edge.Dst != repoEntity.ID {
		t.Fatalf("resolved to wrong entity: got %s want %s", resolved.Edge.Dst, repoEntity.ID)
	}
}

func TestResolve_DanglingRepoRelativeImport_IsBugExtractor(t *testing.T) {
	idx := NewIndex("repo")
	facts := &model.FileFacts{
		File:    "services/userService.ts",
		Imports: []model.ImportBinding{{LocalName: "Missing", Source: "../does/not/exist", ImportedName: "Missing"}},
		Refs: []model.Ref{
			{Kind: model.RefCall, Target: model.RefTarget{Scope: model.ScopeUnqualified, Name: "Missing"}},
		},
	}
	idx.AddFile(facts)
	got := idx.Resolve([]*model.FileFacts{facts})
	if len(got) != 1 || got[0].Disposition != model.DispositionBugExtractor {
		t.Fatalf("expected DispositionBugExtractor for a dangling repo-relative import, got %+v", got)
	}
	if !got[0].Disposition.IsBug() {
		t.Fatal("DispositionBugExtractor must count toward bug_rate")
	}
}

func TestResolve_KnownExternalPackage_IsNotABug(t *testing.T) {
	idx := NewIndex("repo")
	facts := &model.FileFacts{
		File:    "app.ts",
		Imports: []model.ImportBinding{{LocalName: "express", Source: "express", IsDefault: true}},
		Refs: []model.Ref{
			{Kind: model.RefCall, Target: model.RefTarget{Scope: model.ScopeUnqualified, Name: "express"}},
		},
	}
	idx.AddFile(facts)
	got := idx.Resolve([]*model.FileFacts{facts})
	if len(got) != 1 || got[0].Disposition != model.DispositionExternalKnown {
		t.Fatalf("expected DispositionExternalKnown for a known npm package, got %+v", got)
	}
	if got[0].Disposition.IsBug() {
		t.Fatal("a known external package must not count toward bug_rate")
	}
}

func TestResolve_KnownGlobal_IsNotABug(t *testing.T) {
	idx := NewIndex("repo")
	facts := &model.FileFacts{
		File: "app.ts",
		Refs: []model.Ref{
			{Kind: model.RefCall, Target: model.RefTarget{Scope: model.ScopeUnqualified, Name: "console"}},
		},
	}
	idx.AddFile(facts)
	got := idx.Resolve([]*model.FileFacts{facts})
	if len(got) != 1 || got[0].Disposition != model.DispositionExternalKnown {
		t.Fatalf("expected console to resolve as a known global, got %+v", got)
	}
}

func TestResolve_GenericBareNameWithCandidates_IsAmbiguousNotAutoResolved(t *testing.T) {
	idx := NewIndex("repo")
	getterA := entity("repo", model.KindFunction, "a.ts#get", "get")
	facts := &model.FileFacts{
		File:     "b.ts",
		Refs: []model.Ref{
			{Kind: model.RefCall, Target: model.RefTarget{Scope: model.ScopeUnqualified, Name: "get"}},
		},
	}
	otherFacts := &model.FileFacts{File: "a.ts", Entities: []model.Entity{getterA}}
	idx.AddFile(otherFacts)
	idx.AddFile(facts)

	got := idx.Resolve([]*model.FileFacts{otherFacts, facts})
	// Only facts (b.ts) has a ref; find it.
	var r model.ResolvedRef
	for _, x := range got {
		if x.Reason != "" || x.Disposition == model.DispositionAmbiguous {
			r = x
		}
	}
	if r.Disposition != model.DispositionAmbiguous {
		t.Fatalf("expected a generic bare name with a real candidate to be Ambiguous (never auto-bound), got %+v", got)
	}
	if len(r.Candidates) != 1 || r.Candidates[0] != getterA.ID {
		t.Fatalf("expected the candidate list to include the real match, got %+v", r.Candidates)
	}
}

func TestResolve_QualifiedThroughLocalVar_IsUnimplementedNotABug(t *testing.T) {
	idx := NewIndex("repo")
	facts := &model.FileFacts{
		File: "svc.ts",
		Refs: []model.Ref{
			{Kind: model.RefCall, Target: model.RefTarget{Scope: model.ScopeQualified, Name: "repo", Member: "findByEmail"}},
		},
	}
	idx.AddFile(facts)
	got := idx.Resolve([]*model.FileFacts{facts})
	if len(got) != 1 || got[0].Disposition != model.DispositionUnimplemented {
		t.Fatalf("expected DispositionUnimplemented for receiver-type gap, got %+v", got)
	}
	if got[0].Disposition.IsBug() {
		t.Fatal("a documented scope gap must not count toward bug_rate")
	}
}
