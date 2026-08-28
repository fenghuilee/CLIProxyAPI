package main

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/aigc"
)

type mockMemoryDBBridge struct {
	records   map[string]map[string]any
	artifacts map[string][]map[string]any
	nextArtID uint64
}

func newMockMemoryDBBridge() *mockMemoryDBBridge {
	return &mockMemoryDBBridge{
		records:   make(map[string]map[string]any),
		artifacts: make(map[string][]map[string]any),
		nextArtID: 1,
	}
}

func (m *mockMemoryDBBridge) Query(ctx context.Context, query string, args ...any) ([]map[string]any, error) {
	// Query artifacts by generation_id & uri (idempotency check)
	if stringsContains(query, "SELECT id FROM content_generation_artifacts") && len(args) >= 2 {
		genID := fmt.Sprint(args[0])
		uri := fmt.Sprint(args[1])
		for _, art := range m.artifacts[genID] {
			if parseString(art["uri"]) == uri {
				return []map[string]any{{"id": art["id"]}}, nil
			}
		}
		return []map[string]any{}, nil
	}

	// Query artifacts by generation_id
	if stringsContains(query, "SELECT * FROM content_generation_artifacts") && len(args) > 0 {
		genID := fmt.Sprint(args[0])
		var list []map[string]any
		for _, art := range m.artifacts[genID] {
			copied := make(map[string]any, len(art))
			for k, v := range art {
				copied[k] = v
			}
			list = append(list, copied)
		}
		return list, nil
	}

	// Query generation by ID
	if stringsContains(query, "SELECT * FROM content_generations WHERE generation_id") && len(args) > 0 {
		if id, ok := args[0].(string); ok {
			if rec, found := m.records[id]; found {
				copied := make(map[string]any, len(rec))
				for k, v := range rec {
					copied[k] = v
				}
				return []map[string]any{copied}, nil
			}
			return []map[string]any{}, nil
		}
	}

	// Return all records for list/claim queries
	var list []map[string]any
	for _, rec := range m.records {
		copied := make(map[string]any, len(rec))
		for k, v := range rec {
			copied[k] = v
		}
		list = append(list, copied)
	}
	return list, nil
}

func (m *mockMemoryDBBridge) Exec(ctx context.Context, query string, args ...any) (int64, int64, error) {
	if len(args) == 0 {
		return 0, 0, nil
	}

	// 1. Insert generation
	if len(args) >= 10 && stringsContains(query, "INSERT INTO content_generations") {
		id := fmt.Sprint(args[0])
		rec := map[string]any{
			"generation_id": id,
			"request_id":    args[1],
			"kind":          args[2],
			"model":         args[3],
			"status":        args[4],
			"stage":         args[5],
			"progress":      0,
			"revision":      uint64(1),
			"api_key":       args[6],
			"client_ip":     args[7],
			"input":         args[8],
			"metadata":      args[9],
			"billing_usage": args[10],
			"created_at":    args[11],
			"updated_at":    args[12],
		}
		m.records[id] = rec
		return 1, 1, nil
	}

	// 2. Insert artifact
	if stringsContains(query, "INSERT INTO content_generation_artifacts") && len(args) >= 10 {
		genID := fmt.Sprint(args[0])
		artID := m.nextArtID
		m.nextArtID++

		art := map[string]any{
			"id":               artID,
			"generation_id":    genID,
			"artifact_type":    args[1],
			"storage_provider": args[2],
			"uri":              args[3],
			"mime_type":        args[4],
			"size_bytes":       args[5],
			"sha256":           args[6],
			"metadata":         args[7],
			"created_at":       args[8],
			"expires_at":       args[9],
		}
		m.artifacts[genID] = append(m.artifacts[genID], art)
		return 1, int64(artID), nil
	}

	// 3. Update with CAS
	if stringsContains(query, "UPDATE content_generations SET") {
		id := fmt.Sprint(args[len(args)-2])
		expectedRevision := parseUint64(args[len(args)-1])

		rec, found := m.records[id]
		if !found {
			return 0, 0, nil
		}
		currentRevision := parseUint64(rec["revision"])
		if currentRevision != expectedRevision {
			return 0, 0, nil
		}

		rec["revision"] = currentRevision + 1
		rec["updated_at"] = time.Now()

		// Apply fields
		if stringsContains(query, "status = ?") {
			for i, clause := range splitSetClauses(query) {
				if stringsContains(clause, "status = ?") && i+1 < len(args) {
					rec["status"] = args[i+1]
				}
				if stringsContains(clause, "stage = ?") && i+1 < len(args) {
					rec["stage"] = args[i+1]
				}
				if stringsContains(clause, "progress = ?") && i+1 < len(args) {
					rec["progress"] = args[i+1]
				}
			}
		}

		m.records[id] = rec
		return 1, 0, nil
	}

	// 4. Delete
	if stringsContains(query, "DELETE FROM content_generations") {
		count := int64(len(m.records))
		m.records = make(map[string]map[string]any)
		m.artifacts = make(map[string][]map[string]any)
		return count, 0, nil
	}
	if stringsContains(query, "DELETE FROM content_generation_artifacts") {
		count := int64(len(m.artifacts))
		m.artifacts = make(map[string][]map[string]any)
		return count, 0, nil
	}

	return 0, 0, nil
}

func stringsContains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 || (len(s) > 0 && len(substr) > 0 && findSubstr(s, substr)))
}

func findSubstr(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func splitSetClauses(q string) []string {
	return []string{"status = ?", "stage = ?", "progress = ?"}
}

func TestAIGCBusinessStore_LifecycleAndArtifacts(t *testing.T) {
	bridge := newMockMemoryDBBridge()
	store := NewStore(bridge)
	ctx := context.Background()

	// 1. Create with initial input artifact
	gen, errCreate := store.Create(ctx, aigc.GenerationCreateRequest{
		Draft: aigc.ContentGenerationDraft{
			ID:       "cg_test_123",
			Kind:     aigc.ContentKindVideo,
			Model:    "volcengine/doubao-seedance-1-0-pro",
			APIKey:   "sk-test-key",
			ClientIP: "1.2.3.4",
			Input:    []byte(`{"prompt":"hello world"}`),
		},
		Artifacts: []aigc.ContentGenerationArtifact{
			{
				ArtifactType:    "input_image",
				StorageProvider: "tos",
				URI:             "https://mybucket.tos.volces.com/input.png",
				MIMEType:        "image/png",
			},
		},
	})
	if errCreate != nil {
		t.Fatalf("Create generation failed: %v", errCreate)
	}
	if gen.ID != "cg_test_123" || gen.Status != aigc.StatusAccepted || gen.Revision != 1 {
		t.Fatalf("Created generation unexpected: %+v", gen)
	}
	if len(gen.Artifacts) != 1 || gen.Artifacts[0].ArtifactType != "input_image" {
		t.Fatalf("Created generation artifacts unexpected: %+v", gen.Artifacts)
	}

	// 2. Get verifies generation and input artifact
	fetched, errGet := store.Get(ctx, aigc.GenerationGetRequest{ID: "cg_test_123"})
	if errGet != nil {
		t.Fatalf("Get generation failed: %v", errGet)
	}
	if fetched.ID != "cg_test_123" || fetched.APIKey != "sk-test-key" {
		t.Fatalf("Fetched generation mismatch: %+v", fetched)
	}
	if len(fetched.Artifacts) != 1 || fetched.Artifacts[0].URI != "https://mybucket.tos.volces.com/input.png" {
		t.Fatalf("Fetched generation artifacts mismatch: %+v", fetched.Artifacts)
	}

	// 3. Patch CAS success and append output artifacts (output_video + last_frame)
	patched, errPatch := store.Patch(ctx, aigc.GenerationPatchRequest{
		ID:               "cg_test_123",
		ExpectedRevision: 1,
		Set: map[string]any{
			"status":   aigc.StatusSucceeded,
			"stage":    aigc.StageCompleted,
			"progress": 100,
		},
		Artifacts: []aigc.ContentGenerationArtifact{
			{
				ArtifactType:    "output_video",
				StorageProvider: "volcengine",
				URI:             "https://volcengine.example.com/output.mp4",
				MIMEType:        "video/mp4",
			},
			{
				ArtifactType:    "last_frame",
				StorageProvider: "volcengine",
				URI:             "https://volcengine.example.com/last_frame.png",
				MIMEType:        "image/png",
			},
		},
	})
	if errPatch != nil {
		t.Fatalf("Patch generation failed: %v", errPatch)
	}
	if patched.Revision != 2 {
		t.Fatalf("Patched generation revision = %d, want 2", patched.Revision)
	}
	if len(patched.Artifacts) != 3 {
		t.Fatalf("Patched generation expected 3 artifacts (1 input + 2 output), got %d: %+v", len(patched.Artifacts), patched.Artifacts)
	}

	// 4. Test Idempotency: Patching with same artifact URI again should not duplicate
	_, errPatchDup := store.Patch(ctx, aigc.GenerationPatchRequest{
		ID:               "cg_test_123",
		ExpectedRevision: 2,
		Set: map[string]any{
			"progress": 100,
		},
		Artifacts: []aigc.ContentGenerationArtifact{
			{
				ArtifactType:    "output_video",
				StorageProvider: "volcengine",
				URI:             "https://volcengine.example.com/output.mp4",
			},
		},
	})
	if errPatchDup != nil {
		t.Fatalf("Patch duplicate artifact failed: %v", errPatchDup)
	}
	afterDup, _ := store.Get(ctx, aigc.GenerationGetRequest{ID: "cg_test_123"})
	if len(afterDup.Artifacts) != 3 {
		t.Fatalf("Expected 3 artifacts after idempotent patch, got %d", len(afterDup.Artifacts))
	}

	// 5. Patch CAS conflict with stale revision
	_, errConflict := store.Patch(ctx, aigc.GenerationPatchRequest{
		ID:               "cg_test_123",
		ExpectedRevision: 1, // Stale! Current is 3
		Set: map[string]any{
			"progress": 50,
		},
	})
	if errConflict == nil {
		t.Fatalf("Expected revision conflict error, got nil")
	}

	// 6. Delete Expired
	deleted, errDel := store.DeleteExpired(ctx, aigc.GenerationDeleteExpiredRequest{})
	if errDel != nil || deleted != 1 {
		t.Fatalf("DeleteExpired: deleted = %d, err = %v", deleted, errDel)
	}

	// 7. Test EnsureSchema
	if errSchema := store.EnsureSchema(ctx); errSchema != nil {
		t.Fatalf("EnsureSchema failed: %v", errSchema)
	}

	// 8. Test Claim
	claimed, errClaim := store.Claim(ctx, aigc.GenerationClaimRequest{
		WorkerID:      "worker-test",
		LeaseDuration: 1 * time.Minute,
		Limit:         5,
	})
	if errClaim != nil {
		t.Fatalf("Claim failed: %v", errClaim)
	}
	_ = claimed
}

func TestToSnakeCase(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"generationId", "generation_id"},
		{"ProviderTaskId", "provider_task_id"},
		{"clientIP", "client_i_p"},
		{"status", "status"},
	}
	for _, tt := range tests {
		got := toSnakeCase(tt.input)
		if got != tt.want {
			t.Errorf("toSnakeCase(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
