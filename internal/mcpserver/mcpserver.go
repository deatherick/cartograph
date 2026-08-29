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
package mcpserver

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

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

	return server
}

// errorResult reports a tool-level error inside Content with IsError set,
// per the SDK's own guidance (mcp.CallToolResult's doc comment): tool
// errors go in Content so the agent can see and self-correct, not as an
// MCP protocol-level error.
func errorResult(err error) (*mcp.CallToolResult, any, error) {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}},
		IsError: true,
	}, nil, nil
}

type indexArgs struct {
	Root string `json:"root" jsonschema:"absolute path to the repository to index"`
}

func indexHandler(svc *service.Service) mcp.ToolHandlerFor[indexArgs, any] {
	return func(ctx context.Context, req *mcp.CallToolRequest, args indexArgs) (*mcp.CallToolResult, any, error) {
		stats, err := svc.Index(ctx, args.Root, service.RepoName(args.Root))
		if err != nil {
			return errorResult(err)
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
			return errorResult(err)
		}
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: render.Capsule(capsule)}}}, capsule, nil
	}
}

type findArgs struct {
	Root string `json:"root" jsonschema:"absolute path to an already-indexed repository"`
	Name string `json:"name" jsonschema:"bare name, or qualified name (containing '#'), of the entity to find"`
}

func findHandler(svc *service.Service) mcp.ToolHandlerFor[findArgs, any] {
	return func(ctx context.Context, req *mcp.CallToolRequest, args findArgs) (*mcp.CallToolResult, any, error) {
		entities, err := svc.Find(args.Root, service.RepoName(args.Root), args.Name)
		if err != nil {
			return errorResult(err)
		}
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: render.Entities(args.Name, entities)}}}, entities, nil
	}
}

type inspectArgs struct {
	Root string `json:"root" jsonschema:"absolute path to an already-indexed repository"`
	Name string `json:"name" jsonschema:"bare name of the entity to inspect"`
	File string `json:"file,omitempty" jsonschema:"substring to disambiguate when name matches entities in more than one file"`
}

func inspectHandler(svc *service.Service) mcp.ToolHandlerFor[inspectArgs, any] {
	return func(ctx context.Context, req *mcp.CallToolRequest, args inspectArgs) (*mcp.CallToolResult, any, error) {
		insp, err := svc.Inspect(args.Root, service.RepoName(args.Root), args.Name, args.File)
		if err != nil {
			return errorResult(err)
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

func relatedHandler(svc *service.Service) mcp.ToolHandlerFor[relatedArgs, any] {
	return func(ctx context.Context, req *mcp.CallToolRequest, args relatedArgs) (*mcp.CallToolResult, any, error) {
		depth := args.Depth
		if depth <= 0 {
			depth = 2
		}
		related, err := svc.Related(args.Root, service.RepoName(args.Root), args.Name, args.File, depth)
		if err != nil {
			return errorResult(err)
		}
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: render.Related(args.Name, depth, related)}}}, related, nil
	}
}

type sourceArgs struct {
	Root string `json:"root" jsonschema:"absolute path to an already-indexed repository"`
	Name string `json:"name" jsonschema:"bare name of the entity whose source to read"`
	File string `json:"file,omitempty" jsonschema:"substring to disambiguate when name matches entities in more than one file"`
}

type sourceResult struct {
	Entity any    `json:"entity"`
	Source string `json:"source"`
}

func sourceHandler(svc *service.Service) mcp.ToolHandlerFor[sourceArgs, any] {
	return func(ctx context.Context, req *mcp.CallToolRequest, args sourceArgs) (*mcp.CallToolResult, any, error) {
		src, e, err := svc.Source(args.Root, service.RepoName(args.Root), args.Name, args.File)
		if err != nil {
			return errorResult(err)
		}
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: render.Source(e, src)}}}, sourceResult{Entity: e, Source: src}, nil
	}
}

