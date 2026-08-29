// Package render formats service-layer results into the token-dense,
// stable text the project's CLI and MCP interfaces both display — kept
// here, not duplicated between cmd/ctx and internal/mcp, per the
// project's own rule that no logic is duplicated between interfaces (see
// docs/adr and the master plan's "Restricciones permanentes"). Every
// function returns a string; callers decide where it goes (stdout, an MCP
// TextContent block, ...).
package render

import (
	"fmt"
	"strings"

	"github.com/deatherick/cartograph/internal/compile"
	"github.com/deatherick/cartograph/internal/index"
	"github.com/deatherick/cartograph/internal/model"
	"github.com/deatherick/cartograph/internal/service"
)

// IndexStats renders the result of a full index run — file/entity/edge
// counts, duration, and the disposition breakdown bug_rate is computed
// from (docs/research/02-refs-and-dispositions.md).
func IndexStats(s index.Stats) string {
	var b strings.Builder
	fmt.Fprintf(&b, "files:          %d\n", s.Files)
	fmt.Fprintf(&b, "entities:       %d\n", s.Entities)
	fmt.Fprintf(&b, "resolved edges: %d\n", s.ResolvedEdges)
	fmt.Fprintf(&b, "bug_rate:       %.1f%%\n", s.BugRate()*100)
	fmt.Fprintf(&b, "duration:       %s\n", s.Duration)
	b.WriteString("dispositions:\n")
	for _, d := range []string{"resolved", "external-known", "external-unknown", "dynamic", "ambiguous", "bug-extractor", "bug-resolver", "unimplemented", "unclassified"} {
		if n, ok := s.Dispositions[model.Disposition(d)]; ok {
			fmt.Fprintf(&b, "  %-18s %d\n", d, n)
		}
	}
	return b.String()
}

// Capsule renders the token-dense capsule format the master plan
// specifies — stable and parseable, not prose, so an agent (or a human
// scanning quickly) can find PRIMARY/RELATED sections and expand handles.
func Capsule(c *compile.Capsule) string {
	var b strings.Builder
	fmt.Fprintf(&b, "TASK  %s\n", c.Task)
	sessionPart := ""
	if c.SessionID != "" {
		sessionPart = " · SESSION " + c.SessionID
	}
	fmt.Fprintf(&b, "BUDGET %d · USED %d · CONSIDERED %d%s\n\n", c.Budget, c.Used, c.Considered, sessionPart)

	capsuleSection(&b, c, "primary", "PRIMARY")
	capsuleSection(&b, c, "related", "RELATED")

	if len(c.Items) == 0 {
		b.WriteString("(no relevant entities found for this task)\n")
	}
	return b.String()
}

func capsuleSection(b *strings.Builder, c *compile.Capsule, category, header string) {
	var items []compile.Item
	for _, it := range c.Items {
		if it.Category == category {
			items = append(items, it)
		}
	}
	if len(items) == 0 {
		return
	}
	b.WriteString(header)
	b.WriteByte('\n')
	for _, it := range items {
		tag := ""
		if it.AlreadySent {
			tag = " [already sent — handle: " + it.Handle + "]"
		}
		fmt.Fprintf(b, "%-4s %-9s %-45s [%s, %d tok]%s\n", it.Handle, it.Entity.Kind, it.Entity.Qualified, it.Level, it.Tokens, tag)
		if !it.AlreadySent && it.Level != compile.LevelName {
			for _, line := range strings.Split(strings.TrimRight(it.Text, "\n"), "\n") {
				fmt.Fprintf(b, "     %s\n", line)
			}
		}
	}
	b.WriteByte('\n')
}

// Entities renders a Find result: one line per matching entity.
func Entities(name string, entities []model.Entity) string {
	if len(entities) == 0 {
		return fmt.Sprintf("no entity named %q found\n", name)
	}
	var b strings.Builder
	for _, e := range entities {
		fmt.Fprintf(&b, "%-10s %-40s %s:%d-%d\n", e.Kind, e.Qualified, e.Anchor.File, e.Anchor.StartLine, e.Anchor.EndLine)
	}
	return b.String()
}

// Inspection renders full detail on one entity: declaration, signature if
// known, location, and fan-out/fan-in edges.
func Inspection(insp service.Inspection) string {
	var b strings.Builder
	e := insp.Entity
	fmt.Fprintf(&b, "%s %s\n", e.Kind, e.Qualified)
	if e.Signature != "" {
		fmt.Fprintf(&b, "  signature: %s\n", e.Signature)
	}
	fmt.Fprintf(&b, "  location:  %s:%d-%d\n", e.Anchor.File, e.Anchor.StartLine, e.Anchor.EndLine)
	fmt.Fprintf(&b, "  fan-out (%d) — what it calls/extends/implements/uses:\n", len(insp.FanOut))
	for _, edge := range insp.FanOut {
		fmt.Fprintf(&b, "    -> %s %s (%s, conf=%.2f)\n", edge.Kind, edge.Dst, edge.Provenance, edge.Confidence)
	}
	fmt.Fprintf(&b, "  fan-in (%d) — who calls/extends/implements/uses it:\n", len(insp.FanIn))
	for _, edge := range insp.FanIn {
		fmt.Fprintf(&b, "    <- %s %s (%s, conf=%.2f)\n", edge.Kind, edge.Src, edge.Provenance, edge.Confidence)
	}
	return b.String()
}

// Source renders an entity's source excerpt with a one-line header.
func Source(e model.Entity, src string) string {
	return fmt.Sprintf("# %s %s (%s:%d-%d)\n%s", e.Kind, e.Qualified, e.Anchor.File, e.Anchor.StartLine, e.Anchor.EndLine, src)
}

// Related renders a graph-traversal result: one line per related entity,
// with the depth and edge that reached it.
func Related(name string, depth int, related []model.RelatedEntity) string {
	if len(related) == 0 {
		return fmt.Sprintf("no related entities found within %d hop(s) of %q\n", depth, name)
	}
	var b strings.Builder
	for _, r := range related {
		fmt.Fprintf(&b, "[depth %d] %-10s %-40s via %s (%s, conf=%.2f)\n",
			r.Depth, r.Entity.Kind, r.Entity.Qualified, r.Via.Kind, r.Via.Provenance, r.Via.Confidence)
	}
	return b.String()
}

// Stats renders the lightweight snapshot summary (repo name, entity count).
func Stats(s service.Stats) string {
	return fmt.Sprintf("repo:     %s\nentities: %d\n", s.Repo, s.Entities)
}
