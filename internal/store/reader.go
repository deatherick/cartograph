package store

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math"
	"os"
	"sort"

	"github.com/deatherick/cartograph/internal/model"
)

// Snapshot is a loaded, read-only graph. See format.go's package doc for
// the ReadFile-not-mmap scoping decision — the whole file is resident in
// data, and every accessor decodes fields on demand by indexing into it.
type Snapshot struct {
	Repo         string
	data         []byte
	stringsStart uint32
	entityStart  int
	outEdgeStart int
	inEdgeStart  int
	entityCount  int
	outEdgeCount int
	inEdgeCount  int
}

// Open reads and parses a snapshot written by Write.
func Open(path string) (*Snapshot, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("store: opening snapshot: %w", err)
	}
	if len(data) < headerSize || string(data[0:4]) != magic {
		return nil, fmt.Errorf("store: %s is not a valid Cartograph snapshot", path)
	}
	version := binary.LittleEndian.Uint32(data[4:8])
	if version != formatVersion {
		return nil, fmt.Errorf("store: snapshot format version %d unsupported (want %d) — reindex with `ctx index`", version, formatVersion)
	}
	repoOff := binary.LittleEndian.Uint32(data[8:12])
	entityCount := int(binary.LittleEndian.Uint32(data[12:16]))
	outEdgeCount := int(binary.LittleEndian.Uint32(data[16:20]))
	inEdgeCount := int(binary.LittleEndian.Uint32(data[20:24]))
	stringTableLen := int(binary.LittleEndian.Uint32(data[24:28]))

	stringsStart := headerSize
	entityStart := stringsStart + stringTableLen
	outEdgeStart := entityStart + entityCount*entityRecordSize
	inEdgeStart := outEdgeStart + outEdgeCount*edgeRecordSize
	wantLen := inEdgeStart + inEdgeCount*edgeRecordSize
	if len(data) < wantLen {
		return nil, fmt.Errorf("store: snapshot truncated (have %d bytes, want at least %d)", len(data), wantLen)
	}

	s := &Snapshot{
		data:         data,
		stringsStart: uint32(stringsStart),
		entityStart:  entityStart,
		outEdgeStart: outEdgeStart,
		inEdgeStart:  inEdgeStart,
		entityCount:  entityCount,
		outEdgeCount: outEdgeCount,
		inEdgeCount:  inEdgeCount,
	}
	s.Repo = s.readString(repoOff)
	return s, nil
}

// readString reads a NUL-terminated string starting at offset off within
// the string table.
func (s *Snapshot) readString(off uint32) string {
	start := int(s.stringsStart) + int(off)
	end := start
	for end < len(s.data) && s.data[end] != 0 {
		end++
	}
	return string(s.data[start:end])
}

func (s *Snapshot) entityRecordAt(i int) []byte {
	off := s.entityStart + i*entityRecordSize
	return s.data[off : off+entityRecordSize]
}

func (s *Snapshot) entityAt(i int) model.Entity {
	rec := s.entityRecordAt(i)
	return model.Entity{
		ID:         model.EntityID(hex.EncodeToString(rec[0:8])),
		Kind:       model.Kind(s.readString(binary.LittleEndian.Uint32(rec[8:12]))),
		Lang:       model.Lang(s.readString(binary.LittleEndian.Uint32(rec[12:16]))),
		Qualified:  s.readString(binary.LittleEndian.Uint32(rec[16:20])),
		Name:       s.readString(binary.LittleEndian.Uint32(rec[20:24])),
		Signature:  s.readString(binary.LittleEndian.Uint32(rec[24:28])),
		DocSummary: s.readString(binary.LittleEndian.Uint32(rec[28:32])),
		Anchor: model.Anchor{
			File:        s.readString(binary.LittleEndian.Uint32(rec[32:36])),
			StartByte:   uint(binary.LittleEndian.Uint32(rec[36:40])),
			EndByte:     uint(binary.LittleEndian.Uint32(rec[40:44])),
			StartLine:   int(binary.LittleEndian.Uint32(rec[44:48])),
			EndLine:     int(binary.LittleEndian.Uint32(rec[48:52])),
			ContentHash: s.readString(binary.LittleEndian.Uint32(rec[52:56])),
		},
	}
}

func (s *Snapshot) entityEdgeSlots(i int) (outStart, outCount, inStart, inCount uint32) {
	rec := s.entityRecordAt(i)
	return binary.LittleEndian.Uint32(rec[56:60]), binary.LittleEndian.Uint32(rec[60:64]),
		binary.LittleEndian.Uint32(rec[64:68]), binary.LittleEndian.Uint32(rec[68:72])
}

// indexOf finds the entity index for id via binary search over the
// ID-sorted entity records — the "(key)"-sorted-vector lookup ADR-0016
// gets from FlatBuffers' EntitiesByKey; here it is a plain sort.Search
// over fixed-width records.
func (s *Snapshot) indexOf(id model.EntityID) (int, bool) {
	idBytes, err := hex.DecodeString(string(id))
	if err != nil || len(idBytes) != idSize {
		return 0, false
	}
	i := sort.Search(s.entityCount, func(i int) bool {
		rec := s.entityRecordAt(i)
		return bytesCompare(rec[0:8], idBytes) >= 0
	})
	if i < s.entityCount {
		rec := s.entityRecordAt(i)
		if bytesCompare(rec[0:8], idBytes) == 0 {
			return i, true
		}
	}
	return 0, false
}

func bytesCompare(a, b []byte) int {
	for i := range a {
		if a[i] != b[i] {
			if a[i] < b[i] {
				return -1
			}
			return 1
		}
	}
	return 0
}

// Lookup returns the entity with the given ID, if present.
func (s *Snapshot) Lookup(id model.EntityID) (model.Entity, bool) {
	i, ok := s.indexOf(id)
	if !ok {
		return model.Entity{}, false
	}
	return s.entityAt(i), true
}

// All returns every entity in the snapshot. Used by ctx find (linear scan
// by name) and ctx stats — internal/search's indexed lookup (FTS5/exact/
// qualified-name) is unbuilt; see the project plan's remaining Phase 1
// scope.
func (s *Snapshot) All() []model.Entity {
	out := make([]model.Entity, s.entityCount)
	for i := range out {
		out[i] = s.entityAt(i)
	}
	return out
}

func (s *Snapshot) readEdgeRecord(base int, i int, otherIsSrc bool) model.Edge {
	off := base + i*edgeRecordSize
	rec := s.data[off : off+edgeRecordSize]
	otherIdx := binary.LittleEndian.Uint32(rec[0:4])
	kind := model.EdgeKind(s.readString(binary.LittleEndian.Uint32(rec[4:8])))
	prov := model.Provenance(s.readString(binary.LittleEndian.Uint32(rec[8:12])))
	confidence := math.Float32frombits(binary.LittleEndian.Uint32(rec[12:16]))
	evidence := s.readString(binary.LittleEndian.Uint32(rec[16:20]))

	other := s.entityAt(int(otherIdx)).ID
	e := model.Edge{Kind: kind, Provenance: prov, Confidence: confidence, Evidence: evidence}
	if otherIsSrc {
		e.Src = other
	} else {
		e.Dst = other
	}
	return e
}

// FanOut returns id's outgoing edges — an O(1) slice into the out-edge
// array once id's entity index is found (one binary search).
func (s *Snapshot) FanOut(id model.EntityID) []model.Edge {
	i, ok := s.indexOf(id)
	if !ok {
		return nil
	}
	start, count, _, _ := s.entityEdgeSlots(i)
	out := make([]model.Edge, count)
	for j := uint32(0); j < count; j++ {
		e := s.readEdgeRecord(s.outEdgeStart, int(start+j), false)
		e.Src = id
		out[j] = e
	}
	return out
}

// FanIn returns id's incoming edges.
func (s *Snapshot) FanIn(id model.EntityID) []model.Edge {
	i, ok := s.indexOf(id)
	if !ok {
		return nil
	}
	_, _, start, count := s.entityEdgeSlots(i)
	out := make([]model.Edge, count)
	for j := uint32(0); j < count; j++ {
		e := s.readEdgeRecord(s.inEdgeStart, int(start+j), true)
		e.Dst = id
		out[j] = e
	}
	return out
}

// Related does the same depth-limited BFS as graph.Graph.Related, reading
// directly from the snapshot instead of an in-memory adjacency map — the
// whole reason this package exists: repeat queries pay one file read and
// binary searches, not a full reparse+reresolve.
func (s *Snapshot) Related(start model.EntityID, maxDepth int) []model.RelatedEntity {
	if maxDepth <= 0 {
		maxDepth = 2
	}
	visited := map[model.EntityID]bool{start: true}
	var out []model.RelatedEntity

	type frontierItem struct {
		id    model.EntityID
		depth int
	}
	frontier := []frontierItem{{start, 0}}

	for len(frontier) > 0 {
		cur := frontier[0]
		frontier = frontier[1:]
		if cur.depth >= maxDepth {
			continue
		}
		neighbors := append(s.FanOut(cur.id), s.FanIn(cur.id)...)
		for _, edge := range neighbors {
			next := edge.Dst
			if next == cur.id || next == "" {
				next = edge.Src
			}
			if next == "" || visited[next] {
				continue
			}
			visited[next] = true
			ent, ok := s.Lookup(next)
			if !ok {
				continue
			}
			out = append(out, model.RelatedEntity{Entity: ent, Depth: cur.depth + 1, Via: edge})
			frontier = append(frontier, frontierItem{next, cur.depth + 1})
		}
	}
	return out
}

// Upstream does a depth-limited BFS following ONLY incoming edges — the
// transitive set of everything that (directly or indirectly) depends on
// start. This is impact analysis's actual traversal direction
// (internal/service.Impact, Phase 4): Related answers "what's near this
// entity" (both directions, depth 2 by default, tuned for interactive
// exploration); Upstream answers "what breaks if I change this entity" —
// callers, and their callers, and so on.
//
// maxDepth<=0 means UNLIMITED depth (the full transitive closure), unlike
// Related's default of 2 — a real blast radius should not silently stop
// at an arbitrary distance. Safe against cycles via the same visited-set
// BFS every traversal in this file already uses.
func (s *Snapshot) Upstream(start model.EntityID, maxDepth int) []model.RelatedEntity {
	unlimited := maxDepth <= 0
	visited := map[model.EntityID]bool{start: true}
	var out []model.RelatedEntity

	type frontierItem struct {
		id    model.EntityID
		depth int
	}
	frontier := []frontierItem{{start, 0}}

	for len(frontier) > 0 {
		cur := frontier[0]
		frontier = frontier[1:]
		if !unlimited && cur.depth >= maxDepth {
			continue
		}
		for _, edge := range s.FanIn(cur.id) {
			next := edge.Src
			if next == "" || visited[next] {
				continue
			}
			visited[next] = true
			ent, ok := s.Lookup(next)
			if !ok {
				continue
			}
			out = append(out, model.RelatedEntity{Entity: ent, Depth: cur.depth + 1, Via: edge})
			frontier = append(frontier, frontierItem{next, cur.depth + 1})
		}
	}
	return out
}
