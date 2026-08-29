package resolve

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/deatherick/cartograph/internal/model"
)

// TestArchitectureBoundary_CoreNeverBranchesOnLang enforces the "plug and
// play, no bidirectional dependency between languages" design resolve.go's
// package doc claims: the core pipeline file must never mention a specific
// model.Lang value by name. Every per-language decision belongs in that
// language's own lang_*.go file (lang_ts.go, lang_go.go), reached only
// through the LanguagePolicy interface — this is a textual check, matching
// exactly the kind of grep internal/parser/architecture_test.go already
// uses for its own boundary (no tree-sitter type outside internal/parser).
func TestArchitectureBoundary_CoreNeverBranchesOnLang(t *testing.T) {
	content, err := os.ReadFile(filepath.Join(".", "resolve.go"))
	if err != nil {
		t.Fatalf("reading resolve.go: %v", err)
	}
	for _, needle := range []string{"model.LangTS", "model.LangGo"} {
		if strings.Contains(string(content), needle) {
			t.Errorf("resolve.go (the core pipeline) mentions %q directly — per-language logic belongs in a lang_*.go file, reached only through LanguagePolicy", needle)
		}
	}
}

func entity(kind model.Kind, qualified, name string) model.Entity {
	const repo = "repo"
	return model.Entity{
		ID:        model.NewEntityID(repo, kind, qualified, ""),
		Kind:      kind,
		Lang:      model.LangTS,
		Repo:      repo,
		Qualified: qualified,
		Name:      name,
	}
}

// tsIndex builds a resolver Index with the TypeScript policy registered —
// every test below exercises TS-specific tiers, which (per langpolicy.go's
// "plug and play" design) only fire when a language is explicitly
// registered, exactly as a real caller (internal/index) must do.
func tsIndex() *Index {
	idx := NewIndex("repo")
	idx.RegisterPolicy(NewTSPolicy(TSConfig{}))
	return idx
}

func TestResolve_SameFile(t *testing.T) {
	idx := tsIndex()
	fn := entity(model.KindFunction, "a.ts#helper", "helper")
	facts := &model.FileFacts{
		Lang:     model.LangTS,
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
	idx := tsIndex()

	repoEntity := entity(model.KindClass, "repositories/userRepository.ts#UserRepository", "UserRepository")
	repoFacts := &model.FileFacts{Lang: model.LangTS, File: "repositories/userRepository.ts", Entities: []model.Entity{repoEntity}}

	svcFacts := &model.FileFacts{
		Lang: model.LangTS,
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
	idx := tsIndex()
	facts := &model.FileFacts{
		Lang:    model.LangTS,
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
	idx := tsIndex()
	facts := &model.FileFacts{
		Lang:    model.LangTS,
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
	idx := tsIndex()
	facts := &model.FileFacts{
		Lang: model.LangTS,
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
	idx := tsIndex()
	getterA := entity(model.KindFunction, "a.ts#get", "get")
	facts := &model.FileFacts{
		Lang: model.LangTS,
		File: "b.ts",
		Refs: []model.Ref{
			{Kind: model.RefCall, Target: model.RefTarget{Scope: model.ScopeUnqualified, Name: "get"}},
		},
	}
	otherFacts := &model.FileFacts{Lang: model.LangTS, File: "a.ts", Entities: []model.Entity{getterA}}
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
	idx := tsIndex()
	facts := &model.FileFacts{
		Lang: model.LangTS,
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

func TestResolve_ReceiverType_SameFile(t *testing.T) {
	idx := tsIndex()
	svc := entity(model.KindClass, "svc.ts#UserService", "UserService")
	method := entity(model.KindMethod, "svc.ts#UserService.register", "register")
	caller := entity(model.KindMethod, "svc.ts#Caller.run", "run")

	facts := &model.FileFacts{
		Lang:     model.LangTS,
		File:     "svc.ts",
		Entities: []model.Entity{svc, method, caller},
		Refs: []model.Ref{
			{
				Kind: model.RefCall, Src: caller.ID,
				Target: model.RefTarget{Scope: model.ScopeQualified, Name: "userService", Member: "register", ReceiverType: "UserService"},
			},
		},
	}
	idx.AddFile(facts)
	got := idx.Resolve([]*model.FileFacts{facts})
	if len(got) != 1 || got[0].Disposition != model.DispositionResolved {
		t.Fatalf("expected receiver-type resolution to Resolved, got %+v", got)
	}
	if got[0].Edge.Dst != method.ID {
		t.Fatalf("resolved to wrong entity: got %s want %s", got[0].Edge.Dst, method.ID)
	}
	if got[0].Edge.Src != caller.ID {
		t.Fatalf("expected edge Src to be the calling entity, got %s", got[0].Edge.Src)
	}
	if got[0].Edge.Provenance != model.ProvenanceInferred {
		t.Fatalf("expected receiver-type edges to be Inferred provenance, got %s", got[0].Edge.Provenance)
	}
}

func TestResolve_ReceiverType_CrossFileViaImport(t *testing.T) {
	idx := tsIndex()
	repoClass := entity(model.KindClass, "repositories/userRepository.ts#UserRepository", "UserRepository")
	repoMethod := entity(model.KindMethod, "repositories/userRepository.ts#UserRepository.findByEmail", "findByEmail")
	repoFacts := &model.FileFacts{Lang: model.LangTS, File: "repositories/userRepository.ts", Entities: []model.Entity{repoClass, repoMethod}}

	svcFacts := &model.FileFacts{
		Lang: model.LangTS,
		File: "services/userService.ts",
		Imports: []model.ImportBinding{
			{LocalName: "UserRepository", Source: "../repositories/userRepository", ImportedName: "UserRepository"},
		},
		Refs: []model.Ref{
			{
				Kind:   model.RefCall,
				Target: model.RefTarget{Scope: model.ScopeQualified, Name: "repo", Member: "findByEmail", ReceiverType: "UserRepository"},
			},
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
		t.Fatalf("expected a receiver-type ref to resolve cross-file, got: %+v", got)
	}
	if resolved.Edge.Dst != repoMethod.ID {
		t.Fatalf("resolved to wrong entity: got %s want %s", resolved.Edge.Dst, repoMethod.ID)
	}
}

func TestResolve_ReceiverType_ImportedNameUsedAsStaticReceiver(t *testing.T) {
	// `User.findById(...)` — User is a plain (non-namespace) import used
	// directly as the call receiver, the dominant pattern in the real
	// repo this was validated against (Mongoose model static-style calls).
	idx := tsIndex()
	userModel := entity(model.KindClass, "models/user.ts#User", "User")
	userFacts := &model.FileFacts{Lang: model.LangTS, File: "models/user.ts", Entities: []model.Entity{userModel}}

	routeFacts := &model.FileFacts{
		Lang:    model.LangTS,
		File:    "routes/users.ts",
		Imports: []model.ImportBinding{{LocalName: "User", Source: "../models/user", ImportedName: "User"}},
		Refs: []model.Ref{
			{Kind: model.RefCall, Target: model.RefTarget{Scope: model.ScopeQualified, Name: "User", Member: "findById"}},
		},
	}
	idx.AddFile(userFacts)
	idx.AddFile(routeFacts)

	got := idx.Resolve([]*model.FileFacts{userFacts, routeFacts})
	if len(got) != 1 {
		t.Fatalf("expected 1 resolved ref, got %d", len(got))
	}
	// findById is not defined on User in this fixture (it would come from
	// an unindexed base class in real Mongoose code) — must be
	// ExternalUnknown, specifically NOT Unimplemented, since the resolver
	// DID determine the receiver type and made a real decision.
	if got[0].Disposition != model.DispositionExternalUnknown {
		t.Fatalf("expected ExternalUnknown for a known type with an unindexed member, got %+v", got[0])
	}
}

func TestResolve_ReceiverType_UnknownType_StaysUnimplemented(t *testing.T) {
	idx := tsIndex()
	facts := &model.FileFacts{
		Lang: model.LangTS,
		File: "svc.ts",
		Refs: []model.Ref{
			{Kind: model.RefCall, Target: model.RefTarget{Scope: model.ScopeQualified, Name: "thing", Member: "doStuff"}},
		},
	}
	idx.AddFile(facts)
	got := idx.Resolve([]*model.FileFacts{facts})
	if len(got) != 1 || got[0].Disposition != model.DispositionUnimplemented {
		t.Fatalf("expected DispositionUnimplemented when the receiver type is genuinely unknown, got %+v", got)
	}
	if got[0].Disposition.IsBug() {
		t.Fatal("a genuinely unknown receiver type must not count toward bug_rate")
	}
}

func TestResolve_TSConfigPathAlias(t *testing.T) {
	idx := NewIndex("repo")
	idx.RegisterPolicy(NewTSPolicy(TSConfig{BaseURL: ".", Paths: map[string][]string{"@/*": {"src/*"}}}))

	svc := entity(model.KindClass, "src/services/userService.ts#UserService", "UserService")
	svcFacts := &model.FileFacts{Lang: model.LangTS, File: "src/services/userService.ts", Entities: []model.Entity{svc}}

	callerFacts := &model.FileFacts{
		Lang:    model.LangTS,
		File:    "src/app.ts",
		Imports: []model.ImportBinding{{LocalName: "UserService", Source: "@/services/userService", ImportedName: "UserService"}},
		Refs: []model.Ref{
			{Kind: model.RefTypeUse, Target: model.RefTarget{Scope: model.ScopeUnqualified, Name: "UserService"}},
		},
	}
	idx.AddFile(svcFacts)
	idx.AddFile(callerFacts)

	got := idx.Resolve([]*model.FileFacts{svcFacts, callerFacts})
	var resolved *model.ResolvedRef
	for i := range got {
		if got[i].Disposition == model.DispositionResolved {
			resolved = &got[i]
		}
	}
	if resolved == nil {
		t.Fatalf("expected the @/ alias import to resolve, got: %+v", got)
	}
	if resolved.Edge.Dst != svc.ID {
		t.Fatalf("resolved to wrong entity: got %s want %s", resolved.Edge.Dst, svc.ID)
	}
}

func TestResolve_BarrelReExport_Star(t *testing.T) {
	idx := tsIndex()
	userModel := entity(model.KindClass, "models/user.ts#User", "User")
	userFacts := &model.FileFacts{Lang: model.LangTS, File: "models/user.ts", Entities: []model.Entity{userModel}}

	barrelFacts := &model.FileFacts{
		Lang:      model.LangTS,
		File:      "models/index.ts",
		ReExports: []model.ReExport{{Source: "./user", IsStar: true}},
	}

	callerFacts := &model.FileFacts{
		Lang:    model.LangTS,
		File:    "app.ts",
		Imports: []model.ImportBinding{{LocalName: "User", Source: "./models", ImportedName: "User"}},
		Refs: []model.Ref{
			{Kind: model.RefTypeUse, Target: model.RefTarget{Scope: model.ScopeUnqualified, Name: "User"}},
		},
	}
	idx.AddFile(userFacts)
	idx.AddFile(barrelFacts)
	idx.AddFile(callerFacts)

	got := idx.Resolve([]*model.FileFacts{userFacts, barrelFacts, callerFacts})
	var resolved *model.ResolvedRef
	for i := range got {
		if got[i].Disposition == model.DispositionResolved {
			resolved = &got[i]
		}
	}
	if resolved == nil {
		t.Fatalf("expected the import to resolve through the barrel's star re-export, got: %+v", got)
	}
	if resolved.Edge.Dst != userModel.ID {
		t.Fatalf("resolved to wrong entity: got %s want %s", resolved.Edge.Dst, userModel.ID)
	}
}

func TestResolve_BarrelReExport_NamedWithAlias(t *testing.T) {
	idx := tsIndex()
	orderModel := entity(model.KindClass, "models/order.ts#Order", "Order")
	orderFacts := &model.FileFacts{Lang: model.LangTS, File: "models/order.ts", Entities: []model.Entity{orderModel}}

	barrelFacts := &model.FileFacts{
		Lang:      model.LangTS,
		File:      "models/index.ts",
		ReExports: []model.ReExport{{Source: "./order", ExportedName: "Order", LocalAlias: "OrderModel"}},
	}

	callerFacts := &model.FileFacts{
		Lang:    model.LangTS,
		File:    "app.ts",
		Imports: []model.ImportBinding{{LocalName: "OrderModel", Source: "./models", ImportedName: "OrderModel"}},
		Refs: []model.Ref{
			{Kind: model.RefTypeUse, Target: model.RefTarget{Scope: model.ScopeUnqualified, Name: "OrderModel"}},
		},
	}
	idx.AddFile(orderFacts)
	idx.AddFile(barrelFacts)
	idx.AddFile(callerFacts)

	got := idx.Resolve([]*model.FileFacts{orderFacts, barrelFacts, callerFacts})
	var resolved *model.ResolvedRef
	for i := range got {
		if got[i].Disposition == model.DispositionResolved {
			resolved = &got[i]
		}
	}
	if resolved == nil {
		t.Fatalf("expected the import to resolve through the barrel's aliased named re-export, got: %+v", got)
	}
	if resolved.Edge.Dst != orderModel.ID {
		t.Fatalf("resolved to wrong entity: got %s want %s", resolved.Edge.Dst, orderModel.ID)
	}
}

func TestResolve_BarrelReExport_CycleIsSafe(t *testing.T) {
	idx := tsIndex()
	// a.ts re-exports everything from b.ts, and b.ts re-exports everything
	// from a.ts — a cycle that must not hang or stack-overflow.
	aFacts := &model.FileFacts{Lang: model.LangTS, File: "a.ts", ReExports: []model.ReExport{{Source: "./b", IsStar: true}}}
	bFacts := &model.FileFacts{Lang: model.LangTS, File: "b.ts", ReExports: []model.ReExport{{Source: "./a", IsStar: true}}}
	callerFacts := &model.FileFacts{
		Lang:    model.LangTS,
		File:    "app.ts",
		Imports: []model.ImportBinding{{LocalName: "Missing", Source: "./a", ImportedName: "Missing"}},
		Refs: []model.Ref{
			{Kind: model.RefTypeUse, Target: model.RefTarget{Scope: model.ScopeUnqualified, Name: "Missing"}},
		},
	}
	idx.AddFile(aFacts)
	idx.AddFile(bFacts)
	idx.AddFile(callerFacts)

	done := make(chan []model.ResolvedRef, 1)
	go func() { done <- idx.Resolve([]*model.FileFacts{aFacts, bFacts, callerFacts}) }()
	select {
	case got := <-done:
		if len(got) != 1 || got[0].Disposition != model.DispositionBugResolver {
			t.Fatalf("expected a clean BugResolver disposition for a re-export cycle with no real entity, got %+v", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Resolve did not return within 2s — likely an infinite loop in barrel re-export following")
	}
}

// goEntity mirrors entity() but sets Lang, which fileEntry.lang (and thus
// every Go-specific resolution branch) actually reads.
func goEntity(kind model.Kind, qualified, name string) model.Entity {
	const repo = "repo"
	return model.Entity{
		ID:        model.NewEntityID(repo, kind, qualified, ""),
		Kind:      kind,
		Lang:      model.LangGo,
		Repo:      repo,
		Qualified: qualified,
		Name:      name,
	}
}

// goIndex mirrors tsIndex for Go — every test below exercises Go-specific
// tiers, which only fire when Go's policy is explicitly registered.
func goIndex(modulePath string) *Index {
	idx := NewIndex("repo")
	idx.RegisterPolicy(NewGoPolicy(modulePath))
	return idx
}

func TestResolve_Go_SamePackageAcrossFiles(t *testing.T) {
	idx := goIndex("")
	// process is declared in a sibling file of the SAME package directory —
	// TypeScript has no equivalent tier (every file is its own module
	// there); Go's same-package tier is what makes this resolve.
	helperEnt := goEntity(model.KindFunction, "internal/svc#process", "process")
	helperFacts := &model.FileFacts{Lang: model.LangGo, File: "internal/svc/helper.go", Entities: []model.Entity{helperEnt}}
	callerFacts := &model.FileFacts{
		Lang: model.LangGo,
		File: "internal/svc/service.go",
		Refs: []model.Ref{
			{Kind: model.RefCall, Target: model.RefTarget{Scope: model.ScopeUnqualified, Name: "process"}},
		},
	}
	idx.AddFile(helperFacts)
	idx.AddFile(callerFacts)

	got := idx.Resolve([]*model.FileFacts{callerFacts})
	if len(got) != 1 || got[0].Disposition != model.DispositionResolved {
		t.Fatalf("expected same-package resolution, got %+v", got)
	}
	if got[0].Edge.Dst != helperEnt.ID {
		t.Fatalf("resolved to wrong entity: got %s want %s", got[0].Edge.Dst, helperEnt.ID)
	}
}

func TestResolve_Go_PackageQualifiedImport(t *testing.T) {
	idx := goIndex("example.com/m")

	repoEnt := goEntity(model.KindFunction, "internal/repo#Validate", "Validate")
	repoFacts := &model.FileFacts{Lang: model.LangGo, File: "internal/repo/repo.go", Entities: []model.Entity{repoEnt}}

	callerFacts := &model.FileFacts{
		Lang: model.LangGo,
		File: "internal/svc/service.go",
		Imports: []model.ImportBinding{
			{LocalName: "repo", Source: "example.com/m/internal/repo", IsNamespace: true},
		},
		Refs: []model.Ref{
			{Kind: model.RefCall, Target: model.RefTarget{Scope: model.ScopeQualified, Name: "repo", Member: "Validate"}},
		},
	}
	idx.AddFile(repoFacts)
	idx.AddFile(callerFacts)

	got := idx.Resolve([]*model.FileFacts{callerFacts})
	if len(got) != 1 || got[0].Disposition != model.DispositionResolved {
		t.Fatalf("expected package-qualified resolution, got %+v", got)
	}
	if got[0].Edge.Dst != repoEnt.ID {
		t.Fatalf("resolved to wrong entity: got %s want %s", got[0].Edge.Dst, repoEnt.ID)
	}
}

func TestResolve_Go_StdlibImport_IsExternalKnown_NotABug(t *testing.T) {
	idx := goIndex("example.com/m")

	callerFacts := &model.FileFacts{
		Lang: model.LangGo,
		File: "internal/svc/service.go",
		Imports: []model.ImportBinding{
			{LocalName: "fmt", Source: "fmt", IsNamespace: true},
		},
		Refs: []model.Ref{
			{Kind: model.RefCall, Target: model.RefTarget{Scope: model.ScopeQualified, Name: "fmt", Member: "Println"}},
		},
	}
	idx.AddFile(callerFacts)

	got := idx.Resolve([]*model.FileFacts{callerFacts})
	if len(got) != 1 || got[0].Disposition != model.DispositionExternalKnown {
		t.Fatalf("expected ExternalKnown for a stdlib import, got %+v", got)
	}
}

func TestResolve_Go_ReceiverTypeThroughStructField(t *testing.T) {
	idx := goIndex("")

	depEnt := goEntity(model.KindMethod, "internal/svc#Dep.Greeting", "Greeting")
	depFacts := &model.FileFacts{Lang: model.LangGo, File: "internal/svc/dep.go", Entities: []model.Entity{depEnt}}

	callerFacts := &model.FileFacts{
		Lang: model.LangGo,
		File: "internal/svc/service.go",
		Refs: []model.Ref{
			{Kind: model.RefCall, Target: model.RefTarget{Scope: model.ScopeQualified, Name: "dep", Member: "Greeting", ReceiverType: "Dep"}},
		},
	}
	idx.AddFile(depFacts)
	idx.AddFile(callerFacts)

	got := idx.Resolve([]*model.FileFacts{callerFacts})
	if len(got) != 1 || got[0].Disposition != model.DispositionResolved {
		t.Fatalf("expected receiver-type resolution, got %+v", got)
	}
	if got[0].Edge.Dst != depEnt.ID {
		t.Fatalf("resolved to wrong entity: got %s want %s", got[0].Edge.Dst, depEnt.ID)
	}
}

func TestResolve_Go_UnresolvedBareName_IsBugExtractorNotExternal(t *testing.T) {
	// Unlike TypeScript, Go has no implicit unqualified globals — a bare
	// call that isn't same-file, same-package, or a predeclared identifier
	// signals a missed extraction, not a presumed-external reference.
	idx := goIndex("")
	callerFacts := &model.FileFacts{
		Lang: model.LangGo,
		File: "internal/svc/service.go",
		Refs: []model.Ref{
			{Kind: model.RefCall, Target: model.RefTarget{Scope: model.ScopeUnqualified, Name: "totallyMissingHelper"}},
		},
	}
	idx.AddFile(callerFacts)

	got := idx.Resolve([]*model.FileFacts{callerFacts})
	if len(got) != 1 || got[0].Disposition != model.DispositionBugExtractor {
		t.Fatalf("expected BugExtractor, got %+v", got)
	}
}

func TestResolve_Go_PredeclaredIdentifier_IsNotABug(t *testing.T) {
	idx := goIndex("")
	callerFacts := &model.FileFacts{
		Lang: model.LangGo,
		File: "internal/svc/service.go",
		Refs: []model.Ref{
			{Kind: model.RefCall, Target: model.RefTarget{Scope: model.ScopeUnqualified, Name: "make"}},
		},
	}
	idx.AddFile(callerFacts)

	got := idx.Resolve([]*model.FileFacts{callerFacts})
	if len(got) != 1 || got[0].Disposition != model.DispositionExternalKnown {
		t.Fatalf("expected ExternalKnown for a Go predeclared identifier, got %+v", got)
	}
}

func TestResolve_UnregisteredLanguage_IsUnclassifiedNotGuessed(t *testing.T) {
	// A language with no registered policy (deliberately disabled for this
	// run, per NewIndex's doc) must never silently fall through to another
	// language's disposition rules — it gets a plain, honest
	// Unclassified, not a guess.
	idx := NewIndex("repo")
	facts := &model.FileFacts{
		Lang: model.Lang("python"),
		File: "app.py",
		Refs: []model.Ref{
			{Kind: model.RefCall, Target: model.RefTarget{Scope: model.ScopeUnqualified, Name: "helper"}},
		},
	}
	idx.AddFile(facts)
	got := idx.Resolve([]*model.FileFacts{facts})
	if len(got) != 1 || got[0].Disposition != model.DispositionUnclassified {
		t.Fatalf("expected Unclassified for an unregistered language, got %+v", got)
	}
	if got[0].Disposition.IsBug() {
		t.Fatal("a deliberately disabled language must not count toward bug_rate")
	}
}

// --- ADR-0020: incremental indexing support (RemoveFile, AddFile
// idempotency, Dependents) ---

func TestRemoveFile_PrunesByBareNameAndFilesByDir(t *testing.T) {
	idx := tsIndex()
	e := entity(model.KindFunction, "a.ts#helper", "helper")
	facts := &model.FileFacts{Lang: model.LangTS, File: "a.ts", Entities: []model.Entity{e}}
	idx.AddFile(facts)

	if len(idx.byBareName["helper"]) != 1 {
		t.Fatalf("expected helper indexed once before removal, got %v", idx.byBareName["helper"])
	}
	if len(idx.filesByDir["."]) != 1 {
		t.Fatalf("expected a.ts registered under its directory, got %v", idx.filesByDir["."])
	}

	idx.RemoveFile("a.ts")

	if len(idx.byBareName["helper"]) != 0 {
		t.Errorf("expected helper pruned from byBareName after RemoveFile, got %v", idx.byBareName["helper"])
	}
	if _, ok := idx.files["a.ts"]; ok {
		t.Error("expected a.ts removed from idx.files")
	}
	if len(idx.filesByDir["."]) != 0 {
		t.Errorf("expected a.ts pruned from filesByDir, got %v", idx.filesByDir["."])
	}
}

func TestRemoveFile_Unknown_IsNoop(t *testing.T) {
	idx := tsIndex()
	idx.RemoveFile("never-added.ts") // must not panic
}

func TestAddFile_ReAddingSameFile_DoesNotDuplicateOrLeaveStaleEntries(t *testing.T) {
	// A function renamed from "oldName" to "newName" within the same
	// file, re-extracted and re-added (the incremental update path) —
	// byBareName must reflect ONLY the new state: no lingering "oldName"
	// entry, and no duplicate entries from adding the same file twice.
	idx := tsIndex()
	before := entity(model.KindFunction, "a.ts#oldName", "oldName")
	idx.AddFile(&model.FileFacts{Lang: model.LangTS, File: "a.ts", Entities: []model.Entity{before}})

	after := entity(model.KindFunction, "a.ts#newName", "newName")
	idx.AddFile(&model.FileFacts{Lang: model.LangTS, File: "a.ts", Entities: []model.Entity{after}})

	if len(idx.byBareName["oldName"]) != 0 {
		t.Errorf("expected oldName pruned after the file was re-added without it, got %v", idx.byBareName["oldName"])
	}
	if got := idx.byBareName["newName"]; len(got) != 1 || got[0] != after.ID {
		t.Errorf("expected exactly one newName entry, got %v", got)
	}
	if len(idx.filesByDir["."]) != 1 {
		t.Errorf("expected a.ts registered exactly once in filesByDir even after being re-added, got %v", idx.filesByDir["."])
	}
}

func TestDependents_TS_FindsImporter(t *testing.T) {
	idx := tsIndex()
	repoFacts := &model.FileFacts{
		Lang: model.LangTS, File: "repositories/userRepository.ts",
		Entities: []model.Entity{entity(model.KindClass, "repositories/userRepository.ts#UserRepository", "UserRepository")},
	}
	svcFacts := &model.FileFacts{
		Lang: model.LangTS, File: "services/userService.ts",
		Imports: []model.ImportBinding{{LocalName: "UserRepository", Source: "../repositories/userRepository", ImportedName: "UserRepository"}},
	}
	unrelated := &model.FileFacts{Lang: model.LangTS, File: "unrelated.ts"}
	idx.AddFile(repoFacts)
	idx.AddFile(svcFacts)
	idx.AddFile(unrelated)

	deps := idx.Dependents("repositories/userRepository.ts")
	if len(deps) != 1 || deps[0] != "services/userService.ts" {
		t.Fatalf("expected Dependents to find exactly services/userService.ts, got %v", deps)
	}
}

func TestDependents_TS_BarrelReExportCounts(t *testing.T) {
	idx := tsIndex()
	targetFacts := &model.FileFacts{
		Lang: model.LangTS, File: "models/user.ts",
		Entities: []model.Entity{entity(model.KindClass, "models/user.ts#User", "User")},
	}
	barrelFacts := &model.FileFacts{
		Lang: model.LangTS, File: "models/index.ts",
		ReExports: []model.ReExport{{Source: "./user", IsStar: true}},
	}
	idx.AddFile(targetFacts)
	idx.AddFile(barrelFacts)

	deps := idx.Dependents("models/user.ts")
	if len(deps) != 1 || deps[0] != "models/index.ts" {
		t.Fatalf("expected the barrel file that re-exports it, got %v", deps)
	}
}

func TestDependents_TS_NoImporters_IsEmpty(t *testing.T) {
	idx := tsIndex()
	idx.AddFile(&model.FileFacts{Lang: model.LangTS, File: "a.ts"})
	idx.AddFile(&model.FileFacts{Lang: model.LangTS, File: "b.ts"})
	if deps := idx.Dependents("a.ts"); len(deps) != 0 {
		t.Fatalf("expected no dependents, got %v", deps)
	}
}

func TestDependents_Go_PackageSiblingsAndImporter(t *testing.T) {
	idx := goIndex("example.com/m")
	// service.go and helper.go share a package (same-scope siblings —
	// both are mutual dependents of each other via SameScopeFiles).
	// caller.go imports the package via a real import path.
	helperFacts := &model.FileFacts{Lang: model.LangGo, File: "internal/svc/helper.go",
		Entities: []model.Entity{goEntity(model.KindFunction, "internal/svc#process", "process")}}
	serviceFacts := &model.FileFacts{Lang: model.LangGo, File: "internal/svc/service.go"}
	callerFacts := &model.FileFacts{
		Lang: model.LangGo, File: "cmd/main.go",
		Imports: []model.ImportBinding{{LocalName: "svc", Source: "example.com/m/internal/svc", IsNamespace: true}},
	}
	idx.AddFile(helperFacts)
	idx.AddFile(serviceFacts)
	idx.AddFile(callerFacts)

	deps := idx.Dependents("internal/svc/helper.go")
	got := map[string]bool{}
	for _, d := range deps {
		got[d] = true
	}
	if !got["internal/svc/service.go"] {
		t.Errorf("expected the package sibling service.go among dependents, got %v", deps)
	}
	if !got["cmd/main.go"] {
		t.Errorf("expected the importing file cmd/main.go among dependents, got %v", deps)
	}
}
