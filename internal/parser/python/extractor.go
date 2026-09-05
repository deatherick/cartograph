// Package python implements the Python extractor — Phase 3c (ADR-0024),
// added via the plug-and-play language architecture (ADR-0011) after Go
// (ADR-0010) and C# (ADR-0023): a new Extractor here, a new
// LanguagePolicy (internal/resolve/lang_python.go), one registration in
// internal/index/languages.go, and NOTHING ELSE changes.
//
// Lives inside internal/parser's architecture boundary (see that
// package's doc) exactly like internal/parser/ts, golang, and csharp: the
// only place outside internal/parser itself allowed to import
// go-tree-sitter and tree-sitter-python, and it never lets a
// sitter.Node/Tree escape its exported API.
//
// Query-driven, same bet as every prior language (queries/entities.scm),
// not a hand-written AST walk.
package python

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"
	pygrammar "github.com/tree-sitter/tree-sitter-python/bindings/go"

	"github.com/deatherick/cartograph/internal/model"
	"github.com/deatherick/cartograph/internal/parser"
)

//go:embed queries/entities.scm
var entitiesQuerySrc string

var language = parser.NewLanguage("python", sitter.NewLanguage(pygrammar.Language()))

// Extractor implements the parser.Extractor interface for Python.
type Extractor struct {
	query *sitter.Query
}

// New compiles the entity query. Panics on a compile failure — a
// malformed embedded .scm is a packaging bug, matching every other
// language extractor's own New().
func New() *Extractor {
	q, err := sitter.NewQuery(language.Raw(), entitiesQuerySrc)
	if err != nil {
		panic(fmt.Sprintf("python: compiling entities.scm: %v", err))
	}
	return &Extractor{query: q}
}

func (e *Extractor) Extensions() []string { return []string{".py"} }

// Extract parses src (the file at repoRelativePath) and returns every
// entity and unresolved reference it contains. Entities are qualified by
// their FILE, not their directory — unlike Go/C# (ADR-0010, ADR-0023),
// Python has no implicit same-package/same-namespace visibility: a
// sibling file in the same directory still needs an explicit `from
// .other import Name` to use anything from it, so file-scoping (like
// TypeScript) is the structurally correct choice here, not a narrower
// approximation. See internal/resolve/lang_python.go's package doc.
//
// A `def` nested inside ANOTHER `def` (a closure/helper — real and
// common in Python) is deliberately never emitted as its own entity: only
// its name is recorded (localFuncNames), so a bare call to it from
// within its enclosing function is tagged ScopeLocal rather than
// misjudged as a missed module-level declaration or an ambiguous
// repo-wide bare name — the same role Go's localfunc.decl plays for
// closures (edge-case-backlog.md B4/J2), reached differently here since
// Python's nested `def` is syntactically identical to a top-level one
// (the SAME entities.scm pattern matches both; nesting is determined in
// Go code, via enclosingDefScope, not by a separate query pattern).
func (e *Extractor) Extract(ctx context.Context, repo, repoRelativePath string, src []byte) (*model.FileFacts, error) {
	tree, err := parser.Parse(ctx, src, language)
	if err != nil {
		return nil, err
	}
	defer tree.Close()

	file := filepath.ToSlash(repoRelativePath)
	facts := &model.FileFacts{File: file, Lang: model.LangPython}

	cursor := sitter.NewQueryCursor()
	defer cursor.Close()

	names := e.query.CaptureNames()
	matches := cursor.Matches(e.query, tree.Root(), src)

	// fieldTypesByOwner maps a class's NAME to its `self.field = Type(...)`
	// assignments' field->type map — the Python analog of Go's
	// structFieldTypes/C#'s fieldTypesByOwner, except built from an
	// ASSIGNMENT observed anywhere in the class's methods (usually
	// `__init__`), not a static field declaration — Python has none.
	fieldTypesByOwner := map[string]map[string]string{}
	// fileVarTypesRaw mirrors every other extractor's own file-wide (not
	// block-scoped) variable-type map.
	fileVarTypesRaw := map[string][]string{}
	// paramTypesByFunc maps a function_definition's start byte (see
	// enclosingFunctionStartByte) to its OWN typed parameters' name->type
	// map — scoped per-function, unlike fileVarTypesRaw, since two
	// different methods' same-named parameter could have different types
	// and `self.attr = some_param` (below) must only ever cross-reference
	// the SAME method's own parameter, never a same-named one elsewhere
	// in the file.
	paramTypesByFunc := map[uint]map[string]string{}
	// selfFieldFromParam collects every `self.attr = some_param`
	// assignment seen (queries/entities.scm's receiver.fieldfromparam
	// doc) for a second pass once paramTypesByFunc is fully populated —
	// cross-referencing them inline, during this same first pass, would
	// risk missing a parameter type declared later in iteration order
	// within the same function (matches carry no guaranteed order).
	type selfFieldFromParamEntry struct {
		owner, field, param string
		funcStartByte       uint
	}
	var selfFieldFromParam []selfFieldFromParamEntry
	scopeByStartByte := map[uint]model.EntityID{}
	localFuncNames := map[string]bool{}

	var pending []match
	for m := matches.Next(); m != nil; m = matches.Next() {
		pending = append(pending, collectCaptures(m, names))
	}

	for _, m := range pending {
		if n := m.captures["receiver.fieldname"]; n != nil {
			if selfObj := m.captures["receiver.selfobject"]; selfObj != nil && text(src, selfObj) == "self" {
				if t := m.captures["receiver.fieldtype"]; t != nil {
					if owner, ok := enclosingClassName(n, src); ok {
						if fieldTypesByOwner[owner] == nil {
							fieldTypesByOwner[owner] = map[string]string{}
						}
						fieldTypesByOwner[owner][text(src, n)] = text(src, t)
					}
				}
			}
		}
		if n := m.captures["receiver.varname"]; n != nil {
			if t := m.captures["receiver.vartype"]; t != nil {
				fileVarTypesRaw[text(src, n)] = append(fileVarTypesRaw[text(src, n)], text(src, t))
				if funcStart, ok := enclosingFunctionStartByte(n); ok {
					if paramTypesByFunc[funcStart] == nil {
						paramTypesByFunc[funcStart] = map[string]string{}
					}
					paramTypesByFunc[funcStart][text(src, n)] = text(src, t)
				}
			}
		}
		if n := m.captures["receiver.fieldnameparam"]; n != nil {
			selfObj := m.captures["receiver.selfobjectparam"]
			val := m.captures["receiver.fieldvalueparam"]
			if selfObj != nil && val != nil && text(src, selfObj) == "self" {
				if owner, ok := enclosingClassName(n, src); ok {
					if funcStart, ok := enclosingFunctionStartByte(n); ok {
						selfFieldFromParam = append(selfFieldFromParam, selfFieldFromParamEntry{
							owner: owner, field: text(src, n), param: text(src, val), funcStartByte: funcStart,
						})
					}
				}
			}
		}

		ent, id, isLocal, ok := entityFromMatch(repo, file, src, m)
		if isLocal {
			if n := m.captures["entity.name"]; n != nil {
				localFuncNames[text(src, n)] = true
			}
			continue
		}
		if !ok {
			continue
		}
		facts.Entities = append(facts.Entities, ent)
		if ent.Kind == model.KindFunction || ent.Kind == model.KindMethod || ent.Kind == model.KindTest {
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

	// Second pass over selfFieldFromParam, now that paramTypesByFunc is
	// fully populated: cross-reference `self.attr = some_param` against
	// its OWN enclosing function's typed parameters. Silently produces no
	// signal when the parameter has no PEP 484 hint — never a guess. The
	// call-based signal above (`self.attr = SomeClass(...)`) is preferred
	// when both are somehow present for the same field.
	for _, e := range selfFieldFromParam {
		t, ok := paramTypesByFunc[e.funcStartByte][e.param]
		if !ok {
			continue
		}
		if fieldTypesByOwner[e.owner] == nil {
			fieldTypesByOwner[e.owner] = map[string]string{}
		}
		if _, exists := fieldTypesByOwner[e.owner][e.field]; !exists {
			fieldTypesByOwner[e.owner][e.field] = t
		}
	}

	for _, m := range pending {
		facts.Refs = append(facts.Refs, refsFromMatch(repo, file, src, m, scopeByStartByte, fieldTypesByOwner, fileVarTypes, localFuncNames)...)
		facts.Imports = append(facts.Imports, importsFromMatch(m, src)...)
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

// enclosingDefScope walks up from n — the def's OWN function_definition
// node, not its name child, so the walk's first step lands on whatever
// CONTAINS this def, not the def itself — looking for the NEAREST
// enclosing class_definition or function_definition, skipping everything
// else (decorated_definition, block, ...) transparently. Returns
// ("function", "") for a nested def (a closure/helper — never emitted as
// an entity, see Extract's doc), ("class", ownerName) for a method, or
// ("module", "") for a top-level def.
func enclosingDefScope(n *sitter.Node, src []byte) (scopeKind string, ownerName string) {
	for p := n.Parent(); p != nil; p = p.Parent() {
		switch p.Kind() {
		case "function_definition":
			return "function", ""
		case "class_definition":
			nameNode := p.ChildByFieldName("name")
			if nameNode == nil {
				return "class", ""
			}
			return "class", text(src, nameNode)
		}
	}
	return "module", ""
}

// enclosingClassName walks up from n looking for the nearest enclosing
// class_definition REGARDLESS of how many function_definitions sit in
// between — this is what `self`'s type actually resolves to (a closure
// nested inside a method still captures the same `self` as its enclosing
// method, by Python's own closure semantics), unlike enclosingDefScope's
// NEAREST-only rule above, which is used for a different question (is
// THIS def itself nested, a method, or top-level).
func enclosingClassName(n *sitter.Node, src []byte) (string, bool) {
	for p := n.Parent(); p != nil; p = p.Parent() {
		if p.Kind() == "class_definition" {
			nameNode := p.ChildByFieldName("name")
			if nameNode == nil {
				return "", false
			}
			return text(src, nameNode), true
		}
	}
	return "", false
}

// enclosingFunctionStartByte walks up from n looking for the NEAREST
// enclosing function_definition and returns its start byte — the key
// paramTypesByFunc is indexed by, so a `self.attr = some_param`
// assignment can be cross-referenced against only ITS OWN enclosing
// function's parameters, never a same-named parameter belonging to a
// different method entirely.
func enclosingFunctionStartByte(n *sitter.Node) (uint, bool) {
	for p := n.Parent(); p != nil; p = p.Parent() {
		if p.Kind() == "function_definition" {
			sb, _ := p.ByteRange()
			return sb, true
		}
	}
	return 0, false
}

// entityFromMatch converts one collected match into a model.Entity, if
// the match is one of the entity-producing patterns. isLocal reports a
// nested def (see Extract's doc) — ent/id are zero-valued in that case;
// the caller still uses entity.name to populate localFuncNames.
func entityFromMatch(repo, file string, src []byte, m match) (ent model.Entity, id model.EntityID, isLocal bool, ok bool) {
	var kind model.Kind
	var node *sitter.Node
	switch {
	case m.captures["entity.class"] != nil:
		kind, node = model.KindClass, m.captures["entity.class"]
	case m.captures["entity.function"] != nil:
		kind, node = model.KindFunction, m.captures["entity.function"]
	default:
		return model.Entity{}, "", false, false
	}

	nameNode := m.captures["entity.name"]
	if nameNode == nil {
		return model.Entity{}, "", false, false
	}
	name := text(src, nameNode)

	qualified := file + "#" + name
	if kind == model.KindFunction {
		scopeKind, owner := enclosingDefScope(node, src)
		switch scopeKind {
		case "function":
			return model.Entity{}, "", true, false
		case "class":
			kind = model.KindMethod
			qualified = file + "#" + owner + "." + name
		}
		// pytest/unittest convention: a `test_*`-named function or method
		// is a real, ordinary `def` (unlike Jest's callback-block tests in
		// TypeScript), so this is a reclassification, not a second entity
		// — the same "go test"-style naming convention Go's own
		// isGoTestName already established for this project.
		if strings.HasPrefix(name, "test_") {
			kind = model.KindTest
		}
	}

	disambiguator := ""
	if kind == model.KindFunction || kind == model.KindMethod {
		params := m.captures["entity.params"]
		arity := 0
		if params != nil {
			arity = int(params.NamedChildCount())
		}
		disambiguator = fmt.Sprintf("arity:%d", arity)
	}

	id = model.NewEntityID(repo, kind, qualified, disambiguator)
	ent = model.Entity{
		ID:        id,
		Kind:      kind,
		Lang:      model.LangPython,
		Repo:      repo,
		Qualified: qualified,
		Name:      name,
		Anchor:    anchorFrom(file, src, node),
	}
	return ent, id, false, true
}

// refsFromMatch converts call/heritage matches into typed Refs,
// attributing each to its enclosing function/method (Ref.Src) via
// scopeByStartByte.
func refsFromMatch(repo, file string, src []byte, m match, scopeByStartByte map[uint]model.EntityID, fieldTypesByOwner map[string]map[string]string, fileVarTypes map[string]string, localFuncNames map[string]bool) []model.Ref {
	var refs []model.Ref

	if listNode := m.captures["heritage.list"]; listNode != nil {
		if ownerNode := m.captures["heritage.owner"]; ownerNode != nil {
			refs = append(refs, heritageRefsFromList(repo, file, src, ownerNode, listNode, scopeByStartByte)...)
		}
	}

	if n := m.captures["call.name"]; n != nil {
		switch {
		case m.captures["call.selfobject"] != nil:
			// `self.attr.method(...)` — call.qualified.this's shape.
			if text(src, m.captures["call.selfobject"]) != "self" {
				break
			}
			obj := text(src, m.captures["call.object"])
			target := model.RefTarget{Scope: model.ScopeQualified, Name: obj, Member: text(src, n)}
			if owner, ok := enclosingClassName(n, src); ok {
				if fields := fieldTypesByOwner[owner]; fields != nil {
					target.ReceiverType = fields[obj]
				}
			}
			refs = append(refs, model.Ref{
				Kind:   model.RefCall,
				Src:    enclosingScope(n, scopeByStartByte),
				Target: target,
				Anchor: anchorFrom(file, src, n),
			})
		case m.captures["call.object"] != nil:
			obj := text(src, m.captures["call.object"])
			target := model.RefTarget{Scope: model.ScopeQualified, Name: obj, Member: text(src, n)}
			if obj == "self" {
				// `self.method(...)` — a same-class call. self's type IS
				// the enclosing class, deterministically (by Python's own
				// closure/method-binding semantics, not a name-based
				// guess about an arbitrary identifier's type) — see
				// entities.scm's call.qualified.this doc for why this is
				// a convention-based, not syntax-enforced, check, and
				// what happens when a method's first parameter is named
				// something other than `self`.
				if owner, ok := enclosingClassName(n, src); ok {
					target.ReceiverType = owner
				}
			} else {
				target.ReceiverType = fileVarTypes[obj]
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

// heritageRefsFromList walks a class's superclasses argument_list,
// emitting one RefExtends per base class — every entry, no
// reclassification needed (unlike C#'s base_list, ADR-0023 D10: Python
// has no formal interface keyword, so every base in a (possibly
// multiple-inheritance) list is genuinely a base class).
func heritageRefsFromList(repo, file string, src []byte, ownerNode, listNode *sitter.Node, scopeByStartByte map[uint]model.EntityID) []model.Ref {
	var refs []model.Ref
	for i := uint(0); i < listNode.NamedChildCount(); i++ {
		c := listNode.NamedChild(i)
		if c == nil {
			continue
		}
		var target model.RefTarget
		switch c.Kind() {
		case "identifier":
			target = model.RefTarget{Scope: model.ScopeUnqualified, Name: text(src, c)}
		case "attribute":
			objNode := c.ChildByFieldName("object")
			attrNode := c.ChildByFieldName("attribute")
			if objNode == nil || attrNode == nil {
				continue
			}
			target = model.RefTarget{Scope: model.ScopeQualified, Name: text(src, objNode), Member: text(src, attrNode)}
		default:
			// keyword_argument (`metaclass=...`), list_splat, etc. — not a
			// base-class reference, skipped rather than guessed at.
			continue
		}
		refs = append(refs, model.Ref{
			Kind:   model.RefExtends,
			Src:    ownerID(repo, file, src, ownerNode),
			Target: target,
			Anchor: anchorFrom(file, src, c),
		})
	}
	_ = scopeByStartByte // heritage refs are always class-scoped, never inside a method body
	return refs
}

// ownerID recomputes the owning class's EntityID directly from its
// declaration node, mirroring csharp's heritageRefsFromList exactly (same
// reasoning: the class's own entity ID is deterministic from
// (repo, kind, qualified name), so no separate lookup map keyed by
// start-byte or name is needed — it's cheaper to recompute than to
// thread a third map through the whole match-processing pipeline just
// for this one case).
func ownerID(repo, file string, src []byte, ownerNode *sitter.Node) model.EntityID {
	nameNode := ownerNode.ChildByFieldName("name")
	if nameNode == nil {
		return ""
	}
	qualified := file + "#" + text(src, nameNode)
	return model.NewEntityID(repo, model.KindClass, qualified, "")
}

// enclosingScope walks up from n looking for the nearest ancestor whose
// start byte is a registered function/method entity. Both a Python
// function and a method are the SAME node kind (function_definition,
// unlike C#'s separate method_declaration/constructor_declaration), so
// there is only one case to check here, unlike every other language's
// own version of this helper.
func enclosingScope(n *sitter.Node, scopeByStartByte map[uint]model.EntityID) model.EntityID {
	for p := n.Parent(); p != nil; p = p.Parent() {
		if p.Kind() != "function_definition" {
			continue
		}
		sb, _ := p.ByteRange()
		if id, ok := scopeByStartByte[sb]; ok {
			return id
		}
	}
	return ""
}

// importsFromMatch converts an import_statement or import_from_statement
// match into zero or more ImportBindings. Captured whole and parsed by
// hand (see entities.scm's import.stmt/importfrom.stmt doc) rather than
// pattern-matched further.
func importsFromMatch(m match, src []byte) []model.ImportBinding {
	if n := m.captures["import.stmt"]; n != nil {
		return parsePlainImport(n, src)
	}
	if n := m.captures["importfrom.stmt"]; n != nil {
		return parseImportFrom(n, src)
	}
	return nil
}

// parsePlainImport handles `import x.y.z` / `import x.y.z as w` (and the
// comma-separated multi-name form, `import a, b as c`, via the `name`
// field's repeatability).
func parsePlainImport(n *sitter.Node, src []byte) []model.ImportBinding {
	var out []model.ImportBinding
	cursor := n.Walk()
	defer cursor.Close()
	for _, child := range n.ChildrenByFieldName("name", cursor) {
		c := child
		switch c.Kind() {
		case "aliased_import":
			aliasNode := c.ChildByFieldName("alias")
			nameNode := c.ChildByFieldName("name")
			if aliasNode == nil || nameNode == nil {
				continue
			}
			out = append(out, model.ImportBinding{LocalName: text(src, aliasNode), Source: text(src, nameNode), IsNamespace: true})
		case "dotted_name":
			// No alias: Python binds only the TOP-level segment
			// (`import x.y.z` lets you write `x.y.z.thing`, never `y` or
			// `z` alone) — the first named child, not the whole text.
			if c.NamedChildCount() == 0 {
				continue
			}
			top := c.NamedChild(0)
			out = append(out, model.ImportBinding{LocalName: text(src, top), Source: text(src, &c), IsNamespace: true})
		}
	}
	return out
}

// parseImportFrom handles `from x.y import a, b as c`, `from . import x`,
// and `from ..pkg import y` — both absolute (dotted_name) and relative
// (relative_import: leading dots + optional dotted path) module names.
// Source encodes a relative import as literal leading dots followed by
// the (possibly empty) dotted suffix — e.g. ".", ".models", "..pkg" —
// exactly mirroring Python's own relative-import syntax as text, which
// internal/resolve/lang_python.go's resolveImportPath parses back by
// counting leading dots. `from x import *` (wildcard_import, no `name`
// field entries at all) is a documented, unhandled gap: no LocalName is
// bound to any one thing, so there is nothing this extractor can usefully
// emit for it.
func parseImportFrom(n *sitter.Node, src []byte) []model.ImportBinding {
	moduleNode := n.ChildByFieldName("module_name")
	if moduleNode == nil {
		return nil
	}
	var moduleSource string
	switch moduleNode.Kind() {
	case "relative_import":
		var dots string
		var suffix string
		for i := uint(0); i < moduleNode.ChildCount(); i++ {
			c := moduleNode.Child(i)
			if c == nil {
				continue
			}
			switch c.Kind() {
			case "import_prefix":
				dots = text(src, c)
			case "dotted_name":
				suffix = text(src, c)
			}
		}
		moduleSource = dots + suffix
	case "dotted_name":
		moduleSource = text(src, moduleNode)
	default:
		return nil
	}

	var out []model.ImportBinding
	cursor := n.Walk()
	defer cursor.Close()
	for _, child := range n.ChildrenByFieldName("name", cursor) {
		c := child
		switch c.Kind() {
		case "aliased_import":
			aliasNode := c.ChildByFieldName("alias")
			nameNode := c.ChildByFieldName("name")
			if aliasNode == nil || nameNode == nil {
				continue
			}
			out = append(out, model.ImportBinding{
				LocalName: text(src, aliasNode), Source: moduleSource,
				ImportedName: text(src, nameNode), IsDefault: false,
			})
		case "dotted_name":
			imported := text(src, &c)
			out = append(out, model.ImportBinding{
				LocalName: imported, Source: moduleSource, ImportedName: imported,
			})
		}
	}
	return out
}
