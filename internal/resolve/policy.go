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
