// Package golang implements the Go extractor — Phase 3's first language,
// chosen ahead of C#/Python because it is the language this project's own
// source is written in (the self-hosting milestone, docs/MVP.md's deferred
// list) and because dogfooding surfaces real extractor/resolver gaps the
// same way the TypeScript real-repo validation did in Phase 1/2 (ADR-0004).
//
// Named "golang", not "go" — package names are identifiers, and "go" is a
// Go keyword, so `package go` does not compile.
//
// Lives inside internal/parser's architecture boundary (see that package's
// doc) exactly like internal/parser/ts: the only package outside
// internal/parser itself allowed to import go-tree-sitter and
// tree-sitter-go, and it never lets a sitter.Node/Tree escape its exported
// API.
//
// Query-driven, same bet as Phase 1 (queries/entities.scm), not a
// hand-written AST walk.
package golang

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"fmt"
	"path"
	"path/filepath"
	"strings"
	"unicode"

	sitter "github.com/tree-sitter/go-tree-sitter"
	gogrammar "github.com/tree-sitter/tree-sitter-go/bindings/go"

	"github.com/deatherick/cartograph/internal/model"
	"github.com/deatherick/cartograph/internal/parser"
)

//go:embed queries/entities.scm
var entitiesQuerySrc string

var language = parser.NewLanguage("go", sitter.NewLanguage(gogrammar.Language()))

// Extractor implements the parser.Extractor interface for Go.
type Extractor struct {
	query *sitter.Query
}

// New compiles the entity query. Panics on a compile failure — a malformed
// embedded .scm is a packaging bug, matching internal/parser/ts's New.
func New() *Extractor {
	q, err := sitter.NewQuery(language.Raw(), entitiesQuerySrc)
	if err != nil {
		panic(fmt.Sprintf("golang: compiling entities.scm: %v", err))
	}
	return &Extractor{query: q}
}

func (e *Extractor) Extensions() []string { return []string{".go"} }

// Extract parses src (the file at repoRelativePath) and returns every
// entity and unresolved reference it contains. Entities are qualified by
// their DIRECTORY, not their file — a Go package spans every file in one
// directory, unlike TypeScript's one-module-per-file model — so a struct's
// method can live in a different file from the struct itself and still
// resolve, exactly as the Go compiler treats them as one unit.
func (e *Extractor) Extract(ctx context.Context, repo, repoRelativePath string, src []byte) (*model.FileFacts, error) {
	tree, err := parser.Parse(ctx, src, language)
	if err != nil {
		return nil, err
	}
	defer tree.Close()

	file := filepath.ToSlash(repoRelativePath)
	dir := path.Dir(file)
	isTestFile := strings.HasSuffix(file, "_test.go")
	facts := &model.FileFacts{File: file, Lang: model.LangGo}

	cursor := sitter.NewQueryCursor()
	defer cursor.Close()

	names := e.query.CaptureNames()
	matches := cursor.Matches(e.query, tree.Root(), src)

	// structFieldTypes maps a struct type's NAME to its field->type map,
	// built from typed field declarations (queries/entities.scm's
	// field.decl/field.decl.ptr patterns) — the Go analog of
	// internal/parser/ts's classPropTypes. Keyed by name, not start byte:
	// unlike TypeScript, a Go struct's fields are always declared inside
	// the SAME type_spec as the struct itself, in the same file this map is
	// rebuilt for on every call, so two same-named structs colliding is not
	// possible within one file (the compiler forbids it).
	structFieldTypes := map[string]map[string]string{}
	// fileVarTypesRaw mirrors internal/parser/ts's fileVarTypes: every
	// observed type for a locally-declared name (var/short-var/parameter,
	// AND a method's own receiver variable) across the whole file,
	// collapsed below to one type per name only when every observation
	// agreed. File-wide, not block-scoped — a deliberate, bounded
	// simplification carried over unchanged from Phase 1's TypeScript
	// extractor, not full symbol-table scoping.
	fileVarTypesRaw := map[string][]string{}
	scopeByStartByte := map[uint]model.EntityID{}
	// localFuncNames collects every name bound to a func_literal or a
	// function_type — see queries/entities.scm's localfunc.decl doc: a bare
	// call to one of these is ScopeLocal, never cross-resolved, not a
	// package-level bare call the same-file/same-package/builtin tiers
	// should be judging.
	localFuncNames := map[string]bool{}

	var pending []match
	for m := matches.Next(); m != nil; m = matches.Next() {
		pending = append(pending, collectCaptures(m, names))
	}

	for _, m := range pending {
		// Field-type and receiver-variable signals are collected
		// unconditionally, before the entity `continue` below — mirrors
		// internal/parser/ts's Extract, and for the same reason: a
		// field.decl/receiver.* match never satisfies entityFromMatch (it
		// carries no entity.* capture), so collecting it after that check
		// would mean it never runs.
		if fname := m.captures["field.name"]; m.captures["field.type"] != nil {
			if owner, ok := enclosingTypeSpecName(m.captures["field.type"], src); ok {
				if structFieldTypes[owner] == nil {
					structFieldTypes[owner] = map[string]string{}
				}
				if fname != nil {
					structFieldTypes[owner][text(src, fname)] = text(src, m.captures["field.type"])
				}
			}
		}
		if n := m.captures["receiver.varname"]; n != nil && m.captures["receiver.vartype"] != nil {
			fileVarTypesRaw[text(src, n)] = append(fileVarTypesRaw[text(src, n)], text(src, m.captures["receiver.vartype"]))
		}
		// A method's own receiver variable (`func (r *Foo) Bar()`) is a
		// var-type signal too — it's what lets a call like
		// `r.repo.FindByEmail()` inside Bar's body resolve r's type without
		// a separate receiver.vartype capture (see entity.owner's reuse
		// here; queries/entities.scm's method patterns capture the receiver
		// name as receiver.varname but derive its type from the SAME node
		// entityFromMatch reads as entity.owner, since they're the same
		// text — no need for a redundant capture).
		if n := m.captures["receiver.varname"]; n != nil && m.captures["entity.owner"] != nil {
			fileVarTypesRaw[text(src, n)] = append(fileVarTypesRaw[text(src, n)], text(src, m.captures["entity.owner"]))
		}
		if n := m.captures["localfunc.name"]; n != nil {
			localFuncNames[text(src, n)] = true
		}

		ent, id, ok := entityFromMatch(repo, file, dir, isTestFile, src, m)
		if !ok {
			continue
		}
		facts.Entities = append(facts.Entities, ent)
		if ent.Kind == model.KindFunction || ent.Kind == model.KindMethod {
			scopeByStartByte[ent.Anchor.StartByte] = id
		}
	}

	fileVarTypes := map[string]string{}
	for name, types := range fileVarTypesRaw {
		allSame := true
		for _, t := range types[1:] {
			if t != types[0] {
				allSame = false
				break
			}
		}
		if allSame {
			fileVarTypes[name] = types[0]
		}
	}

	for _, m := range pending {
		facts.Refs = append(facts.Refs, refsFromMatch(file, src, m, scopeByStartByte, structFieldTypes, fileVarTypes, localFuncNames)...)
		if ib, ok := importFromMatch(m, src); ok {
			facts.Imports = append(facts.Imports, ib)
		}
	}

	return facts, nil
}

type match struct {
	captures map[string]*sitter.Node
}

func collectCaptures(m *sitter.QueryMatch, names []string) match {
	mm := match{captures: map[string]*sitter.Node{}}
	for _, c := range m.Captures {
		n := c.Node
		mm.captures[names[c.Index]] = &n
	}
	return mm
}

func text(src []byte, n *sitter.Node) string {
	if n == nil {
		return ""
	}
	s, e := n.ByteRange()
	if int(e) > len(src) {
		e = uint(len(src))
	}
	return string(src[s:e])
}

// enclosingTypeSpecName walks up from n looking for a type_spec and returns
// its declared name — used to attribute a field declaration to the struct
// that contains it, the same role internal/parser/ts's
// enclosingClassStartByte plays for class fields.
func enclosingTypeSpecName(n *sitter.Node, src []byte) (string, bool) {
	for p := n.Parent(); p != nil; p = p.Parent() {
		if p.Kind() == "type_spec" {
			name := p.ChildByFieldName("name")
			if name == nil {
				return "", false
			}
			return text(src, name), true
		}
	}
	return "", false
}

func contentHash(s string) string {
	h := sha256.Sum256([]byte(strings.TrimSpace(s)))
	return hex.EncodeToString(h[:])[:16]
}

func anchorFrom(file string, src []byte, n *sitter.Node) model.Anchor {
	sb, eb := n.ByteRange()
	sp, ep := n.StartPosition(), n.EndPosition()
	return model.Anchor{
		File:        file,
		StartByte:   sb,
		EndByte:     eb,
		StartLine:   int(sp.Row) + 1,
		EndLine:     int(ep.Row) + 1,
		ContentHash: contentHash(text(src, n)),
	}
}

// isGoTestName reports whether name follows `go test`'s own discovery rule
// for test functions: "Test" followed by a name that does not start with a
// lowercase letter (so TestFoo qualifies, testFoo and Testable's lowercase
// continuation-adjacent cases are excluded the same way `go vet` warns
// about malformed test names). Applied only within _test.go files.
func isGoTestName(name string) bool {
	if !strings.HasPrefix(name, "Test") {
		return false
	}
	rest := name[len("Test"):]
	if rest == "" {
		return true // bare `func Test(t *testing.T)` is valid, if unusual
	}
	r := []rune(rest)[0]
	return !unicode.IsLower(r)
}

// entityFromMatch converts one collected match into a model.Entity, if the
// match is one of the entity-producing patterns.
func entityFromMatch(repo, file, dir string, isTestFile bool, src []byte, m match) (model.Entity, model.EntityID, bool) {
	var kind model.Kind
	var node *sitter.Node
	switch {
	case m.captures["entity.class"] != nil:
		kind, node = model.KindClass, m.captures["entity.class"]
	case m.captures["entity.interface"] != nil:
		kind, node = model.KindInterface, m.captures["entity.interface"]
	case m.captures["entity.typealias"] != nil:
		kind, node = model.KindTypeAlias, m.captures["entity.typealias"]
	case m.captures["entity.function"] != nil:
		kind, node = model.KindFunction, m.captures["entity.function"]
	case m.captures["entity.method"] != nil:
		kind, node = model.KindMethod, m.captures["entity.method"]
	default:
		return model.Entity{}, "", false
	}

	nameNode := m.captures["entity.name"]
	if nameNode == nil {
		return model.Entity{}, "", false
	}
	name := text(src, nameNode)

	if kind == model.KindFunction && isTestFile && isGoTestName(name) {
		// A go test function IS a real function declaration (unlike Jest's
		// `it(...)` callback blocks in TypeScript) — reclassified here
		// rather than emitted as a second, duplicate entity.
		kind = model.KindTest
	}

	qualified := dir + "#" + name
	disambiguator := ""
	if kind == model.KindMethod {
		owner := m.captures["entity.owner"]
		if owner == nil {
			return model.Entity{}, "", false
		}
		qualified = dir + "#" + text(src, owner) + "." + name
	}
	if kind == model.KindFunction || kind == model.KindMethod {
		// Arity disambiguator, same convention as TypeScript's — Go has no
		// method/function overloading, so collisions are not expected in
		// practice, but Kind.overloadable() requires a non-empty value
		// regardless of language (model.NewEntityID).
		params := m.captures["entity.params"]
		arity := 0
		if params != nil {
			arity = int(params.NamedChildCount())
		}
		disambiguator = fmt.Sprintf("arity:%d", arity)
	}

	id := model.NewEntityID(repo, kind, qualified, disambiguator)
	ent := model.Entity{
		ID:        id,
		Kind:      kind,
		Lang:      model.LangGo,
		Repo:      repo,
		Qualified: qualified,
		Name:      name,
		Anchor:    anchorFrom(file, src, node),
	}
	return ent, id, true
}

// refsFromMatch converts call/embed matches into typed Refs, attributing
// each to its enclosing function/method (Ref.Src) via scopeByStartByte.
func refsFromMatch(file string, src []byte, m match, scopeByStartByte map[uint]model.EntityID, structFieldTypes map[string]map[string]string, fileVarTypes map[string]string, localFuncNames map[string]bool) []model.Ref {
	var refs []model.Ref

	// Anonymous (embedded) struct/interface field: `type X struct { Base }`.
	// A named field (field.name present) is a type-signal only, not a
	// reference — handled in Extract's signal-collection pass, not here.
	if n := m.captures["field.type"]; n != nil && m.captures["field.name"] == nil {
		refs = append(refs, model.Ref{
			Kind:   model.RefExtends,
			Target: model.RefTarget{Scope: model.ScopeUnqualified, Name: text(src, n)},
			Anchor: anchorFrom(file, src, n),
		})
	}

	if n := m.captures["call.name"]; n != nil {
		switch {
		case m.captures["call.base"] != nil:
			// `base.object.Name(...)` — a two-level selector, the Go analog
			// of TypeScript's `this.member.method()`. base's type resolves
			// via fileVarTypes (a local var, parameter, or the enclosing
			// method's own receiver); object's type then resolves via that
			// struct's field-type map.
			base := text(src, m.captures["call.base"])
			object := text(src, m.captures["call.object"])
			target := model.RefTarget{Scope: model.ScopeQualified, Name: object, Member: text(src, n)}
			if baseType := fileVarTypes[base]; baseType != "" {
				if fields := structFieldTypes[baseType]; fields != nil {
					target.ReceiverType = fields[object]
				}
			}
			refs = append(refs, model.Ref{
				Kind:   model.RefCall,
				Src:    enclosingScope(n, scopeByStartByte),
				Target: target,
				Anchor: anchorFrom(file, src, n),
			})
		case m.captures["call.object"] != nil:
			// `obj.Name(...)` — either a package-qualified call (obj is an
			// import alias, resolved by the import table) or a single-level
			// method call through a locally typed receiver (obj is a var,
			// parameter, or method receiver name).
			obj := text(src, m.captures["call.object"])
			target := model.RefTarget{
				Scope:        model.ScopeQualified,
				Name:         obj,
				Member:       text(src, n),
				ReceiverType: fileVarTypes[obj],
			}
			refs = append(refs, model.Ref{
				Kind:   model.RefCall,
				Src:    enclosingScope(n, scopeByStartByte),
				Target: target,
				Anchor: anchorFrom(file, src, n),
			})
		default:
			name := text(src, n)
			scope := model.ScopeUnqualified
			if localFuncNames[name] {
				scope = model.ScopeLocal
			}
			refs = append(refs, model.Ref{
				Kind:   model.RefCall,
				Src:    enclosingScope(n, scopeByStartByte),
				Target: model.RefTarget{Scope: scope, Name: name},
				Anchor: anchorFrom(file, src, n),
			})
		}
	}
	return refs
}

// enclosingScope walks up from n looking for the nearest ancestor whose
// start byte is a registered function/method entity.
func enclosingScope(n *sitter.Node, scopeByStartByte map[uint]model.EntityID) model.EntityID {
	for p := n.Parent(); p != nil; p = p.Parent() {
		switch p.Kind() {
		case "function_declaration", "method_declaration":
		default:
			continue
		}
		sb, _ := p.ByteRange()
		if id, ok := scopeByStartByte[sb]; ok {
			return id
		}
	}
	return ""
}

func importFromMatch(m match, src []byte) (model.ImportBinding, bool) {
	source := m.captures["import.source"]
	if source == nil {
		return model.ImportBinding{}, false
	}
	// interpreted_string_literal's text includes the surrounding quotes
	// (unlike TypeScript's string_fragment, which is the inner content
	// sub-node) — stripped here, once, rather than at every call site.
	src0 := strings.Trim(text(src, source), `"`)
	// Every Go import is used package-qualified at call sites, so every
	// binding is modeled as a namespace import — see queries/entities.scm's
	// import.stmt doc.
	local := ""
	if alias := m.captures["import.alias"]; alias != nil {
		local = text(src, alias)
	} else {
		// No explicit alias: the identifier used at call sites is the
		// imported package's OWN declared name, which this extractor has
		// no way to know without parsing that package's files (possibly in
		// another, not-yet-walked directory, or genuinely external). V0
		// approximates it with the import path's last segment — correct
		// for the overwhelming majority of real packages (including every
		// package in this project's own source), wrong only for the rare
		// package whose declared name differs from its directory name.
		// Documented limitation, not a silent guess: this exact assumption
		// is recorded in the extractor's ADR.
		local = path.Base(src0)
	}
	return model.ImportBinding{LocalName: local, Source: src0, IsNamespace: true}, true
}
