package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/aigc"
)

type submissionClaimBridge struct {
	*mockMemoryDBBridge
	claimQuery string
}

func (b *submissionClaimBridge) Query(ctx context.Context, query string, args ...any) ([]map[string]any, error) {
	if strings.Contains(query, "WHERE status IN") {
		b.claimQuery = query
	}
	return b.mockMemoryDBBridge.Query(ctx, query, args...)
}

func TestClaimExcludesSubmissionEvenWithExpiredLease(t *testing.T) {
	bridge := &submissionClaimBridge{mockMemoryDBBridge: newMockMemoryDBBridge()}
	bridge.records["cg_submitting"] = map[string]any{
		"generation_id": "cg_submitting", "revision": uint64(1),
		"status": "accepted", "stage": "submitting",
		"worker_id": "old-worker", "lease_until": time.Now().Add(-time.Hour),
	}
	claimed, err := NewStore(bridge).Claim(context.Background(), aigc.GenerationClaimRequest{
		WorkerID: "recovery", LeaseDuration: time.Minute,
	})
	if err != nil || len(claimed) != 0 {
		t.Fatalf("in-flight submission claimed: %+v, err=%v", claimed, err)
	}
	if !strings.Contains(bridge.claimQuery, "AND NOT (status = 'accepted' AND stage = 'submitting')") {
		t.Fatal("claim query must exclude in-flight submissions before applying LIMIT")
	}
	if revision := parseUint64(bridge.records["cg_submitting"]["revision"]); revision != 1 {
		t.Fatalf("claim changed submitting revision to %d", revision)
	}
}

func TestReleaseDoesNotInvalidateSubmissionOrAnotherOwner(t *testing.T) {
	for _, tc := range []struct {
		name, status, stage, owner string
	}{
		{"submitting", "accepted", "submitting", "recovery"},
		{"another-owner", "running", "provider_running", "another-worker"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bridge := newMockMemoryDBBridge()
			bridge.records["cg_submission"] = map[string]any{
				"generation_id": "cg_submission", "revision": uint64(2),
				"status": tc.status, "stage": tc.stage, "worker_id": tc.owner,
			}
			err := NewStore(bridge).Release(context.Background(), aigc.GenerationReleaseRequest{
				ID: "cg_submission", WorkerID: "recovery",
			})
			if err != nil {
				t.Fatal(err)
			}
			if revision := parseUint64(bridge.records["cg_submission"]["revision"]); revision != 2 {
				t.Fatalf("release invalidated submission revision: %d", revision)
			}
		})
	}
}
