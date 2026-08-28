package aigc

import (
	"context"
	"time"
)

// GenerationCreateRequest parameters for creating a new generation record.
type GenerationCreateRequest struct {
	Draft      ContentGenerationDraft      `json:"draft"`
	Artifacts  []ContentGenerationArtifact `json:"artifacts,omitempty"`
	InitialSet map[string]any              `json:"initial_set,omitempty"`
}

// GenerationGetRequest parameters for fetching a generation.
type GenerationGetRequest struct {
	ID string `json:"id"`
}

// GenerationPatchRequest parameters for atomic CAS updates.
type GenerationPatchRequest struct {
	ID               string                      `json:"id"`
	ExpectedRevision uint64                      `json:"expected_revision"`
	Set              map[string]any              `json:"set,omitempty"`
	Unset            []string                    `json:"unset,omitempty"`
	Metadata         map[string]any              `json:"metadata,omitempty"`
	Events           []ContentGenerationEvent    `json:"events,omitempty"`
	Artifacts        []ContentGenerationArtifact `json:"artifacts,omitempty"`
}

// GenerationClaimRequest parameters for claiming active tasks for recovery.
type GenerationClaimRequest struct {
	WorkerID      string        `json:"worker_id"`
	LeaseDuration time.Duration `json:"lease_duration"`
	Limit         int           `json:"limit"`
	Kinds         []ContentKind `json:"kinds,omitempty"`
}

// GenerationReleaseRequest parameters for releasing a lease.
type GenerationReleaseRequest struct {
	ID       string `json:"id"`
	WorkerID string `json:"worker_id"`
}

// GenerationDeleteExpiredRequest parameters for cleaning up expired generations and artifacts.
type GenerationDeleteExpiredRequest struct {
	Before time.Time `json:"before"`
	Limit  int       `json:"limit"`
}

// ContentGenerationStore defines the persistence contract for content generations.
type ContentGenerationStore interface {
	Create(ctx context.Context, req GenerationCreateRequest) (ContentGeneration, error)
	Get(ctx context.Context, req GenerationGetRequest) (ContentGeneration, error)
	Patch(ctx context.Context, req GenerationPatchRequest) (ContentGeneration, error)
	Claim(ctx context.Context, req GenerationClaimRequest) ([]ContentGeneration, error)
	Release(ctx context.Context, req GenerationReleaseRequest) error
	DeleteExpired(ctx context.Context, req GenerationDeleteExpiredRequest) (int64, error)
}
