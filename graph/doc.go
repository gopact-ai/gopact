// Package graph provides typed workflow execution primitives for agent runs.
//
// A graph is compiled from named nodes and edges, then executed as an event
// stream. The package keeps state typed at the graph boundary while exposing
// stable step snapshots for checkpointing, replay, and conformance tests.
package graph
