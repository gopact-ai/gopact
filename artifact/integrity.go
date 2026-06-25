package artifact

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/gopact-ai/gopact"
)

// ErrIntegrityMismatch is returned when an artifact payload does not match its reference metadata.
var ErrIntegrityMismatch = errors.New("artifact: integrity mismatch")

const (
	// VerificationCheckArtifactIntegrity is the standard check ID for artifact payload integrity.
	VerificationCheckArtifactIntegrity = "artifact-integrity"

	// VerificationEvidenceTypeArtifact is the standard evidence type for artifact refs.
	VerificationEvidenceTypeArtifact = "artifact"
)

// Getter reads artifacts by id.
type Getter interface {
	Get(ctx context.Context, id string) (gopact.Artifact, error)
}

// VerifyRef reads an artifact and verifies it against the supplied reference metadata.
func VerifyRef(ctx context.Context, getter Getter, ref gopact.ArtifactRef) (gopact.Artifact, error) {
	if ctx == nil {
		ctx = context.TODO()
	}
	if err := ctx.Err(); err != nil {
		return gopact.Artifact{}, err
	}
	if getter == nil {
		return gopact.Artifact{}, errors.New("artifact: getter is nil")
	}
	if ref.ID == "" {
		return gopact.Artifact{}, errors.New("artifact: ref id is required")
	}

	artifact, err := getter.Get(ctx, ref.ID)
	if err != nil {
		return gopact.Artifact{}, err
	}
	if ref.Size > 0 && int64(len(artifact.Content)) != ref.Size {
		return gopact.Artifact{}, fmt.Errorf("%w: artifact %q size got %d want %d", ErrIntegrityMismatch, ref.ID, len(artifact.Content), ref.Size)
	}
	if ref.SHA256 != "" && artifactSHA256(artifact.Content) != ref.SHA256 {
		return gopact.Artifact{}, fmt.Errorf("%w: artifact %q sha256 mismatch", ErrIntegrityMismatch, ref.ID)
	}
	return artifact, nil
}

// VerifyRefs reads and verifies artifact refs in input order.
func VerifyRefs(ctx context.Context, getter Getter, refs []gopact.ArtifactRef) ([]gopact.Artifact, error) {
	if len(refs) == 0 {
		return nil, nil
	}
	artifacts := make([]gopact.Artifact, 0, len(refs))
	for _, ref := range refs {
		artifact, err := VerifyRef(ctx, getter, ref)
		if err != nil {
			return artifacts, err
		}
		artifacts = append(artifacts, artifact)
	}
	return artifacts, nil
}

// RecordVerifyRefs verifies artifact refs and records the outcome as a verification check.
func RecordVerifyRefs(ctx context.Context, recorder *gopact.VerificationRecorder, getter Getter, refs []gopact.ArtifactRef) ([]gopact.Artifact, error) {
	if recorder == nil {
		return nil, errors.New("artifact: verification recorder is nil")
	}

	artifacts, verifyErr := VerifyRefs(ctx, getter, refs)
	check := artifactIntegrityCheck(refs, verifyErr)
	if err := recorder.Record(check); err != nil {
		if verifyErr != nil {
			return artifacts, errors.Join(verifyErr, err)
		}
		return artifacts, err
	}
	return artifacts, verifyErr
}

func artifactIntegrityCheck(refs []gopact.ArtifactRef, verifyErr error) gopact.VerificationCheck {
	status := gopact.VerificationStatusPassed
	summary := fmt.Sprintf("verified %d artifact refs", len(refs))
	evidence := artifactIntegrityEvidence(refs)
	metadata := map[string]any{"artifact_count": len(refs)}

	if len(refs) == 0 {
		status = gopact.VerificationStatusSkipped
		summary = "no artifact refs to verify"
		evidence = nil
	}
	if verifyErr != nil {
		status = gopact.VerificationStatusFailed
		summary = "artifact integrity verification failed"
		metadata["error"] = verifyErr.Error()
	}

	return gopact.VerificationCheck{
		ID:       VerificationCheckArtifactIntegrity,
		Name:     "artifact integrity",
		Status:   status,
		Summary:  summary,
		Evidence: evidence,
		Metadata: metadata,
	}
}

func artifactIntegrityEvidence(refs []gopact.ArtifactRef) []gopact.VerificationEvidence {
	if len(refs) == 0 {
		return nil
	}
	evidence := make([]gopact.VerificationEvidence, 0, len(refs))
	for i, ref := range refs {
		evidenceRef := ref.ID
		if evidenceRef == "" {
			evidenceRef = fmt.Sprintf("artifact_ref:%d", i)
		}
		evidence = append(evidence, gopact.VerificationEvidence{
			Type:    VerificationEvidenceTypeArtifact,
			Ref:     evidenceRef,
			Summary: artifactEvidenceSummary(ref),
			Metadata: map[string]any{
				"name":      ref.Name,
				"uri":       ref.URI,
				"mime_type": ref.MIMEType,
				"size":      ref.Size,
				"sha256":    ref.SHA256,
				"scope":     string(ref.Scope),
			},
		})
	}
	return evidence
}

func artifactEvidenceSummary(ref gopact.ArtifactRef) string {
	if ref.Name != "" {
		return ref.Name
	}
	if ref.ID != "" {
		return ref.ID
	}
	return "artifact ref"
}

func artifactSHA256(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}
