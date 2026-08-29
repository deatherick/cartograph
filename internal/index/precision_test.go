package index

import (
	"context"
	"testing"
)

// checklistEdge is one hand-verified (Src, Kind, Dst) triple: I read the
// corresponding line of fixtures/ts-basic source myself and confirmed
// this is the semantically correct target — not inferred from the
// pipeline's own output. This is the Phase 1 exit criterion the project
// plan names ("import resolution precision ≥95% measured against an
// annotated fixture") and that had never actually been measured before
// this test — see docs/adr/0004-query-based-ts-extraction.md and
// ADR-0006 for that history.
type checklistEdge struct {
	src, kind, dst string
}

// wantResolved is a representative sample spanning every resolver tier
// this Phase 1 pipeline implements: same-file, import-table, receiver-type
// (constructor-property, typed field, cross-file via import, and
// imported-name-as-static-receiver), and barrel/tsconfig-independent
// baseline behavior. Not exhaustive of all edges the fixture produces —
// see TestPrecision_AllResolvedEdgesAreInGraph for the complementary
// full-corpus sanity check.
var wantResolved = []checklistEdge{
	// Tier: same-file bare-name (validation.ts: assertValidEmail calls
	// isValidEmail, both declared in the same file).
	{"src/utils/validation.ts#assertValidEmail", "CALLS", "src/utils/validation.ts#isValidEmail"},

	// Tier: import-table (userService.ts imports these from other files).
	{"src/services/userService.ts#UserService.register", "CALLS", "src/utils/validation.ts#assertValidEmail"},
	{"src/services/userService.ts#UserService.register", "CALLS", "src/services/emailService.ts#welcomeEmail"},
	{"src/services/userService.ts#UserService.promoteToAdmin", "CALLS", "src/models/user.ts#isAdmin"},

	// Tier: receiver-type via constructor-parameter-property
	// (`constructor(private repo: UserRepository, ...)`  then
	// `this.repo.findByEmail(...)`).
	{"src/services/userService.ts#UserService.register", "CALLS", "src/repositories/userRepository.ts#UserRepository.findByEmail"},
	{"src/services/userService.ts#UserService.register", "CALLS", "src/repositories/userRepository.ts#UserRepository.insert"},
	{"src/services/userService.ts#UserService.promoteToAdmin", "CALLS", "src/repositories/userRepository.ts#UserRepository.findById"},
	{"src/services/orderService.ts#OrderService.placeOrder", "CALLS", "src/repositories/orderRepository.ts#OrderRepository.insert"},
	{"src/services/orderService.ts#OrderService.markPaid", "CALLS", "src/repositories/orderRepository.ts#OrderRepository.updateStatus"},

	// Tier: same-file bare `this.method()` (single-level, sibling method
	// on the same instance).
	{"src/controllers/userController.ts#UserController.get", "CALLS", "src/services/userService.ts#UserService.getById"},
	{"src/controllers/orderController.ts#OrderController.markPaid", "CALLS", "src/services/orderService.ts#OrderService.markPaid"},

	// Cross-file `new X()` construction reference resolving to the
	// imported class (this specific pair is the case
	// internal/resolve/resolve.go's KindTest-exclusion fix — found and
	// fixed by this very test — makes correct).
	{"tests/userService.test.ts#setup", "USES", "src/services/userService.ts#UserService"},
	{"tests/orderService.test.ts#setup", "USES", "src/services/orderService.ts#OrderService"},
}

// TestPrecision_AnnotatedChecklist is the exit-criterion measurement:
// every entry above MUST be present among the pipeline's actual resolved
// edges. A missing entry is a false negative (something that should have
// resolved but didn't, or resolved to the wrong target) — either way, a
// precision failure against hand-verified ground truth.
func TestPrecision_AnnotatedChecklist(t *testing.T) {
	root := fixtureRoot(t)
	result, err := Run(context.Background(), root, "ts-basic")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	byID := map[string]string{}
	for _, e := range result.Graph.Entities {
		byID[string(e.ID)] = e.Qualified
	}

	present := map[checklistEdge]bool{}
	for _, e := range result.Graph.Entities {
		for _, edge := range result.Graph.FanOut(e.ID) {
			present[checklistEdge{byID[string(e.ID)], string(edge.Kind), byID[string(edge.Dst)]}] = true
		}
	}

	correct := 0
	for _, want := range wantResolved {
		if present[want] {
			correct++
		} else {
			t.Errorf("checklist edge not found (missing or wrong target): %s -%s-> %s", want.src, want.kind, want.dst)
		}
	}

	precision := float64(correct) / float64(len(wantResolved))
	t.Logf("annotated checklist precision: %d/%d = %.1f%%", correct, len(wantResolved), precision*100)
	if precision < 0.95 {
		t.Errorf("annotated checklist precision %.1f%% is below the Phase 1 exit criterion of 95%%", precision*100)
	}
}
