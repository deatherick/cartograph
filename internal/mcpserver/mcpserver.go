// Package mcpserver exposes internal/service over the Model Context
// Protocol (stdio), so a coding agent — not just a human via the CLI —
// can use Cartograph. This is the last piece of Phase 2's MVP scope (see
// docs/MVP.md): every tool here is a thin adapter with no product logic
// of its own, calling the exact same internal/service methods cmd/ctx
// calls, and rendering results through internal/render — the same "one
// service layer, no duplicated logic between interfaces" rule the
// project has followed since Phase 1.
//
// Built on the official github.com/modelcontextprotocol/go-sdk, not a
// hand-rolled JSON-RPC/stdio implementation — the protocol itself is not
// something this project has any reason to reimplement.
//
// Every handler declares a CONCRETE Out type (never `any`) so
// mcp.AddTool's generic schema derivation has a real shape to work from.
// Found the hard way, via a real agent (see docs/adr/0009-live-agent-demo.md):
// with Out=any, AddTool synthesizes a bare-object output schema, and a
// handler returning a slice (context_find, context_related) then fails
// tools/call response validation with "expected: record" — a genuine bug
// a live demo caught that the in-memory/subprocess tests in
// mcpserver_test.go had not (they never asserted on StructuredContent's
// shape for these two tools, only that Content was present).
package mcpserver

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/deatherick/cartograph/internal/model"
	"github.com/deatherick/cartograph/internal/render"
	"github.com/deatherick/cartograph/internal/service"
)

// New builds an MCP server wrapping svc, with every tool registered.
// Callers run it with server.Run(ctx, &mcp.StdioTransport{}).
func New(svc *service.Service) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: "cartograph", Version: "0.1.0"}, nil)

	mcp.AddTool(server, &mcp.Tool{
		Name: "context_index",
		Description: "Index a repository and persist a snapshot. Must be run once before any " +
			"other context_* tool can be used against that path, and again after the source " +
			"changes (no staleness detection yet). Returns file/entity/edge counts and the " +
			"disposition breakdown bug_rate is computed from.",
	}, indexHandler(svc))

	mcp.AddTool(server, &mcp.Tool{
		Name: "context_compile",
		Description: "Compile a token-budgeted context capsule for a task: the minimum useful " +
			"set of entities (classes, functions, methods, ...) ranked by relevance to the task " +
			"and fit to the budget by a real knapsack — not a truncated dump. Pass session_id " +
			"to avoid re-sending entities already delivered earlier in the same session.",
	}, compileHandler(svc))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "context_find",
		Description: "Find every entity (class, function, method, ...) with an exact bare name or qualified name match.",
	}, findHandler(svc))

	mcp.AddTool(server, &mcp.Tool{
		Name: "context_inspect",
		Description: "Full detail on one entity: its declaration, signature (if known), source " +
			"location, and fan-out/fan-in edges (what it calls/uses, and who calls/uses it).",
	}, inspectHandler(svc))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "context_related",
		Description: "Every entity within N graph hops of a named entity — callers, callees, and their transitive neighbors.",
	}, relatedHandler(svc))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "context_source",
		Description: "The exact source lines an entity spans, read from the working tree.",
	}, sourceHandler(svc))

	mcp.AddTool(server, &mcp.Tool{
		Name: "context_impact",
		Description: "Blast radius: every entity that transitively depends on a named entity (or, " +
			"if git_diff is set, on every entity a `git diff` touched), plus which of those are " +
			"tests worth running. Pass name for a single entity, or git_diff (a ref, default HEAD) " +
			"to analyze uncommitted/recent changes instead.",
	}, impactHandler(svc))

	mcp.AddTool(server, &mcp.Tool{
		Name: "context_stats",
		Description: "Summary counts for an already-indexed repository: entity/file counts and the " +
			"persisted bug_rate/disposition breakdown from the last `ctx index` run — resolved vs. " +
			"external vs. dynamic-by-design vs. an actual extractor/resolver bug, never one opaque " +
			"percentage.",
	}, statsHandler(svc))

	mcp.AddTool(server, &mcp.Tool{
		Name: "context_path",
		Description: "Shortest path (fewest graph hops) from one named entity to another — how does " +
			"A reach B, following calls/uses/extends/implements in either direction.",
	}, pathHandler(svc))

	return server
}

// errorResult reports a tool-level error inside Content with IsError set,
// per the SDK's own guidance (mcp.CallToolResult's doc comment): tool
// errors go in Content so the agent can see and self-correct, not as an
// MCP protocol-level error. Generic over Out so every handler can return
// its own zero value on the error path without an `any` escape hatch.
func errorResult[Out any](err error) (*mcp.CallToolResult, Out, error) { //nolint:unparam // Out's zero value is the point of being generic here, not a fixed always-nil return
	var zero Out
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}},
		IsError: true,
	}, zero, nil
}

type indexArgs struct {
	Root string `json:"root" jsonschema:"absolute path to the repository to index"`
}

func indexHandler(svc *service.Service) mcp.ToolHandlerFor[indexArgs, any] {
	return func(ctx context.Context, req *mcp.CallToolRequest, args indexArgs) (*mcp.CallToolResult, any, error) {
		stats, err := svc.Index(ctx, args.Root, service.RepoName(args.Root))
		if err != nil {
			return errorResult[any](err)
		}
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: render.IndexStats(stats)}}}, stats, nil
	}
}

type compileArgs struct {
	Root      string `json:"root" jsonschema:"absolute path to an already-indexed repository"`
	Task      string `json:"task" jsonschema:"free-text description of the task to compile context for"`
	Budget    int    `json:"budget,omitempty" jsonschema:"token budget for the capsule (default 2500)"`
	SessionID string `json:"session_id,omitempty" jsonschema:"session identifier for the Context Ledger; repeat calls with the same id avoid re-sending already-delivered entities"`
}

func compileHandler(svc *service.Service) mcp.ToolHandlerFor[compileArgs, any] {
	return func(ctx context.Context, req *mcp.CallToolRequest, args compileArgs) (*mcp.CallToolResult, any, error) {
		budget := args.Budget
		if budget <= 0 {
			budget = 2500
		}
		capsule, err := svc.Context(args.Root, service.RepoName(args.Root), args.Task, budget, args.SessionID)
		if err != nil {
			return errorResult[any](err)
		}
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: render.Capsule(capsule)}}}, capsule, nil
	}
}

type findArgs struct {
	Root string `json:"root" jsonschema:"absolute path to an already-indexed repository"`
	Name string `json:"name" jsonschema:"bare name, or qualified name (containing '#'), of the entity to find"`
}

// findResult wraps the slice Find returns — see the package doc: a bare
// slice as a handler's Out value fails output-schema validation, so every
// tool here returns a named struct, never a raw slice/array.
type findResult struct {
	Entities []model.Entity `json:"entities"`
}

func findHandler(svc *service.Service) mcp.ToolHandlerFor[findArgs, findResult] {
	return func(ctx context.Context, req *mcp.CallToolRequest, args findArgs) (*mcp.CallToolResult, findResult, error) {
		entities, err := svc.Find(args.Root, service.RepoName(args.Root), args.Name)
		if err != nil {
			return errorResult[findResult](err)
		}
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: render.Entities(args.Name, entities)}}}, findResult{Entities: entities}, nil
	}
}

type inspectArgs struct {
	Root string `json:"root" jsonschema:"absolute path to an already-indexed repository"`
	Name string `json:"name" jsonschema:"bare name of the entity to inspect"`
	File string `json:"file,omitempty" jsonschema:"substring to disambiguate when name matches entities in more than one file"`
}

func inspectHandler(svc *service.Service) mcp.ToolHandlerFor[inspectArgs, service.Inspection] {
	return func(ctx context.Context, req *mcp.CallToolRequest, args inspectArgs) (*mcp.CallToolResult, service.Inspection, error) {
		insp, err := svc.Inspect(args.Root, service.RepoName(args.Root), args.Name, args.File)
		if err != nil {
			return errorResult[service.Inspection](err)
		}
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: render.Inspection(insp)}}}, insp, nil
	}
}

type relatedArgs struct {
	Root  string `json:"root" jsonschema:"absolute path to an already-indexed repository"`
	Name  string `json:"name" jsonschema:"bare name of the entity to start from"`
	File  string `json:"file,omitempty" jsonschema:"substring to disambiguate when name matches entities in more than one file"`
	Depth int    `json:"depth,omitempty" jsonschema:"graph traversal depth in hops (default 2)"`
}

// relatedResult wraps the slice Related returns — see findResult's doc.
type relatedResult struct {
	Related []model.RelatedEntity `json:"related"`
}

func relatedHandler(svc *service.Service) mcp.ToolHandlerFor[relatedArgs, relatedResult] {
	return func(ctx context.Context, req *mcp.CallToolRequest, args relatedArgs) (*mcp.CallToolResult, relatedResult, error) {
		depth := args.Depth
		if depth <= 0 {
			depth = 2
		}
		related, err := svc.Related(args.Root, service.RepoName(args.Root), args.Name, args.File, depth)
		if err != nil {
			return errorResult[relatedResult](err)
		}
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: render.Related(args.Name, depth, related)}}}, relatedResult{Related: related}, nil
	}
}

type sourceArgs struct {
	Root string `json:"root" jsonschema:"absolute path to an already-indexed repository"`
	Name string `json:"name" jsonschema:"bare name of the entity whose source to read"`
	File string `json:"file,omitempty" jsonschema:"substring to disambiguate when name matches entities in more than one file"`
}

type sourceResult struct {
	Entity model.Entity `json:"entity"`
	Source string       `json:"source"`
}

func sourceHandler(svc *service.Service) mcp.ToolHandlerFor[sourceArgs, sourceResult] {
	return func(ctx context.Context, req *mcp.CallToolRequest, args sourceArgs) (*mcp.CallToolResult, sourceResult, error) {
		src, e, err := svc.Source(args.Root, service.RepoName(args.Root), args.Name, args.File)
		if err != nil {
			return errorResult[sourceResult](err)
		}
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: render.Source(e, src)}}}, sourceResult{Entity: e, Source: src}, nil
	}
}

type impactArgs struct {
	Root    string `json:"root" jsonschema:"absolute path to an already-indexed repository"`
	Name    string `json:"name,omitempty" jsonschema:"bare name of the entity to analyze; omit when using git_diff instead"`
	File    string `json:"file,omitempty" jsonschema:"substring to disambiguate when name matches entities in more than one file"`
	GitDiff string `json:"git_diff,omitempty" jsonschema:"analyze every entity a git diff touched instead of one named entity; the ref to diff against (default HEAD) — pass \"HEAD\" explicitly, or e.g. \"HEAD~3\", to select this mode"`
	Depth   int    `json:"depth,omitempty" jsonschema:"max hops to traverse (default: unlimited — the full transitive closure)"`
}

// impactResult wraps whichever of the two result shapes applies — see
// findResult's doc for why every handler here returns a named struct,
// never a bare slice/union. Exactly one of Impact/GitDiffImpact is set,
// matching which mode impactHandler ran.
type impactResult struct {
	Impact        *service.ImpactResult  `json:"impact,omitempty"`
	GitDiffImpact *service.GitDiffImpact `json:"gitDiffImpact,omitempty"`
}

func impactHandler(svc *service.Service) mcp.ToolHandlerFor[impactArgs, impactResult] {
	return func(ctx context.Context, req *mcp.CallToolRequest, args impactArgs) (*mcp.CallToolResult, impactResult, error) {
		if args.Name == "" {
			result, err := svc.ImpactFromGitDiff(args.Root, service.RepoName(args.Root), args.GitDiff, args.Depth)
			if err != nil {
				return errorResult[impactResult](err)
			}
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: render.GitDiffImpact(result)}}}, impactResult{GitDiffImpact: &result}, nil
		}
		result, err := svc.Impact(args.Root, service.RepoName(args.Root), args.Name, args.File, args.Depth)
		if err != nil {
			return errorResult[impactResult](err)
		}
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: render.Impact(result)}}}, impactResult{Impact: &result}, nil
	}
}

type statsArgs struct {
	Root string `json:"root" jsonschema:"absolute path to an already-indexed repository"`
}

func statsHandler(svc *service.Service) mcp.ToolHandlerFor[statsArgs, service.Stats] {
	return func(ctx context.Context, req *mcp.CallToolRequest, args statsArgs) (*mcp.CallToolResult, service.Stats, error) {
		stats, err := svc.Stats(args.Root, service.RepoName(args.Root))
		if err != nil {
			return errorResult[service.Stats](err)
		}
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: render.Stats(stats)}}}, stats, nil
	}
}

type pathArgs struct {
	Root     string `json:"root" jsonschema:"absolute path to an already-indexed repository"`
	From     string `json:"from" jsonschema:"bare name of the entity to start from"`
	FromFile string `json:"from_file,omitempty" jsonschema:"substring to disambiguate when from matches entities in more than one file"`
	To       string `json:"to" jsonschema:"bare name of the entity to reach"`
	ToFile   string `json:"to_file,omitempty" jsonschema:"substring to disambiguate when to matches entities in more than one file"`
}

func pathHandler(svc *service.Service) mcp.ToolHandlerFor[pathArgs, service.PathResult] {
	return func(ctx context.Context, req *mcp.CallToolRequest, args pathArgs) (*mcp.CallToolResult, service.PathResult, error) {
		result, err := svc.Path(args.Root, service.RepoName(args.Root), args.From, args.FromFile, args.To, args.ToFile)
		if err != nil {
			return errorResult[service.PathResult](err)
		}
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: render.Path(result)}}}, result, nil
	}
}
