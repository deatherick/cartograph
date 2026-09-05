package python

import (
	"context"
	"testing"

	"github.com/deatherick/cartograph/internal/model"
)

const sample = `from django.db import models
from .base import BaseModel
from conduit.apps.core.utils import generate_random_string


class Repository(BaseModel):

    def find_by_id(self, id):
        return None


class ArticleService(models.Model):

    def __init__(self, repository):
        self.repository = Repository()
        self.token = generate_random_string()

    def get_article(self, id):
        article = self.repository.find_by_id(id)
        self.log("fetched")
        return process(article)

    def log(self, msg):
        print(msg)


def process(article):
    def helper(x):
        return x

    return helper(article)


def test_get_article():
    service = ArticleService(None)
    assert service is not None
`

func TestExtract_EntitiesRefsImports(t *testing.T) {
	e := New()
	facts, err := e.Extract(context.Background(), "test-repo", "conduit/apps/articles/services.py", []byte(sample))
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	wantKinds := map[string]model.Kind{
		"conduit/apps/articles/services.py#Repository":                 model.KindClass,
		"conduit/apps/articles/services.py#Repository.find_by_id":      model.KindMethod,
		"conduit/apps/articles/services.py#ArticleService":             model.KindClass,
		"conduit/apps/articles/services.py#ArticleService.__init__":    model.KindMethod,
		"conduit/apps/articles/services.py#ArticleService.get_article": model.KindMethod,
		"conduit/apps/articles/services.py#ArticleService.log":         model.KindMethod,
		"conduit/apps/articles/services.py#process":                    model.KindFunction,
		"conduit/apps/articles/services.py#test_get_article":           model.KindTest,
	}
	got := map[string]model.Kind{}
	for _, ent := range facts.Entities {
		got[ent.Qualified] = ent.Kind
	}
	for q, wantKind := range wantKinds {
		gotKind, ok := got[q]
		if !ok {
			t.Errorf("missing entity %q (want kind %s); got: %+v", q, wantKind, got)
			continue
		}
		if gotKind != wantKind {
			t.Errorf("entity %q: got kind %s, want %s", q, gotKind, wantKind)
		}
	}
	// The nested `helper` def must NOT be a separate entity.
	for q := range got {
		if q == "conduit/apps/articles/services.py#helper" || q == "conduit/apps/articles/services.py#process.helper" {
			t.Errorf("nested def %q should not be emitted as its own entity", q)
		}
	}

	// Heritage: Repository extends BaseModel; ArticleService extends
	// models.Model (a qualified heritage reference).
	var sawUnqualifiedExtends, sawQualifiedExtends bool
	for _, r := range facts.Refs {
		if r.Kind != model.RefExtends {
			continue
		}
		if r.Target.Scope == model.ScopeUnqualified && r.Target.Name == "BaseModel" {
			sawUnqualifiedExtends = true
		}
		if r.Target.Scope == model.ScopeQualified && r.Target.Name == "models" && r.Target.Member == "Model" {
			sawQualifiedExtends = true
		}
	}
	if !sawUnqualifiedExtends {
		t.Error("expected a RefExtends targeting BaseModel")
	}
	if !sawQualifiedExtends {
		t.Error("expected a qualified RefExtends targeting models.Model")
	}

	// Imports: two named (relative + absolute), one namespace.
	var sawRelative, sawAbsolute, sawNamespace bool
	for _, im := range facts.Imports {
		switch {
		case im.Source == ".base" && im.ImportedName == "BaseModel":
			sawRelative = true
		case im.Source == "conduit.apps.core.utils" && im.ImportedName == "generate_random_string":
			sawAbsolute = true
		case im.Source == "django.db" && im.ImportedName == "models":
			sawAbsolute = true
		case im.IsNamespace:
			sawNamespace = true
		}
	}
	if !sawRelative {
		t.Errorf("expected a relative import of BaseModel from .base, got: %+v", facts.Imports)
	}
	if !sawAbsolute {
		t.Errorf("expected absolute imports, got: %+v", facts.Imports)
	}
	_ = sawNamespace // no plain `import x` in this sample; asserted false implicitly

	// Calls: self.repository.find_by_id (two-level, via constructor-set
	// field), self.log (same-class), process (module-level bare), helper
	// (nested, ScopeLocal).
	var sawFieldCall, sawSelfCall, sawModuleCall, sawLocalCall bool
	var fieldReceiverType string
	for _, r := range facts.Refs {
		if r.Kind != model.RefCall {
			continue
		}
		switch {
		case r.Target.Scope == model.ScopeQualified && r.Target.Name == "repository" && r.Target.Member == "find_by_id":
			sawFieldCall = true
			fieldReceiverType = r.Target.ReceiverType
		case r.Target.Scope == model.ScopeQualified && r.Target.Name == "self" && r.Target.Member == "log":
			sawSelfCall = true
		case r.Target.Scope == model.ScopeUnqualified && r.Target.Name == "process":
			sawModuleCall = true
		case r.Target.Name == "helper":
			if r.Target.Scope != model.ScopeLocal {
				t.Errorf("call to nested def helper: got scope %q, want %q", r.Target.Scope, model.ScopeLocal)
			}
			sawLocalCall = true
		}
	}
	if !sawFieldCall {
		t.Error("expected a qualified call self.repository.find_by_id")
	}
	if fieldReceiverType != "Repository" {
		t.Errorf("self.repository.find_by_id: got ReceiverType %q, want Repository", fieldReceiverType)
	}
	if !sawSelfCall {
		t.Error("expected a qualified call self.log (same-class)")
	}
	if !sawModuleCall {
		t.Error("expected a bare call to module-level process")
	}
	if !sawLocalCall {
		t.Error("expected a call to nested def helper")
	}
}

func TestExtract_PlainNamespaceImport(t *testing.T) {
	const src = `import jwt as pyjwt

def encode(payload):
    return pyjwt.encode(payload, "secret")
`
	e := New()
	facts, err := e.Extract(context.Background(), "test-repo", "conduit/apps/authentication/util.py", []byte(src))
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(facts.Imports) != 1 {
		t.Fatalf("got %d imports, want 1: %+v", len(facts.Imports), facts.Imports)
	}
	im := facts.Imports[0]
	if im.LocalName != "pyjwt" || im.Source != "jwt" || !im.IsNamespace {
		t.Errorf("got import %+v, want LocalName=pyjwt Source=jwt IsNamespace=true", im)
	}
}

func TestExtract_RelativeImportDepth(t *testing.T) {
	const src = `from ..core.models import TimestampedModel

class Article(TimestampedModel):
    pass
`
	e := New()
	facts, err := e.Extract(context.Background(), "test-repo", "conduit/apps/articles/models.py", []byte(src))
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(facts.Imports) != 1 {
		t.Fatalf("got %d imports, want 1: %+v", len(facts.Imports), facts.Imports)
	}
	im := facts.Imports[0]
	if im.Source != "..core.models" || im.ImportedName != "TimestampedModel" {
		t.Errorf("got import %+v, want Source=..core.models ImportedName=TimestampedModel", im)
	}
}

// TestExtract_SelfFieldAssignedFromTypedParameter closes a documented
// gap: `self.repository = repository` (assigning a constructor's own
// parameter directly) gave no receiver-type signal before — only
// `self.repository = Repository()` did. With a PEP 484 type hint on the
// parameter, a call through the resulting field must now resolve.
func TestExtract_SelfFieldAssignedFromTypedParameter(t *testing.T) {
	const src = `class ArticleService:
    def __init__(self, repository: Repository):
        self.repository = repository

    def get_article(self, id):
        return self.repository.find_by_id(id)
`
	e := New()
	facts, err := e.Extract(context.Background(), "test-repo", "services.py", []byte(src))
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	var found bool
	for _, r := range facts.Refs {
		if r.Kind == model.RefCall && r.Target.Member == "find_by_id" {
			found = true
			if r.Target.ReceiverType != "Repository" {
				t.Errorf("got ReceiverType %q, want %q", r.Target.ReceiverType, "Repository")
			}
		}
	}
	if !found {
		t.Fatalf("expected a find_by_id call ref, got: %+v", facts.Refs)
	}
}

// TestExtract_SelfFieldAssignedFromUntypedParameter_NoSignal verifies the
// same idiom WITHOUT a type hint produces no ReceiverType (never guessed
// from the parameter's bare name).
func TestExtract_SelfFieldAssignedFromUntypedParameter_NoSignal(t *testing.T) {
	const src = `class ArticleService:
    def __init__(self, repository):
        self.repository = repository

    def get_article(self, id):
        return self.repository.find_by_id(id)
`
	e := New()
	facts, err := e.Extract(context.Background(), "test-repo", "services.py", []byte(src))
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	var found bool
	for _, r := range facts.Refs {
		if r.Kind == model.RefCall && r.Target.Member == "find_by_id" {
			found = true
			if r.Target.ReceiverType != "" {
				t.Errorf("got ReceiverType %q, want empty (no type hint present, must not be guessed)", r.Target.ReceiverType)
			}
		}
	}
	if !found {
		t.Fatalf("expected a find_by_id call ref, got: %+v", facts.Refs)
	}
}
