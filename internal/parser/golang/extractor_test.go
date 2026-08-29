package golang

import (
	"context"
	"testing"

	"github.com/deatherick/cartograph/internal/model"
)

const sample = `package svc

import (
	"fmt"
	"myrepo/internal/repo"
)

type Base struct {
	Name string
}

type Service struct {
	Base
	dep *Dep
}

type Dep struct {
	label string
}

func (d *Dep) Greeting() string {
	return d.label
}

func (s *Service) DoThing(id string) error {
	g := s.dep.Greeting()
	fmt.Println(g)
	return process(id)
}

func process(id string) error {
	return repo.Validate(id)
}
`

const testSample = `package svc

import "testing"

func TestDoThing(t *testing.T) {
	s := &Service{}
	s.DoThing("x")
}

func helperNotATest(t *testing.T) {}
`

func TestExtract_EntitiesRefsImports(t *testing.T) {
	e := New()
	facts, err := e.Extract(context.Background(), "test-repo", "internal/svc/service.go", []byte(sample))
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	wantKinds := map[string]model.Kind{
		"internal/svc#Base":            model.KindClass,
		"internal/svc#Service":         model.KindClass,
		"internal/svc#Dep":             model.KindClass,
		"internal/svc#Dep.Greeting":    model.KindMethod,
		"internal/svc#Service.DoThing": model.KindMethod,
		"internal/svc#process":         model.KindFunction,
	}
	got := map[string]model.Kind{}
	for _, ent := range facts.Entities {
		got[ent.Qualified] = ent.Kind
	}
	for q, wantKind := range wantKinds {
		gotKind, ok := got[q]
		if !ok {
			t.Errorf("missing entity %q (want kind %s)", q, wantKind)
			continue
		}
		if gotKind != wantKind {
			t.Errorf("entity %q: got kind %s, want %s", q, gotKind, wantKind)
		}
	}
	if len(facts.Entities) != len(wantKinds) {
		t.Errorf("got %d entities, want %d: %+v", len(facts.Entities), len(wantKinds), got)
	}

	// Embedding: Service embeds Base -> RefExtends targeting "Base".
	var sawExtends bool
	for _, r := range facts.Refs {
		if r.Kind == model.RefExtends && r.Target.Name == "Base" {
			sawExtends = true
		}
	}
	if !sawExtends {
		t.Error("expected a RefExtends targeting Base (struct embedding)")
	}

	// Two-level selector: s.dep.Greeting() should resolve dep's type via
	// Service's field-type map and report ReceiverType="Dep".
	var sawTwoLevel bool
	for _, r := range facts.Refs {
		if r.Kind == model.RefCall && r.Target.Member == "Greeting" {
			sawTwoLevel = true
			if r.Target.ReceiverType != "Dep" {
				t.Errorf("s.dep.Greeting(): got ReceiverType=%q, want %q", r.Target.ReceiverType, "Dep")
			}
		}
	}
	if !sawTwoLevel {
		t.Error("expected a call ref for s.dep.Greeting()")
	}

	// Package-qualified call to an external package: repo.Validate(id).
	var sawPkgCall bool
	for _, r := range facts.Refs {
		if r.Kind == model.RefCall && r.Target.Name == "repo" && r.Target.Member == "Validate" {
			sawPkgCall = true
		}
	}
	if !sawPkgCall {
		t.Error("expected a call ref for repo.Validate(id)")
	}

	// Bare call: process(id).
	var sawBareCall bool
	for _, r := range facts.Refs {
		if r.Kind == model.RefCall && r.Target.Scope == model.ScopeUnqualified && r.Target.Name == "process" {
			sawBareCall = true
		}
	}
	if !sawBareCall {
		t.Error("expected a bare call ref for process(id)")
	}

	wantImports := map[string]string{"fmt": "fmt", "repo": "myrepo/internal/repo"}
	gotImports := map[string]string{}
	for _, ib := range facts.Imports {
		gotImports[ib.LocalName] = ib.Source
		if !ib.IsNamespace {
			t.Errorf("import %q: expected IsNamespace=true (every Go import is package-qualified)", ib.LocalName)
		}
	}
	for local, source := range wantImports {
		if got := gotImports[local]; got != source {
			t.Errorf("import %q: got source %q, want %q", local, got, source)
		}
	}
}

func TestExtract_TestFunctionDetection(t *testing.T) {
	e := New()
	facts, err := e.Extract(context.Background(), "test-repo", "internal/svc/service_test.go", []byte(testSample))
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	kinds := map[string]model.Kind{}
	for _, ent := range facts.Entities {
		kinds[ent.Name] = ent.Kind
	}
	if kinds["TestDoThing"] != model.KindTest {
		t.Errorf("TestDoThing: got kind %s, want %s", kinds["TestDoThing"], model.KindTest)
	}
	if kinds["helperNotATest"] != model.KindFunction {
		t.Errorf("helperNotATest: got kind %s, want %s (not a test — lowercase after Test prefix doesn't apply, but this name doesn't even start with Test)", kinds["helperNotATest"], model.KindFunction)
	}
}

func TestExtract_LocalFunctionValue_IsScopeLocalNotUnqualified(t *testing.T) {
	// Found by self-hosting: a callback parameter and a closure called
	// bare must be ScopeLocal, never ScopeUnqualified — otherwise the
	// resolver reports DispositionBugExtractor for something that was never
	// a missed package-level declaration in the first place.
	src := `package svc

func walkTree(fn func(path string) error) error {
	walk := func(p string) error {
		return fn(p)
	}
	return walk("x")
}
`
	e := New()
	facts, err := e.Extract(context.Background(), "test-repo", "internal/svc/walk.go", []byte(src))
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	scopes := map[string]model.TargetScope{}
	for _, r := range facts.Refs {
		if r.Kind == model.RefCall {
			scopes[r.Target.Name] = r.Target.Scope
		}
	}
	for _, name := range []string{"fn", "walk"} {
		if got := scopes[name]; got != model.ScopeLocal {
			t.Errorf("call to %q: got scope %s, want %s", name, got, model.ScopeLocal)
		}
	}
}

func TestExtract_NonTestFileTestPrefixedFuncStaysFunction(t *testing.T) {
	// A function named TestFoo in a regular (non-_test.go) file is NOT
	// reclassified — go test itself would never run it either.
	e := New()
	facts, err := e.Extract(context.Background(), "test-repo", "internal/svc/service.go", []byte("package svc\n\nfunc TestFoo() {}\n"))
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(facts.Entities) != 1 || facts.Entities[0].Kind != model.KindFunction {
		t.Errorf("got %+v, want a single KindFunction entity", facts.Entities)
	}
}
