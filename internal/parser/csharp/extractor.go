// Package csharp implements the C# extractor — Phase 3b (ADR-0023), added
// via the plug-and-play language architecture (ADR-0011) after Go
// (ADR-0010): a new Extractor here, a new LanguagePolicy
// (internal/resolve/lang_csharp.go), one registration in
// internal/index/languages.go, and NOTHING ELSE changes.
//
// Lives inside internal/parser's architecture boundary (see that
// package's doc) exactly like internal/parser/ts and
// internal/parser/golang: the only place outside internal/parser itself
// allowed to import go-tree-sitter and tree-sitter-c-sharp, and it never
// lets a sitter.Node/Tree escape its exported API.
//
// Query-driven, same bet as Phase 1/3a (queries/entities.scm), not a
// hand-written AST walk.
package csharp

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"fmt"
	"path"
	"path/filepath"
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"
	csgrammar "github.com/tree-sitter/tree-sitter-c-sharp/bindings/go"

	"github.com/deatherick/cartograph/internal/model"
	"github.com/deatherick/cartograph/internal/parser"
)

//go:embed queries/entities.scm
var entitiesQuerySrc string

var language = parser.NewLanguage("csharp", sitter.NewLanguage(csgrammar.Language()))

// Extractor implements the parser.Extractor interface for C#.
type Extractor struct {
	query *sitter.Query
}

// New compiles the entity query. Panics on a compile failure — a
// malformed embedded .scm is a packaging bug, matching
// internal/parser/ts and internal/parser/golang's own New().
func New() *Extractor {
	q, err := sitter.NewQuery(language.Raw(), entitiesQuerySrc)
	if err != nil {
		panic(fmt.Sprintf("csharp: compiling entities.scm: %v", err))
	}
	return &Extractor{query: q}
}

func (e *Extractor) Extensions() []string { return []string{".cs"} }

// typeDeclKinds are the tree-sitter node kinds that introduce a new
// type scope — what enclosingTypeName walks up looking for, and what
// heritage-list processing reads a base_list's OWNER from. Package-level
// so both can share one list.
var typeDeclKinds = map[string]bool{
	"class_declaration":     true,
	"struct_declaration":    true,
	"record_declaration":    true,
	"interface_declaration": true,
}

// Extract parses src (the file at repoRelativePath) and returns every
// entity and unresolved reference it contains. Entities are qualified by
// their DIRECTORY, not their file or their declared `namespace` — see
// queries/entities.scm's package doc for why this mirrors Go's own
// approximation (ADR-0010) rather than parsing `namespace`/
// file-scoped-namespace declarations.
func (e *Extractor) Extract(ctx context.Context, repo, repoRelativePath string, src []byte) (*model.FileFacts, error) {
	tree, err := parser.Parse(ctx, src, language)
	if err != nil {
		return nil, err
	}
	defer tree.Close()

	file := filepath.ToSlash(repoRelativePath)
	dir := path.Dir(file)
	facts := &model.FileFacts{File: file, Lang: model.LangCSharp}

	cursor := sitter.NewQueryCursor()
	defer cursor.Close()

	names := e.query.CaptureNames()
	matches := cursor.Matches(e.query, tree.Root(), src)

	// fieldTypesByOwner maps a class/struct/record/interface's NAME to its
	// field/property name -> declared type map — the C# analog of Go's
	// structFieldTypes. Keyed by name: within one file (partial classes
	// spanning multiple files are a known, documented gap — see
	// docs/research/edge-case-backlog.md), a name collision within the
	// same file is not possible.
	fieldTypesByOwner := map[string]map[string]string{}
	// fileVarTypesRaw mirrors Go's/TypeScript's own file-wide (not
	// block-scoped) variable-type map: every observed type for a
	// locally-declared name (parameter, local variable) across the whole
	// file, collapsed below to one type per name only when every
	// observation agreed.
	fileVarTypesRaw := map[string][]string{}
	scopeByStartByte := map[uint]model.EntityID{}
	// localFuncNames collects every C# local-function name (`void
	// DoThing() { ... }` declared inside a method body) — a bare call to
	// one of these is ScopeLocal, never cross-resolved, the same role
	// Go's localFuncNames plays for closures (edge-case-backlog.md B4).
	localFuncNames := map[string]bool{}

	var pending []match
	for m := matches.Next(); m != nil; m = matches.Next() {
		pending = append(pending, collectCaptures(m, names))
	}

	// testMethodStartBytes marks every method_declaration node (by start
	// byte) carrying a recognized xUnit/NUnit/MSTest attribute
	// (queries/entities.scm's test.methodnode doc) — collected in its own
	// pass, separate from the entity-producing loop below, since the
	// test.methodnode match carrying @test.attr and the entity.method
	// match producing the actual KindMethod entity are two INDEPENDENT
	// query matches for the same underlying node, in no guaranteed order
	// within `pending`.
	testMethodStartBytes := map[uint]bool{}
	for _, m := range pending {
		attr := m.captures["test.attr"]
		node := m.captures["test.methodnode"]
		if attr == nil || node == nil {
			continue
		}
		if isTestAttribute(text(src, attr)) {
			sb, _ := node.ByteRange()
			testMethodStartBytes[sb] = true
		}
	}

	// extendedTypeByMethodStartByte marks every method_declaration node
	// (by start byte) that is an extension method, mapped to the type it
	// extends (queries/entities.scm's ext.methodnode doc) — collected in
	// its own pass for the same reason testMethodStartBytes is: the
	// ext.methodnode match carrying @ext.modifier/@ext.type and the
	// entity.method match producing the actual KindMethod entity are two
	// INDEPENDENT query matches for the same underlying node.
	extendedTypeByMethodStartByte := map[uint]string{}
	for _, m := range pending {
		modifier := m.captures["ext.modifier"]
		typeNode := m.captures["ext.type"]
		node := m.captures["ext.methodnode"]
		if modifier == nil || typeNode == nil || node == nil {
			continue
		}
		if isThisModifier(text(src, modifier)) {
			sb, _ := node.ByteRange()
			extendedTypeByMethodStartByte[sb] = baseTypeName(src, typeNode)
		}
	}

	for _, m := range pending {
		if n := m.captures["receiver.fieldname"]; n != nil {
			if t := m.captures["receiver.fieldtype"]; t != nil {
				if owner, ok := enclosingTypeName(n, src); ok {
					if fieldTypesByOwner[owner] == nil {
						fieldTypesByOwner[owner] = map[string]string{}
					}
					fieldTypesByOwner[owner][text(src, n)] = baseTypeName(src, t)
				}
			}
		}
		if n := m.captures["receiver.varname"]; n != nil {
			if t := m.captures["receiver.vartype"]; t != nil {
				fileVarTypesRaw[text(src, n)] = append(fileVarTypesRaw[text(src, n)], baseTypeName(src, t))
			}
		}
		if n := m.captures["localfunc.name"]; n != nil {
			localFuncNames[text(src, n)] = true
		}

		ent, id, ok := entityFromMatch(repo, file, dir, src, m, testMethodStartBytes)
		if !ok {
			continue
		}
		facts.Entities = append(facts.Entities, ent)
		if ent.Kind == model.KindMethod || ent.Kind == model.KindTest {
			scopeByStartByte[ent.Anchor.StartByte] = id
		}
		if ent.Kind == model.KindMethod {
			if extendedType, ok := extendedTypeByMethodStartByte[ent.Anchor.StartByte]; ok {
				facts.ExtensionMethods = append(facts.ExtensionMethods, model.ExtensionMethod{
					EntityID: id, Name: ent.Name, ExtendedType: extendedType,
				})
			}
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
		facts.Refs = append(facts.Refs, refsFromMatch(repo, dir, file, src, m, scopeByStartByte, fieldTypesByOwner, fileVarTypes, localFuncNames)...)
		if ib, ok := importFromMatch(m, src); ok {
			facts.Imports = append(facts.Imports, ib)
		}
	}

	// Exposed for the resolver's cross-file receiver-type fallback
	// (internal/resolve's fieldTypesByOwner/lookupFieldTypeCrossFile doc)
	// — a partial class can legitimately declare a field in one file and
	// use it (through `this._field.Method()`) in another, which THIS
	// file's own fieldTypesByOwner (used above, same-file only) cannot
	// see. Closes the "fieldTypesByOwner only sees whichever file's own
	// declarations it was built from" half of the documented "No
	// partial-class support" gap.
	for owner, fields := range fieldTypesByOwner {
		for field, ftype := range fields {
			facts.FieldTypes = append(facts.FieldTypes, model.TypedField{Owner: owner, Field: field, Type: ftype})
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

// baseTypeName strips a generic type's argument list, keeping only the
// base name — `IAsyncRepository<Order>` becomes `IAsyncRepository`. Field/
// parameter/variable types captured as (generic_name) carry the full
// `Name<Args>` text; a bare (identifier) capture already has no argument
// list to strip, so this is a no-op for that case.
func baseTypeName(src []byte, n *sitter.Node) string {
	if n == nil {
		return ""
	}
	if n.Kind() == "generic_name" {
		if id := n.NamedChild(0); id != nil {
			return text(src, id)
		}
	}
	return text(src, n)
}

// testAttributeNames is the exact allowlist of xUnit/NUnit/MSTest
// test-method attribute names this extractor recognizes — both the bare
// form real code overwhelmingly uses (`[Fact]`) and the full form C#
// attribute syntax also permits (`[FactAttribute]`), matched exactly
// (whitelist, never a suffix/substring guess — this project's own
// anti-inference discipline, ADR-0023).
var testAttributeNames = map[string]bool{
	"Fact": true, "FactAttribute": true, // xUnit
	"Theory": true, "TheoryAttribute": true, // xUnit
	"Test": true, "TestAttribute": true, // NUnit
	"TestMethod": true, "TestMethodAttribute": true, // MSTest
}

// isTestAttribute reports whether attrText (an attribute's captured
// name — an identifier like "Fact", or a namespace-qualified name like
// "Xunit.Fact") names a known test-method attribute. Only the LAST
// dotted segment is compared, since a qualified attribute name is always
// "<namespace...>.<AttributeName>" — an exact match against
// testAttributeNames either way, never a guess.
func isTestAttribute(attrText string) bool {
	if i := strings.LastIndexByte(attrText, '.'); i >= 0 {
		attrText = attrText[i+1:]
	}
	return testAttributeNames[attrText]
}

// isThisModifier reports whether a parameter modifier's captured text is
// literally "this" — the only one of the grammar's several parameter
// modifiers (`this`, `scoped`, `ref`, `in`, `readonly`) that makes a
// method an extension method. Exact string comparison, not a guess.
func isThisModifier(modifierText string) bool {
	return modifierText == "this"
}

// enclosingTypeName walks up from n looking for the nearest enclosing
// type declaration (class/struct/record/interface) and returns its
// declared name — the C# analog of Go's enclosingTypeSpecName, used to
// attribute a method/field/property/heritage-list to the type that
// contains it.
func enclosingTypeName(n *sitter.Node, src []byte) (string, bool) {
	for p := n.Parent(); p != nil; p = p.Parent() {
		if typeDeclKinds[p.Kind()] {
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

// entityFromMatch converts one collected match into a model.Entity, if
// the match is one of the entity-producing patterns. testMethodStartBytes
// (queries/entities.scm's test.methodnode doc) reclassifies an
// otherwise-ordinary method_declaration as KindTest when it carries a
// recognized xUnit/NUnit/MSTest attribute — the C# analog of Go's own
// isGoTestName-based reclassification, done by attribute here since C#
// tests have no naming convention to key off of.
func entityFromMatch(repo, file, dir string, src []byte, m match, testMethodStartBytes map[uint]bool) (model.Entity, model.EntityID, bool) {
	var kind model.Kind
	var node *sitter.Node
	switch {
	case m.captures["entity.class"] != nil:
		kind, node = model.KindClass, m.captures["entity.class"]
	case m.captures["entity.interface"] != nil:
		kind, node = model.KindInterface, m.captures["entity.interface"]
	case m.captures["entity.enum"] != nil:
		kind, node = model.KindEnum, m.captures["entity.enum"]
	case m.captures["entity.typealias"] != nil:
		kind, node = model.KindTypeAlias, m.captures["entity.typealias"]
	case m.captures["entity.method"] != nil:
		kind, node = model.KindMethod, m.captures["entity.method"]
	case m.captures["entity.property"] != nil:
		kind, node = model.KindProperty, m.captures["entity.property"]
	default:
		return model.Entity{}, "", false
	}

	if kind == model.KindMethod {
		if sb, _ := node.ByteRange(); testMethodStartBytes[sb] {
			kind = model.KindTest
		}
	}

	nameNode := m.captures["entity.name"]
	if nameNode == nil {
		return model.Entity{}, "", false
	}
	name := text(src, nameNode)
	// A constructor's declared name is syntactically required to equal
	// its class's own name — found while validating against a real repo
	// (ADR-0023, eShopOnWeb): every constructor's bare Name collided with
	// its own class's bare Name in the resolver's byName index, making
	// `ctx find OrderController` (or any lookup by that class's name)
	// report a spurious ambiguity between the class and its own
	// constructor. Renamed to "ctor" (without the leading dot .NET's own
	// reflection API uses for this, `.ctor` — kept dot-free here only so
	// Entity.Qualified reads as "Dir#Class.ctor", not "Dir#Class..ctor"),
	// which also means every class's constructor now shares one bare
	// name repo-wide — an acceptable, deliberate trade: nobody looks up a
	// constructor by bare name expecting a unique match, and Qualified
	// (which callers actually use for a class's OWN constructor) stays
	// unique per class.
	if node.Kind() == "constructor_declaration" {
		name = "ctor"
	}

	qualified := dir + "#" + name
	disambiguator := ""
	if kind == model.KindMethod || kind == model.KindProperty || kind == model.KindTest {
		owner, ok := enclosingTypeName(nameNode, src)
		if !ok {
			// A method/property with no enclosing type (shouldn't happen
			// in valid C#, but defensively skipped rather than
			// mis-qualified — same discipline Go's entityFromMatch
			// applies when entity.owner is missing).
			return model.Entity{}, "", false
		}
		qualified = dir + "#" + owner + "." + name
	}
	if kind == model.KindMethod || kind == model.KindTest {
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
		Lang:      model.LangCSharp,
		Repo:      repo,
		Qualified: qualified,
		Name:      name,
		Anchor:    anchorFrom(file, src, node),
	}
	return ent, id, true
}

// refsFromMatch converts call/heritage matches into typed Refs,
// attributing each to its enclosing method (Ref.Src) via
// scopeByStartByte.
func refsFromMatch(repo, dir, file string, src []byte, m match, scopeByStartByte map[uint]model.EntityID, fieldTypesByOwner map[string]map[string]string, fileVarTypes map[string]string, localFuncNames map[string]bool) []model.Ref {
	var refs []model.Ref

	if listNode := m.captures["heritage.list"]; listNode != nil {
		if ownerNode := m.captures["heritage.owner"]; ownerNode != nil {
			refs = append(refs, heritageRefsFromList(repo, dir, file, src, ownerNode, listNode, scopeByStartByte)...)
		}
	}

	if n := m.captures["newtype.target"]; n != nil {
		refs = append(refs, model.Ref{
			Kind:   model.RefTypeUse,
			Src:    enclosingScope(n, scopeByStartByte),
			Target: model.RefTarget{Scope: model.ScopeUnqualified, Name: text(src, n)},
			Anchor: anchorFrom(file, src, n),
		})
	}

	if n := m.captures["call.name"]; n != nil {
		switch {
		case m.captures["call.object"] != nil:
			obj := text(src, m.captures["call.object"])
			target := model.RefTarget{Scope: model.ScopeQualified, Name: obj, Member: text(src, n)}
			if m.captures["call.qualified.this"] != nil {
				// `this._field.Method()`: _field's type comes from the
				// enclosing type's own field/property map — an exact,
				// unambiguous scope (there is only one enclosing type per
				// call site).
				if owner, ok := enclosingTypeName(n, src); ok {
					if fields := fieldTypesByOwner[owner]; fields != nil {
						target.ReceiverType = fields[obj]
					}
				}
			} else {
				// Bare `obj.Method()`: obj's type comes from the
				// file-wide typed-variable map (a parameter, local
				// variable, or constructor-injected field accessed
				// without a `this.` prefix — the common case).
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

// heritageRefsFromList walks a base_list node's named children, emitting
// one RefExtends per base class / implemented interface. Every entry is
// emitted as RefExtends regardless of whether it turns out to be a class
// or an interface — see queries/entities.scm's heritage.list doc for why
// this is deliberate (C#'s grammar does not distinguish the two
// syntactically) and internal/resolve's reclassifyHeritageEdge for the
// deterministic, resolved-data-based correction applied afterward.
func heritageRefsFromList(repo, dir, file string, src []byte, ownerNode, listNode *sitter.Node, scopeByStartByte map[uint]model.EntityID) []model.Ref {
	ownerName := ownerNode.ChildByFieldName("name")
	if ownerName == nil {
		return nil
	}
	ownerKind := model.KindClass
	if ownerNode.Kind() == "interface_declaration" {
		ownerKind = model.KindInterface
	}
	ownerQualified := dir + "#" + text(src, ownerName)
	ownerID := model.NewEntityID(repo, ownerKind, ownerQualified, "")

	var refs []model.Ref
	for i := uint(0); i < listNode.NamedChildCount(); i++ {
		c := listNode.NamedChild(i)
		if c == nil {
			continue
		}
		var targetName string
		switch c.Kind() {
		case "identifier":
			targetName = text(src, c)
		case "qualified_name":
			if nameField := c.ChildByFieldName("name"); nameField != nil {
				targetName = text(src, nameField)
			}
		case "generic_name":
			if id := c.NamedChild(0); id != nil {
				targetName = text(src, id)
			}
		default:
			// argument_list (a primary constructor's base-call arguments,
			// `: Base(x, y)`), primary_constructor_base_type — neither
			// names a type this project's fixed edge taxonomy needs a
			// separate ref for; skipped, not guessed at.
			continue
		}
		if targetName == "" {
			continue
		}
		refs = append(refs, model.Ref{
			Kind:   model.RefExtends,
			Src:    ownerID,
			Target: model.RefTarget{Scope: model.ScopeUnqualified, Name: targetName},
			Anchor: anchorFrom(file, src, c),
		})
	}
	_ = scopeByStartByte // heritage refs are always type-scoped, never inside a method body
	return refs
}

// enclosingScope walks up from n looking for the nearest ancestor whose
// start byte is a registered method entity (methods and constructors
// both use model.KindMethod, so both are registered the same way).
func enclosingScope(n *sitter.Node, scopeByStartByte map[uint]model.EntityID) model.EntityID {
	for p := n.Parent(); p != nil; p = p.Parent() {
		switch p.Kind() {
		case "method_declaration", "constructor_declaration":
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

// importFromMatch converts a using_directive node into an ImportBinding,
// or ok=false for a form this V0 extractor deliberately does not handle
// (`using static X;` — brings a class's STATIC members into unqualified
// scope, a documented gap; see queries/entities.scm's using.stmt doc for
// why this is parsed by hand rather than pattern-matched).
func importFromMatch(m match, src []byte) (model.ImportBinding, bool) {
	n := m.captures["using.stmt"]
	if n == nil {
		return model.ImportBinding{}, false
	}
	var hasEquals, isStatic bool
	for i := uint(0); i < n.ChildCount(); i++ {
		c := n.Child(i)
		if c == nil {
			continue
		}
		switch c.Kind() {
		case "=":
			hasEquals = true
		case "static":
			isStatic = true
		}
	}
	if isStatic {
		return model.ImportBinding{}, false
	}
	if hasEquals {
		// `using Alias = Some.Namespace.Type;` — a namespace/type alias.
		aliasNode := n.ChildByFieldName("name")
		if aliasNode == nil || n.NamedChildCount() < 2 {
			return model.ImportBinding{}, false
		}
		target := n.NamedChild(n.NamedChildCount() - 1)
		if target == nil {
			return model.ImportBinding{}, false
		}
		return model.ImportBinding{LocalName: text(src, aliasNode), Source: text(src, target), IsNamespace: true}, true
	}
	// Plain form: `using Some.Namespace;` — brings every type in that
	// namespace into unqualified scope for this file. Modeled with an
	// empty LocalName (no ONE specific name is bound — every class in the
	// namespace becomes visible); this ImportBinding never participates
	// in the core pipeline's LocalName-matching tiers, only in
	// internal/resolve/lang_csharp.go's SameScopeFiles, which reads
	// fe.imports directly.
	ns := n.NamedChild(0)
	if ns == nil {
		return model.ImportBinding{}, false
	}
	return model.ImportBinding{Source: text(src, ns), IsNamespace: true}, true
}
