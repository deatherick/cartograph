// Command ctx is the CLI for the context engine daemon. Not yet
// implemented — arrives with Fase 1 (find/inspect/related/source/stats)
// and Fase 2 (context/expand/session).
package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "ctx: not implemented yet (scaffolding only, see docs/adr and the project plan)")
	os.Exit(1)
}
