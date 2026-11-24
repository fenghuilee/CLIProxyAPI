package main

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

// Scoped DB fixture verifies that reads and writes cannot cross account boundaries.
type scopedDB struct{ rows map[string]AssetRecord }

func (d *scopedDB) Query(_ context.Context, query string, args ...any) ([]map[string]any, error) {
	if !strings.Contains(query, "channel_assets WHERE scope = ? AND") || len(args) != 2 {
		return nil, fmt.Errorf("unscoped query")
	}
	scope, value := args[0].(string), args[1].(string)
	for key, rec := range d.rows {
		if strings.HasPrefix(key, scope+"\x00") && ((strings.Contains(query, "url_sha256 = ?") && rec.URLSHA256 == value) || (strings.Contains(query, "asset_id = ?") && rec.AssetID == value)) {
			return []map[string]any{{"url_sha256": rec.URLSHA256, "asset_id": rec.AssetID, "file_type": rec.FileType, "status": rec.Status}}, nil
		}
	}
	return nil, nil
}
func (d *scopedDB) Exec(_ context.Context, query string, args ...any) (int64, int64, error) {
	if strings.HasPrefix(query, "CREATE TABLE") {
		return 0, 0, nil
	}
	if strings.HasPrefix(query, "INSERT INTO channel_assets") {
		if len(args) != 7 {
			return 0, 0, fmt.Errorf("incorrect scoped insert")
		}
		d.rows[args[0].(string)+"\x00"+args[1].(string)] = AssetRecord{URLSHA256: args[1].(string), OriginalURL: args[2].(string), AssetID: args[3].(string), FileType: args[4].(string), Status: args[5].(string)}
		return 1, 1, nil
	}
	if strings.HasPrefix(query, "UPDATE channel_assets") && strings.Contains(query, "WHERE scope = ? AND asset_id = ?") {
		for key, rec := range d.rows {
			if strings.HasPrefix(key, args[2].(string)+"\x00") && rec.AssetID == args[3] {
				rec.Status = args[0].(string)
				d.rows[key] = rec
			}
		}
		return 1, 0, nil
	}
	return 0, 0, fmt.Errorf("unexpected query")
}

func TestScopedStorePersistenceIsolation(t *testing.T) {
	ctx := context.Background()
	db := &scopedDB{rows: map[string]AssetRecord{}}
	for _, scope := range []string{"direct-account", "relay-account"} {
		store := NewAssetStore(db, time.Hour)
		store.scope = scope
		if err := store.Save(ctx, &AssetRecord{URLSHA256: "same-hash", AssetID: "same-id", FileType: "video", Status: "Processing"}); err != nil {
			t.Fatal(err)
		}
	}
	direct := NewAssetStore(db, time.Hour)
	direct.scope = "direct-account"
	if err := direct.UpdateStatus(ctx, "same-id", "Active", ""); err != nil {
		t.Fatal(err)
	}
	relay := NewAssetStore(db, time.Hour)
	relay.scope = "relay-account"
	rec, found, err := relay.GetByHash(ctx, "same-hash")
	if err != nil || !found || rec.Status != "Processing" {
		t.Fatalf("cross-scope state leak: %+v, %v", rec, err)
	}
	rec, found, err = relay.GetByID(ctx, "same-id")
	if err != nil || !found || rec.Status != "Processing" {
		t.Fatalf("cross-scope id leak: %+v, %v", rec, err)
	}
	other := NewAssetStore(db, time.Hour)
	other.scope = "other-account"
	if _, found, err := other.GetByID(ctx, "same-id"); err != nil || found {
		t.Fatalf("other account reused asset: %v", err)
	}
}
