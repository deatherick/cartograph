package csharp

import (
	"context"
	"testing"

	"github.com/deatherick/cartograph/internal/model"
)

const sample = `using System;
using Ordering.Domain;

namespace Ordering.Services
{
    public interface IOrderRepository
    {
        Order GetById(int id);
    }

    public class BaseService
    {
        protected void Log(string msg) { Console.WriteLine(msg); }
    }

    public class OrderService : BaseService, IOrderRepository
    {
        private readonly IOrderRepository _repository;

        public OrderService(IOrderRepository repository)
        {
            _repository = repository;
        }

        public Order GetById(int id)
        {
            var order = _repository.GetById(id);
            this.Log("fetched");
            return Process(order);
        }

        private Order Process(Order order)
        {
            return order;
        }
    }
}
`

func TestExtract_EntitiesRefsImports(t *testing.T) {
	e := New()
	facts, err := e.Extract(context.Background(), "test-repo", "src/Ordering/OrderService.cs", []byte(sample))
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	wantKinds := map[string]model.Kind{
		"src/Ordering#IOrderRepository":         model.KindInterface,
		"src/Ordering#IOrderRepository.GetById": model.KindMethod,
		"src/Ordering#BaseService":              model.KindClass,
		"src/Ordering#BaseService.Log":          model.KindMethod,
		"src/Ordering#OrderService":             model.KindClass,
		"src/Ordering#OrderService.ctor":        model.KindMethod, // constructor
		"src/Ordering#OrderService.GetById":     model.KindMethod,
		"src/Ordering#OrderService.Process":     model.KindMethod,
	}
	got := map[string]model.Kind{}
	for _, ent := range facts.Entities {
		got[ent.Qualified] = ent.Kind
	}
	for q, wantKind := range wantKinds {
		gotKind, ok := got[q]
		if !ok {
			t.Errorf("missing entity %q (want kind %s); got entities: %+v", q, wantKind, got)
			continue
		}
		if gotKind != wantKind {
			t.Errorf("entity %q: got kind %s, want %s", q, gotKind, wantKind)
		}
	}

	// Heritage: OrderService extends BaseService and (heuristically, per
	// the extractor) "implements" IOrderRepository — both emitted as
	// RefExtends here; reclassification to RefImplements is
	// internal/resolve's job (reclassifyHeritageEdge), not the
	// extractor's, so both should appear as RefExtends targets.
	extendsTargets := map[string]bool{}
	for _, r := range facts.Refs {
		if r.Kind == model.RefExtends {
			extendsTargets[r.Target.Name] = true
		}
	}
	if !extendsTargets["BaseService"] {
		t.Error("expected a RefExtends targeting BaseService")
	}
	if !extendsTargets["IOrderRepository"] {
		t.Error("expected a RefExtends targeting IOrderRepository (reclassified downstream by the resolver)")
	}

	// Imports: two `using` directives, both plain-form namespace imports.
	wantSources := map[string]bool{"System": true, "Ordering.Domain": true}
	if len(facts.Imports) != len(wantSources) {
		t.Errorf("got %d imports, want %d: %+v", len(facts.Imports), len(wantSources), facts.Imports)
	}
	for _, im := range facts.Imports {
		if !wantSources[im.Source] {
			t.Errorf("unexpected import source %q", im.Source)
		}
		if !im.IsNamespace {
			t.Errorf("import %q: expected IsNamespace=true", im.Source)
		}
		if im.LocalName != "" {
			t.Errorf("import %q: expected empty LocalName (plain form), got %q", im.Source, im.LocalName)
		}
	}

	// Calls: constructor-injected field call (_repository.GetById), a
	// this.Method() call (Log), and a bare same-class call (Process).
	var sawQualified, sawThisBare, sawBare bool
	var qualifiedReceiverType string
	for _, r := range facts.Refs {
		if r.Kind != model.RefCall {
			continue
		}
		switch {
		case r.Target.Scope == model.ScopeQualified && r.Target.Name == "_repository" && r.Target.Member == "GetById":
			sawQualified = true
			qualifiedReceiverType = r.Target.ReceiverType
		case r.Target.Scope == model.ScopeUnqualified && r.Target.Name == "Log":
			sawThisBare = true
		case r.Target.Scope == model.ScopeUnqualified && r.Target.Name == "Process":
			sawBare = true
		}
	}
	if !sawQualified {
		t.Error("expected a qualified call _repository.GetById")
	}
	if qualifiedReceiverType != "IOrderRepository" {
		t.Errorf("_repository.GetById: got ReceiverType %q, want IOrderRepository (constructor-injected field type)", qualifiedReceiverType)
	}
	if !sawThisBare {
		t.Error("expected this.Log(...) to be captured as an unqualified bare call named Log")
	}
	if !sawBare {
		t.Error("expected a bare call to Process")
	}
}

func TestExtract_UsingAlias(t *testing.T) {
	const src = `using Repo = Ordering.Domain.Repositories;
namespace X { class Y { void M() { var x = Repo.Thing.Create(); } } }
`
	e := New()
	facts, err := e.Extract(context.Background(), "test-repo", "src/X/Y.cs", []byte(src))
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(facts.Imports) != 1 {
		t.Fatalf("got %d imports, want 1: %+v", len(facts.Imports), facts.Imports)
	}
	im := facts.Imports[0]
	if im.LocalName != "Repo" || im.Source != "Ordering.Domain.Repositories" || !im.IsNamespace {
		t.Errorf("got import %+v, want LocalName=Repo Source=Ordering.Domain.Repositories IsNamespace=true", im)
	}
}

func TestExtract_LocalFunctionCallIsScopeLocal(t *testing.T) {
	const src = `namespace X { class Y { void M() {
    int Helper(int n) { return n + 1; }
    Helper(1);
  } } }
`
	e := New()
	facts, err := e.Extract(context.Background(), "test-repo", "src/X/Y.cs", []byte(src))
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	var sawLocal bool
	for _, r := range facts.Refs {
		if r.Kind == model.RefCall && r.Target.Name == "Helper" {
			if r.Target.Scope != model.ScopeLocal {
				t.Errorf("call to local function Helper: got scope %q, want %q", r.Target.Scope, model.ScopeLocal)
			}
			sawLocal = true
		}
	}
	if !sawLocal {
		t.Error("expected a call ref targeting Helper")
	}
}

func TestExtract_PropertyEntity(t *testing.T) {
	const src = `namespace X { class Y {
    public IOrderRepository Repository { get; set; }
  } }
`
	e := New()
	facts, err := e.Extract(context.Background(), "test-repo", "src/X/Y.cs", []byte(src))
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	var found bool
	for _, ent := range facts.Entities {
		if ent.Qualified == "src/X#Y.Repository" {
			found = true
			if ent.Kind != model.KindProperty {
				t.Errorf("got kind %s, want KindProperty", ent.Kind)
			}
		}
	}
	if !found {
		t.Errorf("missing property entity src/X#Y.Repository, got: %+v", facts.Entities)
	}
}
