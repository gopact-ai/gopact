// Package checkpoint provides checkpoint stores for resumable graph execution.
//
// The package includes memory, file, object, and row-oriented reference stores
// plus codecs and verification helpers. Production stores should implement the
// same contracts and pass the reusable conformance tests.
package checkpoint
