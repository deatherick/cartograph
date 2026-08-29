package store

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"

	"github.com/deatherick/cartograph/internal/graph"
	"github.com/deatherick/cartograph/internal/model"
)

// stringTable interns strings once, returning a stable byte offset for
// each unique value — the mechanism that makes repeated Kind/Provenance/
// EdgeKind values (and repeated qualified-name prefixes, in practice) cost
// one copy instead of one per entity, the same string-dominates-size
// lesson docs/research/04 draws from Grafel's own measurements.
type stringTable struct {
	offsets map[string]uint32
	buf     []byte
}

func newStringTable() *stringTable {
	return &stringTable{offsets: map[string]uint32{}}
}

func (t *stringTable) intern(s string) uint32 {
	if off, ok := t.offsets[s]; ok {
		return off
	}
	off := uint32(len(t.buf))
	t.buf = append(t.buf, s...)
	t.buf = append(t.buf, 0) // NUL terminator, so the reader can find the end without a length table
	t.offsets[s] = off
	return off
}

// Meta is the run-level bookkeeping persisted alongside the graph itself —
// today, just enough to reconstruct BugRate later without recomputing an
// index (see index.Stats, whose in-memory shape this mirrors and whose
// Files/Dispositions fields are its natural source at the one call site,
// service.Index).
type Meta struct {
	Files        int
	Dispositions map[model.Disposition]int
}

// Write builds a snapshot of g for repo and writes it atomically to path
// (temp file + rename, so a reader never observes a partial write — the
// same handoff pattern docs/research/08-process-architecture-and-residuals.md
// documents adopting from Grafel's ADR-0024).
func Write(path, repo string, g *graph.Graph, meta Meta) error {
	entities := make([]model.Entity, 0, len(g.Entities))
	for _, e := range g.Entities {
		entities = append(entities, e)
	}
	// Sort by raw ID bytes so the reader can binary-search by EntityID —
	// this is the "(key)"-sorted-vector trick ADR-0016 uses FlatBuffers
	// for; we get it for free from a plain sort before writing.
	sort.Slice(entities, func(i, j int) bool { return entities[i].ID < entities[j].ID })

	indexByID := make(map[model.EntityID]uint32, len(entities))
	for i, e := range entities {
		indexByID[e.ID] = uint32(i)
	}

	st := newStringTable()

	type edgeOcc struct {
		otherIndex uint32
		kind       model.EdgeKind
		prov       model.Provenance
		confidence float32
		evidence   string
	}
	outByEntity := make([][]edgeOcc, len(entities))
	inByEntity := make([][]edgeOcc, len(entities))

	for i, e := range entities {
		for _, edge := range g.FanOut(e.ID) {
			dstIdx, ok := indexByID[edge.Dst]
			if !ok {
				continue // dangling edge to an entity outside this snapshot; drop rather than corrupt the index
			}
			outByEntity[i] = append(outByEntity[i], edgeOcc{dstIdx, edge.Kind, edge.Provenance, edge.Confidence, edge.Evidence})
		}
		for _, edge := range g.FanIn(e.ID) {
			srcIdx, ok := indexByID[edge.Src]
			if !ok {
				continue
			}
			inByEntity[i] = append(inByEntity[i], edgeOcc{srcIdx, edge.Kind, edge.Provenance, edge.Confidence, edge.Evidence})
		}
	}

	var outEdges, inEdges []byte
	entityRecords := make([]byte, 0, len(entities)*entityRecordSize)

	appendEdgeRecord := func(buf []byte, occ edgeOcc) []byte {
		rec := make([]byte, edgeRecordSize)
		binary.LittleEndian.PutUint32(rec[0:4], occ.otherIndex)
		binary.LittleEndian.PutUint32(rec[4:8], st.intern(string(occ.kind)))
		binary.LittleEndian.PutUint32(rec[8:12], st.intern(string(occ.prov)))
		binary.LittleEndian.PutUint32(rec[12:16], float32bits(occ.confidence))
		binary.LittleEndian.PutUint32(rec[16:20], st.intern(occ.evidence))
		return append(buf, rec...)
	}

	for i, e := range entities {
		idBytes, err := hex.DecodeString(string(e.ID))
		if err != nil || len(idBytes) != idSize {
			return fmt.Errorf("store: entity %d has malformed EntityID %q: %w", i, e.ID, err)
		}

		outStart := uint32(len(outEdges) / edgeRecordSize)
		for _, occ := range outByEntity[i] {
			outEdges = appendEdgeRecord(outEdges, occ)
		}
		outCount := uint32(len(outByEntity[i]))

		inStart := uint32(len(inEdges) / edgeRecordSize)
		for _, occ := range inByEntity[i] {
			inEdges = appendEdgeRecord(inEdges, occ)
		}
		inCount := uint32(len(inByEntity[i]))

		rec := make([]byte, entityRecordSize)
		copy(rec[0:8], idBytes)
		binary.LittleEndian.PutUint32(rec[8:12], st.intern(string(e.Kind)))
		binary.LittleEndian.PutUint32(rec[12:16], st.intern(string(e.Lang)))
		binary.LittleEndian.PutUint32(rec[16:20], st.intern(e.Qualified))
		binary.LittleEndian.PutUint32(rec[20:24], st.intern(e.Name))
		binary.LittleEndian.PutUint32(rec[24:28], st.intern(e.Signature))
		binary.LittleEndian.PutUint32(rec[28:32], st.intern(e.DocSummary))
		binary.LittleEndian.PutUint32(rec[32:36], st.intern(e.Anchor.File))
		binary.LittleEndian.PutUint32(rec[36:40], uint32(e.Anchor.StartByte))
		binary.LittleEndian.PutUint32(rec[40:44], uint32(e.Anchor.EndByte))
		binary.LittleEndian.PutUint32(rec[44:48], uint32(e.Anchor.StartLine))
		binary.LittleEndian.PutUint32(rec[48:52], uint32(e.Anchor.EndLine))
		binary.LittleEndian.PutUint32(rec[52:56], st.intern(e.Anchor.ContentHash))
		binary.LittleEndian.PutUint32(rec[56:60], outStart)
		binary.LittleEndian.PutUint32(rec[60:64], outCount)
		binary.LittleEndian.PutUint32(rec[64:68], inStart)
		binary.LittleEndian.PutUint32(rec[68:72], inCount)
		entityRecords = append(entityRecords, rec...)
	}

	// Disposition records are written after the string table is otherwise
	// finalized by everything above, but string interning is idempotent
	// (newStringTable.intern returns the existing offset for a repeat
	// value), so appending more to st.buf here is safe — the disposition
	// strings just extend the same table any of them may already share
	// entries with (e.g. an EdgeKind string never collides with a
	// Disposition string, but interning doesn't care either way).
	dispositionKinds := make([]model.Disposition, 0, len(meta.Dispositions))
	for d := range meta.Dispositions {
		dispositionKinds = append(dispositionKinds, d)
	}
	sort.Slice(dispositionKinds, func(i, j int) bool { return dispositionKinds[i] < dispositionKinds[j] })
	dispositionRecords := make([]byte, 0, len(dispositionKinds)*dispositionRecordSize)
	for _, d := range dispositionKinds {
		rec := make([]byte, dispositionRecordSize)
		binary.LittleEndian.PutUint32(rec[0:4], st.intern(string(d)))
		binary.LittleEndian.PutUint32(rec[4:8], uint32(meta.Dispositions[d]))
		dispositionRecords = append(dispositionRecords, rec...)
	}

	header := make([]byte, headerSize)
	copy(header[0:4], magic)
	binary.LittleEndian.PutUint32(header[4:8], formatVersion)
	repoOff := st.intern(repo)
	binary.LittleEndian.PutUint32(header[8:12], repoOff)
	binary.LittleEndian.PutUint32(header[12:16], uint32(len(entities)))
	binary.LittleEndian.PutUint32(header[16:20], uint32(len(outEdges)/edgeRecordSize))
	binary.LittleEndian.PutUint32(header[20:24], uint32(len(inEdges)/edgeRecordSize))
	binary.LittleEndian.PutUint32(header[24:28], uint32(len(st.buf)))
	binary.LittleEndian.PutUint32(header[28:32], uint32(meta.Files))
	binary.LittleEndian.PutUint32(header[32:36], uint32(len(dispositionKinds)))

	full := make([]byte, 0, headerSize+len(st.buf)+len(entityRecords)+len(outEdges)+len(inEdges)+len(dispositionRecords))
	full = append(full, header...)
	full = append(full, st.buf...)
	full = append(full, entityRecords...)
	full = append(full, outEdges...)
	full = append(full, inEdges...)
	full = append(full, dispositionRecords...)

	return writeAtomic(path, full)
}

func writeAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("store: creating snapshot directory: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("store: writing temp snapshot: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp) // best-effort cleanup; the rename error is what matters
		return fmt.Errorf("store: renaming snapshot into place: %w", err)
	}
	return nil
}

func float32bits(f float32) uint32 {
	return math.Float32bits(f)
}
