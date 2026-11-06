package driver

import (
	"encoding/json"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/aigc"
)

// BuildCompletedSubmitResult builds a completed GenerationSubmitResult for synchronous generation.
func BuildCompletedSubmitResult(artifacts []aigc.ContentGenerationArtifact, metadata map[string]any) aigc.GenerationSubmitResult {
	outBytes, _ := json.Marshal(artifacts)
	return aigc.GenerationSubmitResult{
		Status:    aigc.StatusSucceeded,
		Stage:     aigc.StageCompleted,
		Progress:  100,
		Output:    outBytes,
		Artifacts: artifacts,
		Metadata:  metadata,
	}
}

// BuildAsyncSubmitResult builds an async GenerationSubmitResult for queued/running provider tasks.
func BuildAsyncSubmitResult(taskID string, metadata map[string]any) aigc.GenerationSubmitResult {
	return aigc.GenerationSubmitResult{
		Status:         aigc.StatusRunning,
		Stage:          aigc.StageProviderRunning,
		Progress:       10,
		ProviderTaskID: taskID,
		Metadata:       metadata,
	}
}

// BuildCompletedPollResult builds a completed GenerationPollResult.
func BuildCompletedPollResult(artifacts []aigc.ContentGenerationArtifact) aigc.GenerationPollResult {
	outBytes, _ := json.Marshal(artifacts)
	return aigc.GenerationPollResult{
		Status:    aigc.StatusSucceeded,
		Stage:     aigc.StageCompleted,
		Progress:  100,
		Output:    outBytes,
		Artifacts: artifacts,
	}
}

// BuildRunningPollResult builds a running/queued GenerationPollResult.
func BuildRunningPollResult(progress int, stage aigc.GenerationStage) aigc.GenerationPollResult {
	if progress <= 0 {
		progress = 10
	}
	if stage == "" {
		stage = aigc.StageProviderRunning
	}
	return aigc.GenerationPollResult{
		Status:   aigc.StatusRunning,
		Stage:    stage,
		Progress: progress,
	}
}

// BuildFailedPollResult builds a failed GenerationPollResult.
func BuildFailedPollResult(code, message string) aigc.GenerationPollResult {
	if code == "" {
		code = aigc.ErrCodeInternalServerError
	}
	return aigc.GenerationPollResult{
		Status:       aigc.StatusFailed,
		Stage:        aigc.StageFailed,
		ErrorCode:    code,
		ErrorMessage: message,
	}
}

// BuildFailedPollResultFromRaw normalizes upstream error responses into a failed GenerationPollResult.
func BuildFailedPollResultFromRaw(provider string, statusCode int, rawBody []byte, fallbackErr error) aigc.GenerationPollResult {
	norm := aigc.NormalizeError(provider, statusCode, rawBody, fallbackErr)
	return aigc.GenerationPollResult{
		Status:       aigc.StatusFailed,
		Stage:        aigc.StageFailed,
		ErrorCode:    norm.Code,
		ErrorMessage: norm.Message,
	}
}

// BuildFailedSubmitResultFromRaw normalizes upstream error responses into a failed GenerationSubmitResult.
func BuildFailedSubmitResultFromRaw(provider string, statusCode int, rawBody []byte, fallbackErr error) aigc.GenerationSubmitResult {
	norm := aigc.NormalizeError(provider, statusCode, rawBody, fallbackErr)
	return aigc.GenerationSubmitResult{
		Status:           aigc.StatusFailed,
		Stage:            aigc.StageFailed,
		ErrorCode:        norm.Code,
		ErrorMessage:     norm.Message,
		ProviderResponse: json.RawMessage(rawBody),
	}
}

// BuildCanceledPollResult builds a canceled GenerationPollResult.
func BuildCanceledPollResult() aigc.GenerationPollResult {
	return aigc.GenerationPollResult{
		Status:       aigc.StatusCanceled,
		Stage:        aigc.StageCanceled,
		ErrorCode:    aigc.ErrCodeClientCanceled,
		ErrorMessage: "generation was canceled",
	}
}

// CreateArtifact creates a single ContentGenerationArtifact with CreatedAt timestamp.
func CreateArtifact(artifactType, storageProvider, uri, mimeType string, sizeBytes int64, metadata map[string]any) aigc.ContentGenerationArtifact {
	return aigc.ContentGenerationArtifact{
		ArtifactType:    artifactType,
		StorageProvider: storageProvider,
		URI:             uri,
		MIMEType:        mimeType,
		SizeBytes:       sizeBytes,
		CreatedAt:       time.Now(),
		Metadata:        metadata,
	}
}
