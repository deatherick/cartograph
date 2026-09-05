package resolve

// bareNameAllowlist is the set of bare names the resolver may bind
// optimistically when there is exactly one repo-wide candidate. Starts
// EMPTY and grows only when a fixture demonstrates a name is safe — this
// is Grafel's own hard-won policy (docs/research/03-import-resolution-and-bare-names.md,
// ADR-0011): "a name only joins the allowlist when matching it bare cannot
// plausibly be wrong." Editing this list is a reviewed, tested change —
// never a runtime knob (same discipline Grafel enforces).
var bareNameAllowlist = map[string]bool{}

// bareNameExclusions traps short, generic identifiers whose bare-name
// match is never trustworthy — copied from Grafel's own validated list
// (docs/research/03), since these are language-idiom facts, not
// project-specific tuning.
var bareNameExclusions = map[string]bool{
	"format": true, "get": true, "set": true, "run": true, "make": true,
	"init": true, "new": true, "value": true, "parse": true, "create": true,
	"build": true, "handle": true, "update": true,
	"validate": true, "check": true, "load": true, "save": true,
}

// knownGlobals is a fixed list of JS/TS runtime built-ins. A bare
// reference to one of these is never a repo entity and must not be
// flagged as a potential bug — see resolveUnqualified's tier 3.
var knownGlobals = map[string]bool{
	"console": true, "Promise": true, "Array": true, "Object": true,
	"Math": true, "JSON": true, "Map": true, "Set": true, "Date": true,
	"Error": true, "TypeError": true, "RangeError": true, "Symbol": true,
	"RegExp": true, "Boolean": true, "Number": true, "String": true,
	"parseInt": true, "parseFloat": true, "setTimeout": true,
	"setInterval": true, "clearTimeout": true, "clearInterval": true,
	"require": true, "module": true, "exports": true, "process": true,
	"Buffer": true, "isNaN": true, "isFinite": true, "encodeURIComponent": true,
	"decodeURIComponent": true, "WeakMap": true, "WeakSet": true, "Proxy": true,
	"Reflect": true, "structuredClone": true, "fetch": true, "URL": true,
	"crypto": true,
	// Jest/Mocha test-framework globals — otherwise every `describe`/`it`/
	// `expect` call in a test file (which queries/entities.scm's
	// test.call pattern also emits a bare-call Ref for, alongside the
	// KindTest entity itself) would count as a bug-worthy unresolved
	// reference. Found while adding test detection.
	"describe": true, "it": true, "test": true, "expect": true,
	"beforeEach": true, "afterEach": true, "beforeAll": true, "afterAll": true,
	"jest": true,
}

// knownPackages is a starter allowlist of well-known npm packages, mirrored
// after Grafel's own external-package allowlist concept (docs/research/09
// mentions django/react/fmt as examples for their ecosystem). Starter list
// only — grows as real repos surface more, same discipline as
// bareNameAllowlist above.
var knownPackages = map[string]bool{
	"express": true, "mongoose": true, "react": true, "react-dom": true,
	"jsonwebtoken": true, "passport": true, "slugify": true,
	"mongoose-unique-validator": true,
}

// goBuiltins is Go's predeclared identifier set (the spec's own fixed
// list — https://go.dev/ref/spec#Predeclared_identifiers) — functions,
// types, and constants available unqualified in every Go file with no
// import. A bare reference to one of these is never a repo entity and must
// not be flagged as a potential bug, exactly like knownGlobals for TS/JS,
// but a disjoint list: Go's bare-identifier resolution rules are different
// enough (see resolveUnqualified's Go branch) that reusing knownGlobals
// would be semantically wrong even where a name might coincidentally match.
var goBuiltins = map[string]bool{
	// Functions.
	"append": true, "cap": true, "clear": true, "close": true, "complex": true,
	"copy": true, "delete": true, "imag": true, "len": true, "make": true,
	"max": true, "min": true, "new": true, "panic": true, "print": true,
	"println": true, "real": true, "recover": true,
	// Types.
	"any": true, "bool": true, "byte": true, "comparable": true, "complex64": true,
	"complex128": true, "error": true, "float32": true, "float64": true,
	"int": true, "int8": true, "int16": true, "int32": true, "int64": true,
	"rune": true, "string": true, "uint": true, "uint8": true, "uint16": true,
	"uint32": true, "uint64": true, "uintptr": true,
	// Constants and the zero value.
	"true": true, "false": true, "iota": true, "nil": true,
}

// goKnownPackages is a starter allowlist of well-known Go module paths,
// mirroring knownPackages for npm above. Starter list only — populated with
// this project's own real dependencies (go.mod), grows as real repos surface
// more, same discipline as bareNameAllowlist.
//
// Deliberately does NOT list this project's own parsing-library
// dependencies (go-tree-sitter itself, and its per-language grammar
// modules) by their literal import path — found while self-hosting
// (docs/MVP.md's deferred-turned-done milestone): internal/parser's own
// architecture-boundary test (architecture_test.go) greps the WHOLE repo's
// TEXT (not just import declarations) for those exact path substrings, to
// catch a leaked binding type (docs/adr's Grafel ADR-0023 story) — writing
// either one out here, even as an inert map value never imported, trips
// that grep as a false positive. Missing from this allowlist only means
// those imports classify as ExternalUnknown instead of ExternalKnown when
// resolving THIS repo's own source — cosmetic, not a bug_rate hit (only
// BugExtractor/BugResolver count toward bug_rate; ExternalUnknown does not).
var goKnownPackages = map[string]bool{
	"github.com/modelcontextprotocol/go-sdk/mcp": true,
	"github.com/pkoukk/tiktoken-go":              true,
}

// csBuiltins is a starter list of C# identifiers that are never a repo
// entity — deliberately small, since this extractor's query patterns
// only capture (identifier) nodes as call/receiver targets, and most of
// C#'s actual keyword-level builtins (int, string, void, var, ...) are
// separate `predefined_type`/keyword tokens the grammar never surfaces as
// an `identifier`, so they can never reach a bare-name ref in the first
// place — unlike Go/TS, where a predeclared name IS an ordinary
// identifier. Grows as real repos surface more (ADR-0023), same
// discipline as goBuiltins/knownGlobals above.
var csBuiltins = map[string]bool{
	"nameof": true, "typeof": true, "default": true,
}

// csKnownNamespaces is a starter allowlist of well-known NuGet package
// namespace roots (first dotted segment), mirroring goKnownPackages for
// Go above — checked only after "System"/"Microsoft" (see
// lang_csharp.go's externalDisposition), which already cover the .NET
// BCL and first-party ASP.NET Core namespaces without needing an entry
// here. Starter list only — grows as real repos surface more.
var csKnownNamespaces = map[string]bool{
	"Ardalis": true, "Autofac": true, "AutoMapper": true, "MediatR": true,
	"Newtonsoft": true, "Xunit": true, "Moq": true, "FluentAssertions": true,
	"NSubstitute": true, "Serilog": true, "Blazored": true,
}
