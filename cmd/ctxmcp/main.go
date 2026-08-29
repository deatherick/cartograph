// Command ctxmcp runs the Cartograph MCP server over stdio, so a coding
// agent (Claude Code or any other MCP client) can use the same
// find/inspect/related/source/context tools the CLI exposes, without a
// human relaying results by hand. See internal/mcpserver for the tool
// definitions; this binary only wires stdio and runs the server.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/deatherick/cartograph/internal/mcpserver"
	"github.com/deatherick/cartograph/internal/service"
)

func main() {
	server := mcpserver.New(service.New())
	if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		fmt.Fprintf(os.Stderr, "ctxmcp: %v\n", err)
		os.Exit(1)
	}
}
