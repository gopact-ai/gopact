// Package trace adapts gopact runtime events into provider-neutral span records.
//
// The package intentionally does not depend on a concrete telemetry backend.
// It includes a memory exporter, a function adapter, a small HTTP/JSON
// exporter for host-owned collectors, an OTLP/HTTP JSON exporter for
// OpenTelemetry collectors, a LangSmith-compatible HTTP run exporter, and a
// LangGraph-style HTTP event exporter.
package trace
