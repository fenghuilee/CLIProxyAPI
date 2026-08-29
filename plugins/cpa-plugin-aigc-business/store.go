package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/aigc"
	"github.com/tidwall/gjson"
)

// DBBridge defines database interaction methods provided by the host.
type DBBridge interface {
	Query(ctx context.Context, query string, args ...any) ([]map[string]any, error)
	Exec(ctx context.Context, query string, args ...any) (rowsAffected int64, lastInsertID int64, err error)
}

// Store implements aigc.ContentGenerationStore using a generic DBBridge.
type Store struct {
	bridge DBBridge
}

// NewStore creates a new Store instance.
func NewStore(bridge DBBridge) *Store {
	return &Store{bridge: bridge}
}

// EnsureSchema creates the core tables if they do not exist.
func (s *Store) EnsureSchema(ctx context.Context) error {
	if s == nil || s.bridge == nil {
		return fmt.Errorf("store bridge not initialized")
	}
	return EnsureSchema(ctx, s.bridge)
}

// Create inserts a new content generation record.
func (s *Store) Create(ctx context.Context, req aigc.GenerationCreateRequest) (aigc.ContentGeneration, error) {
	if s == nil || s.bridge == nil {
		return aigc.ContentGeneration{}, fmt.Errorf("store bridge not initialized")
	}

	draft := req.Draft
	if draft.ID == "" {
		return aigc.ContentGeneration{}, fmt.Errorf("generation_id is required")
	}

	status := aigc.StatusAccepted
	stage := aigc.StageCreated

	var metadataStr *string
	if draft.Metadata != nil {
		b, err := json.Marshal(draft.Metadata)
		if err == nil {
			str := string(b)
			metadataStr = &str
		}
	}

	prompt := draft.Prompt
	if prompt == "" && len(draft.Input) > 0 {
		prompt = gjson.GetBytes(draft.Input, "prompt").String()
		if prompt == "" {
			prompt = gjson.GetBytes(draft.Input, "text").String()
		}
	}
	var promptVal *string
	if prompt != "" {
		promptVal = &prompt
	}

	bType := draft.BillingType
	if bType == "" {
		bType = "fixed"
	}
	bStatus := draft.BillingStatus
	if bStatus == "" {
		bStatus = "unbilled"
	}
	costVal := draft.Cost
	if costVal == "" {
		costVal = "0.00000000"
	}

	now := time.Now()
	query := `
INSERT INTO content_generations (
    generation_id, request_id, user_id, kind, model, prompt, status, stage, progress, revision,
    api_key, client_ip, input, provider_request,
    output, provider, provider_task_id, error_code, error_message, duration_ms,
    metadata, billing_type, billing_status, cost, worker_id, lease_until, created_at, updated_at, completed_at, expires_at
) VALUES (
    ?, ?, ?, ?, ?, ?, ?, ?, 0, 1,
    ?, ?, ?, NULL,
    NULL, NULL, NULL, NULL, NULL, NULL,
    ?, ?, ?, ?, NULL, NULL, ?, ?, NULL, NULL
)`

	var reqIDVal *string
	if draft.RequestID != "" {
		reqIDVal = &draft.RequestID
	}

	_, _, errExec := s.bridge.Exec(ctx, query,
		draft.ID, reqIDVal, draft.UserID, string(draft.Kind), draft.Model, promptVal, string(status), string(stage),
		draft.APIKey, draft.ClientIP, string(draft.Input),
		metadataStr, bType, bStatus, costVal, now, now,
	)
	if errExec != nil {
		return aigc.ContentGeneration{}, fmt.Errorf("insert generation: %w", errExec)
	}

	if len(req.Artifacts) > 0 {
		if errArt := s.insertArtifacts(ctx, draft.ID, req.Artifacts); errArt != nil {
			fmt.Printf("aigc store: failed to insert initial artifacts for %s: %v\n", draft.ID, errArt)
		}
	}

	return s.Get(ctx, aigc.GenerationGetRequest{ID: draft.ID})
}

// Get retrieves a generation by its ID along with its persisted artifacts.
func (s *Store) Get(ctx context.Context, req aigc.GenerationGetRequest) (aigc.ContentGeneration, error) {
	if s == nil || s.bridge == nil {
		return aigc.ContentGeneration{}, fmt.Errorf("store bridge not initialized")
	}

	query := "SELECT * FROM content_generations WHERE generation_id = ?"
	rows, err := s.bridge.Query(ctx, query, req.ID)
	if err != nil {
		return aigc.ContentGeneration{}, fmt.Errorf("query generation: %w", err)
	}
	if len(rows) == 0 {
		return aigc.ContentGeneration{}, aigc.ErrNotFound
	}

	gen := rowToGeneration(rows[0])
	artifacts, errArt := s.getArtifacts(ctx, req.ID)
	if errArt == nil && len(artifacts) > 0 {
		gen.Artifacts = artifacts
	}
	return gen, nil
}

// Patch updates a generation record using optimistic locking (revision CAS).
func (s *Store) Patch(ctx context.Context, req aigc.GenerationPatchRequest) (aigc.ContentGeneration, error) {
	if s == nil || s.bridge == nil {
		return aigc.ContentGeneration{}, fmt.Errorf("store bridge not initialized")
	}
	if len(req.Set) == 0 {
		return s.Get(ctx, aigc.GenerationGetRequest{ID: req.ID})
	}

	setClauses := make([]string, 0, len(req.Set)+2)
	args := make([]any, 0, len(req.Set)+4)

	setClauses = append(setClauses, "revision = revision + 1", "updated_at = ?")
	args = append(args, time.Now())

	for k, v := range req.Set {
		col := toSnakeCase(k)
		if col == "revision" || col == "generation_id" || col == "created_at" || col == "updated_at" {
			continue
		}

		if rawMsg, ok := v.(json.RawMessage); ok {
			setClauses = append(setClauses, col+" = ?")
			args = append(args, string(rawMsg))
		} else if byteSlice, ok := v.([]byte); ok {
			setClauses = append(setClauses, col+" = ?")
			args = append(args, string(byteSlice))
		} else if objMap, ok := v.(map[string]any); ok {
			b, _ := json.Marshal(objMap)
			setClauses = append(setClauses, col+" = ?")
			args = append(args, string(b))
		} else if arrSlice, ok := v.([]any); ok {
			b, _ := json.Marshal(arrSlice)
			setClauses = append(setClauses, col+" = ?")
			args = append(args, string(b))
		} else {
			setClauses = append(setClauses, col+" = ?")
			args = append(args, v)
		}
	}

	query := fmt.Sprintf("UPDATE content_generations SET %s WHERE generation_id = ? AND revision = ?", strings.Join(setClauses, ", "))
	args = append(args, req.ID, req.ExpectedRevision)

	rowsAffected, _, errExec := s.bridge.Exec(ctx, query, args...)
	if errExec != nil {
		return aigc.ContentGeneration{}, fmt.Errorf("exec patch: %w", errExec)
	}

	if rowsAffected == 0 {
		// Revision conflict or record not found
		current, errGet := s.Get(ctx, aigc.GenerationGetRequest{ID: req.ID})
		if errGet != nil {
			return aigc.ContentGeneration{}, aigc.ErrNotFound
		}
		if current.Revision != req.ExpectedRevision {
			return current, fmt.Errorf("%w: expected revision %d, current is %d", aigc.ErrRevisionConflict, req.ExpectedRevision, current.Revision)
		}
		return current, nil
	}

	if len(req.Artifacts) > 0 {
		if errArt := s.insertArtifacts(ctx, req.ID, req.Artifacts); errArt != nil {
			fmt.Printf("aigc store: failed to insert patched artifacts for %s: %v\n", req.ID, errArt)
		}
	}

	return s.Get(ctx, aigc.GenerationGetRequest{ID: req.ID})
}

// Claim acquires worker leases for unassigned or expired running tasks.
func (s *Store) Claim(ctx context.Context, req aigc.GenerationClaimRequest) ([]aigc.ContentGeneration, error) {
	if s == nil || s.bridge == nil {
		return nil, fmt.Errorf("store bridge not initialized")
	}

	limit := req.Limit
	if limit <= 0 {
		limit = 10
	}

	now := time.Now()
	leaseExpiry := now.Add(req.LeaseDuration)

	query := fmt.Sprintf(`
SELECT * FROM content_generations
WHERE status IN ('accepted', 'running')
  AND (worker_id IS NULL OR worker_id = '' OR lease_until IS NULL OR lease_until < ?)
ORDER BY created_at ASC
LIMIT %d`, limit)

	rows, err := s.bridge.Query(ctx, query, now)
	if err != nil {
		return nil, fmt.Errorf("query claimable tasks: %w", err)
	}

	var claimed []aigc.ContentGeneration
	for _, row := range rows {
		gen := rowToGeneration(row)
		patched, errPatch := s.Patch(ctx, aigc.GenerationPatchRequest{
			ID:               gen.ID,
			ExpectedRevision: gen.Revision,
			Set: map[string]any{
				"worker_id":   req.WorkerID,
				"lease_until": leaseExpiry,
			},
		})
		if errPatch == nil {
			claimed = append(claimed, patched)
		}
	}

	return claimed, nil
}

// Release clears the worker lease on a generation.
func (s *Store) Release(ctx context.Context, req aigc.GenerationReleaseRequest) error {
	if s == nil || s.bridge == nil {
		return fmt.Errorf("store bridge not initialized")
	}

	gen, errGet := s.Get(ctx, aigc.GenerationGetRequest{ID: req.ID})
	if errGet != nil {
		return errGet
	}

	_, errPatch := s.Patch(ctx, aigc.GenerationPatchRequest{
		ID:               gen.ID,
		ExpectedRevision: gen.Revision,
		Set: map[string]any{
			"worker_id":   nil,
			"lease_until": nil,
		},
	})
	return errPatch
}

// DeleteExpired removes generation records that have exceeded their expires_at timestamp.
func (s *Store) DeleteExpired(ctx context.Context, req aigc.GenerationDeleteExpiredRequest) (int64, error) {
	if s == nil || s.bridge == nil {
		return 0, fmt.Errorf("store bridge not initialized")
	}

	now := time.Now()
	// Clean up expired artifacts first
	queryArt := "DELETE FROM content_generation_artifacts WHERE expires_at IS NOT NULL AND expires_at < ?"
	_, _, _ = s.bridge.Exec(ctx, queryArt, now)

	// Clean up expired generations
	query := "DELETE FROM content_generations WHERE expires_at IS NOT NULL AND expires_at < ?"
	rowsAffected, _, err := s.bridge.Exec(ctx, query, now)
	if err != nil {
		return 0, fmt.Errorf("delete expired: %w", err)
	}
	return rowsAffected, nil
}

func (s *Store) insertArtifacts(ctx context.Context, genID string, artifacts []aigc.ContentGenerationArtifact) error {
	if len(artifacts) == 0 {
		return nil
	}

	for _, art := range artifacts {
		uri := strings.TrimSpace(art.URI)
		if uri == "" {
			continue
		}
		if len(uri) > 1024 {
			uri = uri[:1024]
		}
		artType := strings.TrimSpace(art.ArtifactType)
		if artType == "" {
			artType = "output_video"
		}
		storageProvider := strings.TrimSpace(art.StorageProvider)
		if storageProvider == "" {
			storageProvider = "volcengine"
		}

		// Check if artifact with same generation_id and uri already exists (idempotency)
		checkQuery := "SELECT id FROM content_generation_artifacts WHERE generation_id = ? AND uri = ? LIMIT 1"
		existing, errCheck := s.bridge.Query(ctx, checkQuery, genID, uri)
		if errCheck == nil && len(existing) > 0 {
			continue
		}

		var metadataStr *string
		if art.Metadata != nil {
			b, err := json.Marshal(art.Metadata)
			if err == nil {
				str := string(b)
				metadataStr = &str
			}
		}

		createdAt := art.CreatedAt
		if createdAt.IsZero() {
			createdAt = time.Now()
		}

		insertQuery := `
INSERT INTO content_generation_artifacts (
    generation_id, artifact_type, storage_provider, uri,
    mime_type, size_bytes, sha256, metadata, created_at, expires_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

		_, _, errExec := s.bridge.Exec(ctx, insertQuery,
			genID, artType, storageProvider, uri,
			art.MIMEType, art.SizeBytes, art.SHA256, metadataStr, createdAt, art.ExpiresAt,
		)
		if errExec != nil {
			return fmt.Errorf("insert artifact (%s): %w", uri, errExec)
		}
	}
	return nil
}

func (s *Store) getArtifacts(ctx context.Context, genID string) ([]aigc.ContentGenerationArtifact, error) {
	query := "SELECT * FROM content_generation_artifacts WHERE generation_id = ? ORDER BY id ASC"
	rows, err := s.bridge.Query(ctx, query, genID)
	if err != nil {
		return nil, fmt.Errorf("query artifacts: %w", err)
	}

	artifacts := make([]aigc.ContentGenerationArtifact, 0, len(rows))
	for _, row := range rows {
		artifacts = append(artifacts, rowToArtifact(row))
	}
	return artifacts, nil
}

func toSnakeCase(s string) string {
	var sb strings.Builder
	for i, r := range s {
		if r >= 'A' && r <= 'Z' {
			if i > 0 {
				sb.WriteByte('_')
			}
			sb.WriteRune(r + 32)
		} else {
			sb.WriteRune(r)
		}
	}
	return sb.String()
}
