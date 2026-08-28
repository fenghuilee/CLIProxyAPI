package utils

import (
	"encoding/json"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/aigc"
)

// BuildCompletedSubmitResult creates a completed GenerationSubmitResult with output artifacts.
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

// BuildAsyncSubmitResult creates a running GenerationSubmitResult for asynchronous tasks.
func BuildAsyncSubmitResult(taskID string, metadata map[string]any) aigc.GenerationSubmitResult {
	return aigc.GenerationSubmitResult{
		Status:         aigc.StatusRunning,
		Stage:          aigc.StageProviderRunning,
		Progress:       10,
		ProviderTaskID: taskID,
		Metadata:       metadata,
	}
}

// BuildCompletedPollResult creates a completed GenerationPollResult with output artifacts.
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

// BuildRunningPollResult creates a running GenerationPollResult.
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

// BuildFailedPollResult creates a failed GenerationPollResult with normalized code.
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

// BuildFailedPollResultFromRaw normalizes provider raw payload and creates a failed GenerationPollResult.
func BuildFailedPollResultFromRaw(provider string, statusCode int, rawBody []byte, fallbackErr error) aigc.GenerationPollResult {
	norm := aigc.NormalizeError(provider, statusCode, rawBody, fallbackErr)
	return aigc.GenerationPollResult{
		Status:       aigc.StatusFailed,
		Stage:        aigc.StageFailed,
		ErrorCode:    norm.Code,
		ErrorMessage: norm.Message,
	}
}

// BuildFailedSubmitResultFromRaw normalizes provider raw payload and creates a failed GenerationSubmitResult.
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

// BuildCanceledPollResult creates a canceled GenerationPollResult.
func BuildCanceledPollResult() aigc.GenerationPollResult {
	return aigc.GenerationPollResult{
		Status:       aigc.StatusCanceled,
		Stage:        aigc.StageCanceled,
		ErrorCode:    aigc.ErrCodeClientCanceled,
		ErrorMessage: "generation was canceled",
	}
}

// CreateArtifact creates a single ContentGenerationArtifact with current timestamp.
func CreateArtifact(artifactType, storageProvider, uri, mimeType string, sizeBytes int64, metadata map[string]any) aigc.ContentGenerationArtifact {
	return aigc.ContentGenerationArtifact{
		ArtifactType:    artifactType,
		StorageProvider: storageProvider,
		URI:             uri,
		MIMEType:        mimeType,
		SizeBytes:       sizeBytes,
		Metadata:        metadata,
		CreatedAt:       time.Now(),
	}
}
