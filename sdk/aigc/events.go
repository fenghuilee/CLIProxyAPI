package aigc

import (
	"context"
	"encoding/json"
	"time"
)

// EventType represents the category of a lifecycle event.
type EventType string

const (
	EventGenerationCreated   EventType = "generation.created"
	EventInputPrepared       EventType = "input.prepared"
	EventProviderSelected    EventType = "provider.selected"
	EventProviderSubmitted   EventType = "provider.submitted"
	EventProviderPolled      EventType = "provider.polled"
	EventOutputPrepared      EventType = "output.prepared"
	EventGenerationSucceeded EventType = "generation.succeeded"
	EventGenerationFailed    EventType = "generation.failed"
	EventGenerationCanceled  EventType = "generation.canceled"
	EventGenerationRecovered EventType = "generation.recovered"
)

// ContentGenerationEvent is an immutable audit log entry for generation lifecycle events.
type ContentGenerationEvent struct {
	ID               uint64           `json:"id"`
	GenerationID     string           `json:"generation_id"`
	Revision         uint64           `json:"revision"`
	EventType        EventType        `json:"event_type"`
	Stage            GenerationStage  `json:"stage"`
	Status           GenerationStatus `json:"status"`
	ProviderRequest  json.RawMessage  `json:"provider_request,omitempty"`
	ProviderResponse json.RawMessage  `json:"provider_response,omitempty"`
	HTTPStatus       int              `json:"http_status,omitempty"`
	DurationMs       int64            `json:"duration_ms,omitempty"`
	CreatedAt        time.Time        `json:"created_at"`
}

// ContentGenerationObserver interface for read-only event subscribers.
type ContentGenerationObserver interface {
	OnContentGenerationEvent(ctx context.Context, event ContentGenerationEvent) error
}
