package aigc

import (
	"encoding/json"
	"time"
)

// ContentKind represents the media family being generated.
type ContentKind string

const (
	ContentKindVideo ContentKind = "video"
	ContentKindImage ContentKind = "image"
	ContentKindAudio ContentKind = "audio"
)

// ContentGeneration represents a durable asynchronous generation task snapshot.
type ContentGeneration struct {
	ID        string           `json:"id"`
	RequestID string           `json:"request_id,omitempty"`
	Kind      ContentKind      `json:"kind"`
	Model     string           `json:"model"`
	Status    GenerationStatus `json:"status"`
	Stage     GenerationStage  `json:"stage"`
	Progress  int              `json:"progress"`
	Revision  uint64           `json:"revision"`

	APIKey   string `json:"api_key,omitempty"`
	ClientIP string `json:"client_ip,omitempty"`

	Input            json.RawMessage `json:"input,omitempty"`
	PreparedInput    json.RawMessage `json:"prepared_input,omitempty"`
	ProviderRequest  json.RawMessage `json:"provider_request,omitempty"`
	ProviderResponse json.RawMessage `json:"provider_response,omitempty"`
	Output           json.RawMessage `json:"output,omitempty"`

	Provider       string `json:"provider,omitempty"`
	ProviderTaskID string `json:"provider_task_id,omitempty"`
	AuthID         string `json:"auth_id,omitempty"`

	ErrorCode    string `json:"error_code,omitempty"`
	ErrorMessage string `json:"error_message,omitempty"`

	Metadata map[string]any `json:"metadata,omitempty"`

	WorkerID   string     `json:"worker_id,omitempty"`
	LeaseUntil *time.Time `json:"lease_until,omitempty"`

	Artifacts []ContentGenerationArtifact `json:"artifacts,omitempty"`

	BillingUsage json.RawMessage `json:"billing_usage,omitempty"`

	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

// ContentGenerationDraft holds the initial parameters for a new generation before persistence.
type ContentGenerationDraft struct {
	ID           string          `json:"id"`
	RequestID    string          `json:"request_id,omitempty"`
	Kind         ContentKind     `json:"kind"`
	Model        string          `json:"model"`
	APIKey       string          `json:"api_key,omitempty"`
	ClientIP     string          `json:"client_ip,omitempty"`
	Input        json.RawMessage `json:"input,omitempty"`
	Metadata     map[string]any  `json:"metadata,omitempty"`
	BillingUsage json.RawMessage `json:"billing_usage,omitempty"`
}

// ContentGenerationArtifact represents an input or output asset associated with a generation.
type ContentGenerationArtifact struct {
	ID              uint64         `json:"id"`
	GenerationID    string         `json:"generation_id"`
	ArtifactType    string         `json:"artifact_type"`
	StorageProvider string         `json:"storage_provider"`
	URI             string         `json:"uri"`
	MIMEType        string         `json:"mime_type,omitempty"`
	SizeBytes       int64          `json:"size_bytes,omitempty"`
	SHA256          string         `json:"sha256,omitempty"`
	Metadata        map[string]any `json:"metadata,omitempty"`
	CreatedAt       time.Time      `json:"created_at"`
	ExpiresAt       *time.Time     `json:"expires_at,omitempty"`
}
