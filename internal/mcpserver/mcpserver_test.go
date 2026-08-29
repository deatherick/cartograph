package mcpserver

import (
	"context"
	"encoding/json"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/deatherick/cartograph/internal/service"
)

func fixtureRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Join(filepath.Dir(file), "..", "..", "fixtures", "ts-basic")
}

// connect wires a client to a fresh Cartograph MCP server via in-memory
// transports — the same pattern the SDK's own tests use — so tool calls
// exercise the real JSON-RPC request/response path, not just Go function
// calls into the handlers directly.
func connect(t *testing.T) *mcp.ClientSession {
	t.Helper()
	t.Setenv("HOME", t.TempDir()) // isolate snapshot/ledger storage per test

	ctx := context.Background()
	server := New(service.New())
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "v0.0.1"}, nil)

	t1, t2 := mcp.NewInMemoryTransports()
	if _, err := server.Connect(ctx, t1, nil); err != nil {
		t.Fatalf("server.Connect: %v", err)
	}
	cs, err := client.Connect(ctx, t2, nil)
	if err != nil {
		t.Fatalf("client.Connect: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

func callTool(t *testing.T, cs *mcp.ClientSession, name string, args any) *mcp.CallToolResult {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("CallTool(%s): %v", name, err)
	}
	return res
}

func textOf(t *testing.T, res *mcp.CallToolResult) string {
	t.Helper()
	if len(res.Content) == 0 {
		t.Fatal("expected at least one content block, got none")
	}
	tc, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", res.Content[0])
	}
	return tc.Text
}

func TestMCPServer_ListTools(t *testing.T) {
	cs := connect(t)
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	want := map[string]bool{
		"context_index": true, "context_compile": true, "context_find": true,
		"context_inspect": true, "context_related": true, "context_source": true,
	}
	got := map[string]bool{}
	for _, tool := range res.Tools {
		got[tool.Name] = true
	}
	for name := range want {
		if !got[name] {
			t.Errorf("expected tool %q to be registered, got tools: %v", name, got)
		}
	}
}

func TestMCPServer_IndexThenFind(t *testing.T) {
	cs := connect(t)
	root := fixtureRoot(t)

	indexRes := callTool(t, cs, "context_index", map[string]any{"root": root})
	if indexRes.IsError {
		t.Fatalf("context_index returned an error: %s", textOf(t, indexRes))
	}
	if !strings.Contains(textOf(t, indexRes), "entities:") {
		t.Errorf("expected index output to mention entity count, got: %s", textOf(t, indexRes))
	}

	findRes := callTool(t, cs, "context_find", map[string]any{"root": root, "name": "UserService"})
	if findRes.IsError {
		t.Fatalf("context_find returned an error: %s", textOf(t, findRes))
	}
	if !strings.Contains(textOf(t, findRes), "UserService") {
		t.Errorf("expected find output to mention UserService, got: %s", textOf(t, findRes))
	}
}

func TestMCPServer_FindWithoutIndex_ReturnsToolError(t *testing.T) {
	cs := connect(t)
	root := fixtureRoot(t)

	res := callTool(t, cs, "context_find", map[string]any{"root": root, "name": "UserService"})
	if !res.IsError {
		t.Fatal("expected context_find without a prior context_index to return a tool-level error")
	}
	if !strings.Contains(textOf(t, res), "run") {
		t.Errorf("expected the error to point at running the index tool, got: %s", textOf(t, res))
	}
}

func TestMCPServer_InspectAndRelated(t *testing.T) {
	cs := connect(t)
	root := fixtureRoot(t)
	callTool(t, cs, "context_index", map[string]any{"root": root})

	inspectRes := callTool(t, cs, "context_inspect", map[string]any{"root": root, "name": "UserService", "file": "services"})
	if inspectRes.IsError {
		t.Fatalf("context_inspect returned an error: %s", textOf(t, inspectRes))
	}
	if !strings.Contains(textOf(t, inspectRes), "fan-in") {
		t.Errorf("expected inspect output to include fan-in section, got: %s", textOf(t, inspectRes))
	}

	relatedRes := callTool(t, cs, "context_related", map[string]any{"root": root, "name": "UserService", "file": "services", "depth": 2})
	if relatedRes.IsError {
		t.Fatalf("context_related returned an error: %s", textOf(t, relatedRes))
	}
}

func TestMCPServer_Source(t *testing.T) {
	cs := connect(t)
	root := fixtureRoot(t)
	callTool(t, cs, "context_index", map[string]any{"root": root})

	res := callTool(t, cs, "context_source", map[string]any{"root": root, "name": "register", "file": "services"})
	if res.IsError {
		t.Fatalf("context_source returned an error: %s", textOf(t, res))
	}
	if !strings.Contains(textOf(t, res), "register(") {
		t.Errorf("expected source output to include the function signature line, got: %s", textOf(t, res))
	}
}

func TestMCPServer_Impact(t *testing.T) {
	cs := connect(t)
	root := fixtureRoot(t)
	callTool(t, cs, "context_index", map[string]any{"root": root})

	res := callTool(t, cs, "context_impact", map[string]any{"root": root, "name": "UserService", "file": "services"})
	if res.IsError {
		t.Fatalf("context_impact returned an error: %s", textOf(t, res))
	}
	if !strings.Contains(textOf(t, res), "direct callers") {
		t.Errorf("expected impact output to include a direct-callers section, got: %s", textOf(t, res))
	}
}

func TestMCPServer_Compile(t *testing.T) {
	cs := connect(t)
	root := fixtureRoot(t)
	callTool(t, cs, "context_index", map[string]any{"root": root})

	res := callTool(t, cs, "context_compile", map[string]any{"root": root, "task": "fix a bug in UserService.register", "budget": 800})
	if res.IsError {
		t.Fatalf("context_compile returned an error: %s", textOf(t, res))
	}
	text := textOf(t, res)
	if !strings.Contains(text, "TASK") || !strings.Contains(text, "BUDGET") {
		t.Errorf("expected capsule-formatted output, got: %s", text)
	}

	// StructuredContent should also be populated (AddTool's generic
	// auto-population from the typed Out value) and round-trip as valid JSON.
	if res.StructuredContent == nil {
		t.Fatal("expected StructuredContent to be populated for context_compile")
	}
	if _, err := json.Marshal(res.StructuredContent); err != nil {
		t.Errorf("StructuredContent did not marshal to JSON: %v", err)
	}
}

func TestMCPServer_CompileWithSession_SecondCallCostsLess(t *testing.T) {
	cs := connect(t)
	root := fixtureRoot(t)
	callTool(t, cs, "context_index", map[string]any{"root": root})

	args := map[string]any{"root": root, "task": "UserService register", "budget": 2000, "session_id": "test-session"}
	first := textOf(t, callTool(t, cs, "context_compile", args))
	second := textOf(t, callTool(t, cs, "context_compile", args))

	if len(second) >= len(first) {
		t.Errorf("expected the second call in the same session to be smaller (ledger dedup): first=%d bytes second=%d bytes", len(first), len(second))
	}
}

// TestMCPServer_StructuredContentIsAlwaysAnObject is a regression test for
// a real bug a live agent demo found (docs/adr/0009-live-agent-demo.md):
// context_find and context_related originally declared Out=any and
// returned a bare slice, which mcp.AddTool's generic schema derivation
// turns into an output schema expecting a JSON object ("record") — so the
// slice value failed tools/call response validation with "expected:
// record" every single time, in a way none of this file's other tests
// caught, because they only asserted Content was present, never that
// StructuredContent actually validated against the tool's own schema.
//
// The fix (wrapping every slice-shaped result in a named struct) is
// verified here the same way the failure was found: attempt to unmarshal
// StructuredContent as a JSON object. A bare array fails this immediately;
// an object (even an empty one) succeeds.
func TestMCPServer_StructuredContentIsAlwaysAnObject(t *testing.T) {
	cs := connect(t)
	root := fixtureRoot(t)
	callTool(t, cs, "context_index", map[string]any{"root": root})

	cases := []struct {
		tool string
		args map[string]any
	}{
		{"context_find", map[string]any{"root": root, "name": "UserService"}},
		{"context_related", map[string]any{"root": root, "name": "UserService", "file": "services", "depth": 2}},
		{"context_inspect", map[string]any{"root": root, "name": "UserService", "file": "services"}},
		{"context_source", map[string]any{"root": root, "name": "register", "file": "services"}},
		{"context_compile", map[string]any{"root": root, "task": "UserService register", "budget": 500}},
		{"context_impact", map[string]any{"root": root, "name": "UserService", "file": "services"}},
	}
	for _, c := range cases {
		t.Run(c.tool, func(t *testing.T) {
			res := callTool(t, cs, c.tool, c.args)
			if res.IsError {
				t.Fatalf("%s returned an error: %s", c.tool, textOf(t, res))
			}
			raw, err := json.Marshal(res.StructuredContent)
			if err != nil {
				t.Fatalf("%s: StructuredContent did not marshal: %v", c.tool, err)
			}
			var obj map[string]any
			if err := json.Unmarshal(raw, &obj); err != nil {
				t.Fatalf("%s: StructuredContent is not a JSON object (this is exactly the bug the live demo found — a bare array/slice Out value fails tools/call schema validation with \"expected: record\"): %v\nraw: %s", c.tool, err, raw)
			}
		})
	}
}
