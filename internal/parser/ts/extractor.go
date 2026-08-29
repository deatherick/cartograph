// Package ts implements the TypeScript/JavaScript extractor: the concrete
// Extractor for Phase 1. It lives inside internal/parser's architecture
// boundary (see internal/parser's package doc) — it is the only package
// outside internal/parser itself allowed to import go-tree-sitter and
// tree-sitter-typescript, and it never lets a sitter.Node or sitter.Tree
// escape its exported API: everything it returns is model types.
//
// Extraction is query-driven (queries/entities.scm), not a hand-written
// AST walk — this is the central bet of Phase 1, made explicit because
// Grafel's own TS/JS extractor is 21,128 lines of manual traversal with
// zero .scm files (docs/research/01-parser-and-treesitter-binding.md).
package ts

import (
	"context"
	_ "embed"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"
	tsgrammar "github.com/tree-sitter/tree-sitter-typescript/bindings/go"

	"github.com/deatherick/cartograph/internal/model"
	"github.com/deatherick/cartograph/internal/parser"
)

//go:embed queries/entities.scm
var entitiesQuerySrc string

var language = parser.NewLanguage("typescript", sitter.NewLanguage(tsgrammar.LanguageTypescript()))

// Extractor implements the parser.Extractor interface for TypeScript and
// JavaScript. The compiled query is built once and reused across files.
type Extractor struct {
	query *sitter.Query
}

// New compiles the entity query. Compilation failure is a packaging bug
// (a malformed .scm shipped in the binary), not a runtime condition — it
// panics, matching how a bad embedded asset should fail loudly at startup
// rather than degrade silently per file.
func New() *Extractor {
	q, err := sitter.NewQuery(language.Raw(), entitiesQuerySrc)
	if err != nil {
		panic(fmt.Sprintf("ts: compiling entities.scm: %v", err))
	}
	return &Extractor{query: q}
}

func (e *Extractor) Extensions() []string { return []string{".ts", ".tsx"} }

// Extract parses src (the file at repoRelativePath) and returns every
// entity and unresolved reference it contains. repo is threaded through to
// compute EntityID (see docs/adr/0003-data-model.md — identity is scoped
// per repo).
func (e *Extractor) Extract(ctx context.Context, repo, repoRelativePath string, src []byte) (*model.FileFacts, error) {
	tree, err := parser.Parse(ctx, src, language)
	if err != nil {
		return nil, err
	}
	defer tree.Close()

	file := filepath.ToSlash(repoRelativePath)
	facts := &model.FileFacts{File: file, Lang: model.LangTS}

	cursor := sitter.NewQueryCursor()
	defer cursor.Close()

	names := e.query.CaptureNames()
	matches := cursor.Matches(e.query, tree.Root(), src)

	// classPropTypes maps a class's start byte to its property->type map,
	// built from constructor-parameter-properties and typed class fields
	// (queries/entities.scm's receiver.ctorprop/receiver.fieldprop
	// patterns). This is what lets `this.repo.findByEmail()` resolve: we
	// know `repo`'s declared type is `UserRepository` from
	// `constructor(private repo: UserRepository)`.
	classPropTypes := map[uint]map[string]string{}
	// fileVarTypesRaw collects every observed type for a locally-declared
	// variable name across the WHOLE FILE (not per-scope — see
	// resolveReceiverType's doc for why this is a deliberate, bounded
	// simplification, not full symbol-table scoping). Collapsed to
	// fileVarTypes after the first pass: a name keeps its type only if
	// every observation in the file agrees.
	fileVarTypesRaw := map[string][]string{}
	// scopeByStartByte maps a function_declaration/method_definition's
	// start byte to its EntityID, so refsFromMatch can attribute each
	// reference to the entity making it (Ref.Src) by walking up the AST
	// from the reference site to the nearest enclosing scope. Without
	// this, every edge would be missing its source end and the graph
	// (internal/graph) would have nothing to traverse from.
	scopeByStartByte := map[uint]model.EntityID{}

	// First pass: entities only, so extends/implements/call refs (second
	// pass) can resolve against a complete same-file entity set.
	var pending []match
	for m := matches.Next(); m != nil; m = matches.Next() {
		pending = append(pending, collectCaptures(m, names))
	}

	for _, m := range pending {
		// Receiver-type signals are collected unconditionally, BEFORE the
		// entity `continue` below — receiver.ctorprop/fieldprop/typedvar/
		// newvar matches never satisfy entityFromMatch (they carry no
		// entity.* capture), so if this ran after that check it would
		// never run at all. Found by a targeted debug test when the
		// receiver-type tier's resolved-edge count stayed at zero after
		// the resolver-side wiring landed.
		if n := m.captures["receiver.propname"]; n != nil && m.captures["receiver.proptype"] != nil {
			if classStart, ok := enclosingClassStartByte(n); ok {
				if classPropTypes[classStart] == nil {
					classPropTypes[classStart] = map[string]string{}
				}
				classPropTypes[classStart][text(src, n)] = text(src, m.captures["receiver.proptype"])
			}
		}
		if n := m.captures["receiver.varname"]; n != nil && m.captures["receiver.vartype"] != nil {
			name := text(src, n)
			fileVarTypesRaw[name] = append(fileVarTypesRaw[name], text(src, m.captures["receiver.vartype"]))
		}

		ent, id, ok := entityFromMatch(repo, file, src, m)
		if !ok {
			continue
		}
		facts.Entities = append(facts.Entities, ent)
		switch ent.Kind {
		case model.KindFunction, model.KindMethod:
			// scopeStartByte is the byte offset enclosingScope's walk-up
			// matches against, for attributing refs made INSIDE this
			// entity (Ref.Src). For a plain function_declaration/
			// method_definition that's simply the entity's own anchor.
			// For a methodassign (`X.methods.foo = function (...) {...}`)
			// the entity's anchor is the enclosing assignment_expression,
			// but the actual scope-introducing node a nested call's
			// parent-walk will hit is the function_expression on the
			// right-hand side — reached via entity.body's parent, since
			// function_expression's body field is the statement_block
			// directly.
			scopeStartByte := ent.Anchor.StartByte
			if body := m.captures["entity.body"]; m.captures["entity.methodassign"] != nil && body != nil {
				if fn := body.Parent(); fn != nil {
					scopeStartByte, _ = fn.ByteRange()
				}
			}
			scopeByStartByte[scopeStartByte] = id
		}
	}

	// Collapse fileVarTypesRaw: a variable name keeps its inferred type
	// only when every observation across the file agreed. A name reused
	// with different types in different scopes is deliberately left
	// unresolved rather than guessed — the same whitelist-not-guess
	// principle the bare-name tier already follows (docs/research/03).
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
		facts.Refs = append(facts.Refs, refsFromMatch(file, src, m, scopeByStartByte, classPropTypes, fileVarTypes)...)
		if ib, ok := importFromMatch(m, src); ok {
			facts.Imports = append(facts.Imports, ib)
		}
		if re, ok := reExportFromMatch(m, src); ok {
			facts.ReExports = append(facts.ReExports, re)
		}
		if ent, ok := testEntityFromMatch(repo, file, src, m); ok {
			facts.Entities = append(facts.Entities, ent)
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

// enclosingClassName walks up from n looking for a class_declaration and
// returns its name — used to build a dotted qualified name for methods
// (Class.method) independent of match order.
func enclosingClassName(n *sitter.Node, src []byte) (string, bool) {
	for p := n.Parent(); p != nil; p = p.Parent() {
		if p.Kind() == "class_declaration" {
			name := p.ChildByFieldName("name")
			if name == nil {
				return "", false
			}
			return text(src, name), true
		}
	}
	return "", false
}

// enclosingClassStartByte is enclosingClassName's counterpart returning the
// class_declaration's start byte instead of its name — the key
// classByStartByte/classPropTypes are indexed by, so a property
// declaration or a call site can be matched to the SAME class entity
// without re-deriving or comparing names.
func enclosingClassStartByte(n *sitter.Node) (uint, bool) {
	for p := n.Parent(); p != nil; p = p.Parent() {
		if p.Kind() == "class_declaration" {
			sb, _ := p.ByteRange()
			return sb, true
		}
	}
	return 0, false
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

// entityFromMatch converts one collected match into a model.Entity, if the
// match is one of the entity-producing patterns. Returns ok=false for
// matches that are refs/imports, handled by refsFromMatch/importFromMatch
// instead.
func entityFromMatch(repo, file string, src []byte, m match) (model.Entity, model.EntityID, bool) {
	var kind model.Kind
	var node *sitter.Node
	switch {
	case m.captures["entity.class"] != nil:
		kind, node = model.KindClass, m.captures["entity.class"]
	case m.captures["entity.interface"] != nil:
		kind, node = model.KindInterface, m.captures["entity.interface"]
	case m.captures["entity.function"] != nil:
		kind, node = model.KindFunction, m.captures["entity.function"]
	case m.captures["entity.method"] != nil:
		kind, node = model.KindMethod, m.captures["entity.method"]
	case m.captures["entity.enum"] != nil:
		kind, node = model.KindEnum, m.captures["entity.enum"]
	case m.captures["entity.typealias"] != nil:
		kind, node = model.KindTypeAlias, m.captures["entity.typealias"]
	case m.captures["entity.methodassign"] != nil:
		kind, node = model.KindMethod, m.captures["entity.methodassign"]
	default:
		return model.Entity{}, "", false
	}

	nameNode := m.captures["entity.name"]
	if nameNode == nil {
		return model.Entity{}, "", false
	}
	name := text(src, nameNode)

	qualified := file + "#" + name
	disambiguator := ""
	if kind == model.KindMethod {
		if owner := m.captures["entity.owner"]; owner != nil {
			// Prototype/schema-method assignment (`X.methods.foo = ...`):
			// the owner is captured directly, no parent-walk needed.
			qualified = file + "#" + text(src, owner) + "." + name
		} else if className, ok := enclosingClassName(nameNode, src); ok {
			qualified = file + "#" + className + "." + name
		}
	}
	if kind == model.KindFunction || kind == model.KindMethod {
		// V0 disambiguator: parameter arity. Full parameter-type-based
		// disambiguation needs type resolution, out of scope for Phase 1 —
		// documented in model.NewEntityID. This still separates the common
		// overload-by-arity case (edge-case-backlog.md A4).
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
		Lang:      model.LangTS,
		Repo:      repo,
		Qualified: qualified,
		Name:      name,
		Anchor:    anchorFrom(file, src, node),
	}
	return ent, id, true
}

// refsFromMatch converts call/extends/implements/new-expression matches
// into typed Refs, attributing each one to its enclosing function/method
// (Ref.Src) via scopeByStartByte — a ref at module scope (outside any
// function) legitimately gets an empty Src, not an error.
//
// classByStartByte/classPropTypes/fileVarTypes populate RefTarget.ReceiverType
// on qualified-call refs when the receiver's static type is known — see
// those maps' doc comments in Extract. This is what makes the resolver's
// receiver-type tier possible (docs/research/edge-case-backlog.md B13).
func refsFromMatch(file string, src []byte, m match, scopeByStartByte map[uint]model.EntityID, classPropTypes map[uint]map[string]string, fileVarTypes map[string]string) []model.Ref {
	var refs []model.Ref

	if n := m.captures["extends.target"]; n != nil {
		refs = append(refs, model.Ref{
			Kind:   model.RefExtends,
			Src:    enclosingScope(n, scopeByStartByte),
			Target: model.RefTarget{Scope: model.ScopeUnqualified, Name: text(src, n)},
			Anchor: anchorFrom(file, src, n),
		})
	}
	if n := m.captures["implements.target"]; n != nil {
		refs = append(refs, model.Ref{
			Kind:   model.RefImplements,
			Src:    enclosingScope(n, scopeByStartByte),
			Target: model.RefTarget{Scope: model.ScopeUnqualified, Name: text(src, n)},
			Anchor: anchorFrom(file, src, n),
		})
	}
	if n := m.captures["call.name"]; n != nil {
		if obj := m.captures["call.object"]; obj != nil {
			objName := text(src, obj)
			target := model.RefTarget{
				Scope:  model.ScopeQualified,
				Name:   objName,
				Member: text(src, n),
			}
			if m.captures["call.qualified.this"] != nil {
				// `this.member.method()`: member's type comes from the
				// enclosing class's constructor-parameter-properties or
				// typed fields — an exact, unambiguous scope (there is
				// only one enclosing class), so this is the
				// higher-confidence receiver-type source.
				if classStart, ok := enclosingClassStartByte(n); ok {
					if props := classPropTypes[classStart]; props != nil {
						target.ReceiverType = props[objName]
					}
				}
			} else if m.captures["call.qualified"] != nil {
				// Bare `obj.method()`: obj's type comes from the
				// file-wide typed-variable map — a looser, file-scoped
				// (not block-scoped) source; see fileVarTypes's doc.
				target.ReceiverType = fileVarTypes[objName]
			}
			refs = append(refs, model.Ref{
				Kind:   model.RefCall,
				Src:    enclosingScope(n, scopeByStartByte),
				Target: target,
				Anchor: anchorFrom(file, src, n),
			})
		} else {
			refs = append(refs, model.Ref{
				Kind:   model.RefCall,
				Src:    enclosingScope(n, scopeByStartByte),
				Target: model.RefTarget{Scope: model.ScopeUnqualified, Name: text(src, n)},
				Anchor: anchorFrom(file, src, n),
			})
		}
	}
	if n := m.captures["new.target"]; n != nil {
		refs = append(refs, model.Ref{
			Kind:   model.RefTypeUse,
			Src:    enclosingScope(n, scopeByStartByte),
			Target: model.RefTarget{Scope: model.ScopeUnqualified, Name: text(src, n)},
			Anchor: anchorFrom(file, src, n),
		})
	}
	return refs
}

// enclosingScope walks up from n looking for the nearest ancestor whose
// start byte is a registered function/method entity. Returns "" (not an
// error) when n is at module scope — a top-level call outside any
// function is a legitimate, common case.
func enclosingScope(n *sitter.Node, scopeByStartByte map[uint]model.EntityID) model.EntityID {
	for p := n.Parent(); p != nil; p = p.Parent() {
		switch p.Kind() {
		case "function_declaration", "method_definition", "function_expression":
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
	// CommonJS forms first — they use a different capture family
	// (import.cjs.*) since they come from variable_declarator + require(),
	// not import_statement.
	if cjsSrc := m.captures["import.cjs.source"]; cjsSrc != nil {
		src0 := text(src, cjsSrc)
		if n := m.captures["import.cjs.default"]; n != nil {
			// `const x = require('./m')` — binds the whole module export
			// to one name, functionally equivalent to an ESM default
			// import for resolution purposes (see model.ImportBinding.IsDefault).
			return model.ImportBinding{LocalName: text(src, n), Source: src0, IsDefault: true}, true
		}
		if n := m.captures["import.cjs.named"]; n != nil {
			// `const { a, b } = require('./m')` — one match per
			// destructured name (mirrors how ESM named imports fire once
			// per specifier). Only the shorthand form (`{ a }`) is
			// handled; `{ a: renamed }` is a documented gap (pair_pattern,
			// not shorthand_property_identifier_pattern) — lower value
			// than the common case and left for a follow-up.
			name := text(src, n)
			return model.ImportBinding{LocalName: name, Source: src0, ImportedName: name}, true
		}
	}

	source := m.captures["import.source"]
	if source == nil {
		return model.ImportBinding{}, false
	}
	src0 := text(src, source)

	if n := m.captures["import.default"]; n != nil {
		return model.ImportBinding{LocalName: text(src, n), Source: src0, IsDefault: true}, true
	}
	if n := m.captures["import.namespace"]; n != nil {
		return model.ImportBinding{LocalName: text(src, n), Source: src0, IsNamespace: true}, true
	}
	if n := m.captures["import.named"]; n != nil {
		local := text(src, n)
		imported := local
		if alias := m.captures["import.alias"]; alias != nil {
			local = text(src, alias)
		}
		return model.ImportBinding{LocalName: local, Source: src0, ImportedName: imported}, true
	}
	return model.ImportBinding{}, false
}

// reExportFromMatch handles both re-export query patterns. The named form
// (@reexport.namedstmt, carrying reexport.named) is unambiguous and always
// correct. The star form (@reexport.stmt) fires for EVERY re-export
// statement regardless of shape — including named ones, since both share
// export_statement's `source` field — so it is accepted here only when
// the captured node has no export_clause child, confirmed by walking its
// children directly rather than relying on cross-match correlation.
func reExportFromMatch(m match, src []byte) (model.ReExport, bool) {
	if n := m.captures["reexport.named"]; n != nil {
		name := text(src, n)
		alias := name
		if a := m.captures["reexport.alias"]; a != nil {
			alias = text(src, a)
		}
		source := ""
		if s := m.captures["reexport.source"]; s != nil {
			source = text(src, s)
		}
		return model.ReExport{Source: source, ExportedName: name, LocalAlias: alias}, true
	}
	if stmt := m.captures["reexport.stmt"]; stmt != nil {
		if hasChildOfKind(stmt, "export_clause") {
			return model.ReExport{}, false // named form, handled by the branch above
		}
		source := m.captures["reexport.source"]
		if source == nil {
			return model.ReExport{}, false
		}
		return model.ReExport{Source: text(src, source), IsStar: true}, true
	}
	return model.ReExport{}, false
}

func hasChildOfKind(n *sitter.Node, kind string) bool {
	cursor := n.Walk()
	defer cursor.Close()
	for _, c := range n.Children(cursor) {
		if c.Kind() == kind {
			return true
		}
	}
	return false
}

// testEntityFromMatch recognizes `it('...', ...)` / `test('...', ...)` /
// `describe('...', ...)` calls with a string-literal first argument as
// KindTest entities — the dominant Jest/Mocha convention (see
// queries/entities.scm's test.call pattern doc). Nested calls inside the
// test callback are not currently attributed to the test entity as Src
// (a documented Phase 1 gap: it would need arrow_function callbacks
// registered in scopeByStartByte the same way methodassign's
// function_expression is, not yet done).
func testEntityFromMatch(repo, file string, src []byte, m match) (model.Entity, bool) {
	fnNode := m.captures["test.fn"]
	nameNode := m.captures["test.name"]
	callNode := m.captures["test.call"]
	if fnNode == nil || nameNode == nil || callNode == nil {
		return model.Entity{}, false
	}
	fnName := text(src, fnNode)
	if fnName != "it" && fnName != "test" && fnName != "describe" {
		return model.Entity{}, false
	}
	name := text(src, nameNode)
	qualified := file + "#" + name
	id := model.NewEntityID(repo, model.KindTest, qualified, anchorKey(callNode))
	return model.Entity{
		ID:        id,
		Kind:      model.KindTest,
		Lang:      model.LangTS,
		Repo:      repo,
		Qualified: qualified,
		Name:      name,
		Anchor:    anchorFrom(file, src, callNode),
	}, true
}

// anchorKey gives KindTest entities a disambiguator — test names are not
// guaranteed unique within a file (e.g. two `it('works', ...)` under
// different `describe` blocks), and KindTest is not in Kind.overloadable's
// arity-based scheme (tests have no meaningful "arity"). The call site's
// own byte offset is a simple, always-available tiebreaker.
func anchorKey(n *sitter.Node) string {
	sb, _ := n.ByteRange()
	return fmt.Sprintf("byte:%d", sb)
}
