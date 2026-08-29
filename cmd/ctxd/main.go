// Command ctxd is the context engine daemon. Not yet implemented — arrives
// with Fase 3 (watcher, incremental indexing, project manager).
package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "ctxd: not implemented yet (scaffolding only, see docs/adr and the project plan)")
	os.Exit(1)
}
