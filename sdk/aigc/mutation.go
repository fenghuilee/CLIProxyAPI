package aigc

import "context"

// GenerationLifecyclePhase defines distinct interception points in the generation lifecycle.
type GenerationLifecyclePhase string

const (
	// PhaseBeforeCreate intercepts generation creation before initial store persistence (ingress transformation, TOS upload, validation).
	PhaseBeforeCreate GenerationLifecyclePhase = "BeforeCreate"

	// PhaseBeforeSubmit intercepts generation submission before provider execution (readiness checks, prompt/model mutation).
	PhaseBeforeSubmit GenerationLifecyclePhase = "BeforeSubmit"

	// PhaseBeforeComplete intercepts output artifacts and payload before final completion (egress Base64 to TOS upload).
	PhaseBeforeComplete GenerationLifecyclePhase = "BeforeComplete"

	// PhaseBeforeCancel intercepts task cancellation before cancel signals are processed.
	PhaseBeforeCancel GenerationLifecyclePhase = "BeforeCancel"

	// PhaseOnSucceeded is triggered when a task reaches terminal succeeded status (billing settlement, notifications).
	PhaseOnSucceeded GenerationLifecyclePhase = "OnSucceeded"

	// PhaseOnFailed is triggered when a task reaches terminal failed status (error logging, quota refund).
	PhaseOnFailed GenerationLifecyclePhase = "OnFailed"

	// PhaseOnCanceled is triggered when a task reaches terminal canceled status (resource cleanup).
	PhaseOnCanceled GenerationLifecyclePhase = "OnCanceled"
)

// ContentGenerationMutation specifies atomic field updates guarded by ExpectedRevision.
type ContentGenerationMutation struct {
	ExpectedRevision uint64         `json:"expected_revision"`
	Set              map[string]any `json:"set,omitempty"`
	Unset            []string       `json:"unset,omitempty"`
	Metadata         map[string]any `json:"metadata,omitempty"`
}

// GenerationMutationRequest is passed to lifecycle mutators.
type GenerationMutationRequest struct {
	Phase      GenerationLifecyclePhase `json:"phase"`
	Generation ContentGeneration        `json:"generation"`
	Draft      *ContentGenerationDraft  `json:"draft,omitempty"`
	Metadata   map[string]any           `json:"metadata,omitempty"`
}

// GenerationMutationResponse is returned by lifecycle mutators.
type GenerationMutationResponse struct {
	Draft        *ContentGenerationDraft     `json:"draft,omitempty"`
	Generation   *ContentGeneration          `json:"generation,omitempty"`
	Artifacts    []ContentGenerationArtifact `json:"artifacts,omitempty"`
	Mutation     *ContentGenerationMutation  `json:"mutation,omitempty"`
	Reject       bool                        `json:"reject,omitempty"`
	ErrorCode    string                      `json:"error_code,omitempty"`
	ErrorMessage string                      `json:"error_message,omitempty"`
}

// ContentGenerationMutator interface for plugins that intercept and modify generation lifecycle steps.
type ContentGenerationMutator interface {
	MutateContentGeneration(ctx context.Context, req GenerationMutationRequest) (GenerationMutationResponse, error)
}
