package aigc

import (
	"context"
	"fmt"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/aigc"
)

// maxEventPayloadBytes bounds a single audit payload. Synchronous image responses
// can embed base64 images, so the event log must not store them verbatim.
const maxEventPayloadBytes = 64 << 10

// syncGenerationRecorder collects the lifecycle events and the terminal state of a
// synchronous pipeline run so they are written by a single CAS patch. Applying the
// status change and its events together means an event is never persisted for a
// state that was rejected by the revision check.
type syncGenerationRecorder struct {
	store   aigc.ContentGenerationStore
	created aigc.ContentGeneration
	emit    func(context.Context, aigc.ContentGenerationEvent)
	events  []aigc.ContentGenerationEvent
}

func newSyncGenerationRecorder(store aigc.ContentGenerationStore, created aigc.ContentGeneration, emit func(context.Context, aigc.ContentGenerationEvent)) *syncGenerationRecorder {
	return &syncGenerationRecorder{store: store, created: created, emit: emit}
}

// enabled reports whether lifecycle events can be persisted for this run.
func (r *syncGenerationRecorder) enabled() bool {
	return r != nil && r.store != nil && r.created.ID != ""
}

// record buffers an event for the next patch, stamping the revision it describes.
func (r *syncGenerationRecorder) record(event aigc.ContentGenerationEvent) {
	if r == nil {
		return
	}
	if event.Revision == 0 {
		event.Revision = r.created.Revision
	}
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now()
	}
	r.events = append(r.events, event)
}

// flush writes the terminal state together with every buffered event and then
// publishes the persisted events to observers.
func (r *syncGenerationRecorder) flush(ctx context.Context, set map[string]any, artifacts []aigc.ContentGenerationArtifact) error {
	if !r.enabled() {
		return aigc.ErrStoreNotConfigured
	}
	if _, errPatch := r.store.Patch(ctx, aigc.GenerationPatchRequest{
		ID:               r.created.ID,
		ExpectedRevision: r.created.Revision,
		Set:              set,
		Artifacts:        artifacts,
		Events:           r.events,
	}); errPatch != nil {
		return errPatch
	}
	if r.emit == nil {
		return nil
	}
	// Observers only see events that reached the store, so a failed write cannot
	// make live telemetry disagree with the audit log.
	for _, event := range r.events {
		r.emit(ctx, event)
	}
	return nil
}

// syncEventPayload renders an upstream payload for the event log, replacing bodies
// that exceed the storage budget with an explicit truncation marker.
func syncEventPayload(raw []byte) []byte {
	if len(raw) == 0 {
		return nil
	}
	if len(raw) <= maxEventPayloadBytes {
		return raw
	}
	return []byte(fmt.Sprintf(
		`{"truncated":true,"original_bytes":%d,"limit_bytes":%d}`,
		len(raw), maxEventPayloadBytes,
	))
}
