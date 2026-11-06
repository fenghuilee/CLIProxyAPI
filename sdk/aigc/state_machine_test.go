package aigc

import (
	"encoding/json"
	"testing"
	"time"
)

func TestStateTransitions(t *testing.T) {
	tests := []struct {
		name      string
		oldStatus GenerationStatus
		newStatus GenerationStatus
		wantErr   bool
	}{
		{"same status", StatusAccepted, StatusAccepted, false},
		{"accepted to running", StatusAccepted, StatusRunning, false},
		{"accepted to succeeded", StatusAccepted, StatusSucceeded, false},
		{"accepted to failed", StatusAccepted, StatusFailed, false},
		{"accepted to canceled", StatusAccepted, StatusCanceled, false},
		{"running to succeeded", StatusRunning, StatusSucceeded, false},
		{"running to failed", StatusRunning, StatusFailed, false},
		{"running to canceled", StatusRunning, StatusCanceled, false},
		{"running to accepted", StatusRunning, StatusAccepted, true},
		{"succeeded to running", StatusSucceeded, StatusRunning, true},
		{"failed to running", StatusFailed, StatusRunning, true},
		{"canceled to running", StatusCanceled, StatusRunning, true},
		{"succeeded to failed", StatusSucceeded, StatusFailed, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateStatusTransition(tt.oldStatus, tt.newStatus)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateStatusTransition(%v, %v) error = %v, wantErr %v", tt.oldStatus, tt.newStatus, err, tt.wantErr)
			}
		})
	}
}

func TestStageTransitions(t *testing.T) {
	tests := []struct {
		name    string
		status  GenerationStatus
		stage   GenerationStage
		wantErr bool
	}{
		{"accepted created", StatusAccepted, StageCreated, false},
		{"accepted preparing input", StatusAccepted, StagePreparingInput, false},
		{"accepted input ready", StatusAccepted, StageInputReady, false},
		{"accepted submitting", StatusAccepted, StageSubmitting, false},
		{"accepted completed", StatusAccepted, StageCompleted, true},
		{"running provider running", StatusRunning, StageProviderRunning, false},
		{"running polling", StatusRunning, StagePolling, false},
		{"running preparing output", StatusRunning, StagePreparingOutput, false},
		{"running created", StatusRunning, StageCreated, true},
		{"succeeded completed", StatusSucceeded, StageCompleted, false},
		{"succeeded polling", StatusSucceeded, StagePolling, true},
		{"failed failed", StatusFailed, StageFailed, false},
		{"failed completed", StatusFailed, StageCompleted, true},
		{"canceled canceled", StatusCanceled, StageCanceled, false},
		{"canceled running", StatusCanceled, StageProviderRunning, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateStageTransition(tt.status, tt.stage)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateStageTransition(%v, %v) error = %v, wantErr %v", tt.status, tt.stage, err, tt.wantErr)
			}
		})
	}
}

func TestJSONSerialization(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	gen := ContentGeneration{
		ID:        "cg_12345",
		Kind:      ContentKindVideo,
		Model:     "doubao-seedance-2-0-mini",
		Status:    StatusAccepted,
		Stage:     StageCreated,
		Progress:  0,
		Revision:  1,
		APIKey:    "sk-test",
		ClientIP:  "127.0.0.1",
		Input:     json.RawMessage(`{"prompt":"a cute cat"}`),
		Metadata:  map[string]any{"source": "test"},
		CreatedAt: now,
		UpdatedAt: now,
	}

	data, err := json.Marshal(gen)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var decoded ContentGeneration
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if decoded.ID != gen.ID || decoded.Kind != gen.Kind || decoded.Status != gen.Status || decoded.Revision != gen.Revision {
		t.Errorf("Decoded generation does not match original: got %+v, want %+v", decoded, gen)
	}
}
