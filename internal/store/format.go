// Package store implements the on-disk graph snapshot: the format decided
// in docs/adr/0003-data-model.md and justified by the numbers in
// docs/research/04-storage-and-graph-format.md (Grafel's JSON→FlatBuffers
// jump: 132ms→1.6ms cold open; their own ADR-0016 left neighbor lookups at
// O(R) because edges reference entities by string ID, not index).
//
// This format fixes that: entities are fixed-width records sorted by
// EntityID for binary search, and edges reference entities by ARRAY INDEX,
// not by ID string — so once a start entity is found (one binary search),
// every further hop is a direct slice index, O(1).
//
// SCOPING DECISION (documented, not hidden): the reader loads the whole
// file into memory with os.ReadFile rather than a real mmap. True mmap
// (zero-copy, lazy paging) earns its complexity at Grafel's scale — a
// long-lived daemon holding many large repos' graphs concurrently, which
// does not exist yet (that's Phase 3). At today's scale (single CLI
// invocation, KB-to-low-MB snapshots) a full read is not the bottleneck,
// and the on-disk layout (fixed-width records, sorted ID index, CSR
// adjacency) is deliberately mmap-ready: swapping ReadFile for a real
// mmap later is a localized change to reader.go, not a format redesign.
// This mirrors the project's own principle of measuring before optimizing
// for a scale that doesn't exist yet (docs/research/04, learned from
// Grafel's own ADR-0026, which deferred edge-sharding after measuring
// their real corpus needed 5x less headroom than the original estimate).
package store

const (
	magic         = "CGF1" // Cartograph Graph, format 1
	formatVersion = uint32(1)

	idSize = 8 // EntityID is 16 hex chars = 8 raw bytes

	// entityRecordSize is the fixed byte width of one entity record. Field
	// layout (all little-endian):
	//   ID              [8]byte
	//   KindOff         uint32  -- string table offset
	//   LangOff         uint32
	//   QualifiedOff    uint32
	//   NameOff         uint32
	//   SignatureOff    uint32
	//   DocOff          uint32
	//   AnchorFileOff   uint32
	//   AnchorStartByte uint32
	//   AnchorEndByte   uint32
	//   AnchorStartLine uint32
	//   AnchorEndLine   uint32
	//   AnchorHashOff   uint32
	//   OutStart        uint32  -- index into the out-edge array
	//   OutCount        uint32
	//   InStart         uint32  -- index into the in-edge array
	//   InCount         uint32
	entityRecordSize = idSize + 16*4 // 72 bytes

	// edgeRecordSize is the fixed byte width of one edge occurrence.
	// Edges are stored TWICE — once in an array sorted/grouped by source
	// (for FanOut) and once grouped by destination (for FanIn) — trading
	// roughly 2x edge storage for O(1) traversal in both directions
	// without a second indirection table. Even doubled, this is far
	// leaner than Grafel's own measured ~325 bytes/relationship
	// (docs/research/04).
	//   OtherIndex uint32  -- Dst index (out array) or Src index (in array)
	//   KindOff    uint32  -- string table offset
	//   ProvOff    uint32  -- string table offset
	//   Confidence float32
	//   EvidenceOff uint32
	edgeRecordSize = 5 * 4 // 20 bytes

	// headerSize: magic(4) + version(4) + repoOff(4) + entityCount(4) +
	// outEdgeCount(4) + inEdgeCount(4) + stringTableLen(4) = 28 bytes,
	// followed by the string table, then entities, then out-edges, then
	// in-edges.
	headerSize = 4 + 4 + 4 + 4 + 4 + 4 + 4
)
