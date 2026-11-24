package main

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// DBClient abstracts SQL database operations provided by Host Data Bus.
type DBClient interface {
	Query(ctx context.Context, query string, args ...any) ([]map[string]any, error)
	Exec(ctx context.Context, query string, args ...any) (rowsAffected int64, lastInsertID int64, err error)
}

// AssetRecord represents a row in volcengine_assets.
type AssetRecord struct {
	ID           int64     `json:"id"`
	URLSHA256    string    `json:"url_sha256"`
	OriginalURL  string    `json:"original_url"`
	AssetID      string    `json:"asset_id"`
	FileType     string    `json:"file_type"`
	Status       string    `json:"status"`
	ErrorMessage string    `json:"error_message"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type cachedEntry struct {
	record    *AssetRecord
	expiresAt time.Time
}

// AssetStore provides database persistence and memory caching for Volcengine assets.
type AssetStore struct {
	schemaOnce sync.Once
	schemaErr  error
	scope      string
	db         DBClient
	ttl        time.Duration
	mu         sync.RWMutex
	cache      map[string]cachedEntry
}

// NewAssetStore creates a new AssetStore instance.
func NewAssetStore(db DBClient, ttl time.Duration) *AssetStore {
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	return &AssetStore{
		db:    db,
		ttl:   ttl,
		cache: make(map[string]cachedEntry),
	}
}

// GetByHash retrieves an asset record by URL SHA256 hash.
func (s *AssetStore) GetByHash(ctx context.Context, hash string) (*AssetRecord, bool, error) {
	if err := s.ensureScopedSchema(ctx); err != nil {
		return nil, false, err
	}
	if hash == "" {
		return nil, false, nil
	}

	// 1. Check memory cache
	s.mu.RLock()
	entry, ok := s.cache[hash]
	if ok && time.Now().Before(entry.expiresAt) {
		rec := *entry.record
		s.mu.RUnlock()
		return &rec, true, nil
	}
	s.mu.RUnlock()

	// 2. Query Database
	if s.db == nil {
		return nil, false, nil
	}

	query := "SELECT id, url_sha256, original_url, asset_id, file_type, status, error_message FROM volcengine_assets WHERE url_sha256 = ? LIMIT 1"
	args := []any{hash}
	if s.scope != "" {
		query = "SELECT id, url_sha256, original_url, asset_id, file_type, status, error_message FROM channel_assets WHERE scope = ? AND url_sha256 = ? LIMIT 1"
		args = []any{s.scope, hash}
	}
	rows, errQuery := s.db.Query(ctx, query, args...)
	if errQuery != nil {
		return nil, false, fmt.Errorf("query asset by hash: %w", errQuery)
	}
	if len(rows) == 0 {
		return nil, false, nil
	}

	rec := parseAssetRow(rows[0])
	s.putCache(hash, rec)
	return rec, true, nil
}

// GetByID retrieves an asset record by asset ID.
func (s *AssetStore) GetByID(ctx context.Context, assetID string) (*AssetRecord, bool, error) {
	if err := s.ensureScopedSchema(ctx); err != nil {
		return nil, false, err
	}
	if assetID == "" {
		return nil, false, nil
	}
	s.mu.RLock()
	for _, entry := range s.cache {
		if entry.record.AssetID == assetID && time.Now().Before(entry.expiresAt) {
			rec := *entry.record
			s.mu.RUnlock()
			return &rec, true, nil
		}
	}
	s.mu.RUnlock()
	if s.db == nil {
		return nil, false, nil
	}

	query := "SELECT id, url_sha256, original_url, asset_id, file_type, status, error_message FROM volcengine_assets WHERE asset_id = ? LIMIT 1"
	args := []any{assetID}
	if s.scope != "" {
		query = "SELECT id, url_sha256, original_url, asset_id, file_type, status, error_message FROM channel_assets WHERE scope = ? AND asset_id = ? LIMIT 1"
		args = []any{s.scope, assetID}
	}
	rows, errQuery := s.db.Query(ctx, query, args...)
	if errQuery != nil {
		return nil, false, fmt.Errorf("query asset by id: %w", errQuery)
	}
	if len(rows) == 0 {
		return nil, false, nil
	}

	rec := parseAssetRow(rows[0])
	if rec.URLSHA256 != "" {
		s.putCache(rec.URLSHA256, rec)
	}
	return rec, true, nil
}

// Save inserts or updates an asset record.
func (s *AssetStore) Save(ctx context.Context, rec *AssetRecord) error {
	if err := s.ensureScopedSchema(ctx); err != nil {
		return err
	}
	if rec == nil || rec.URLSHA256 == "" {
		return nil
	}
	if s.db == nil {
		s.putCache(rec.URLSHA256, rec)
		return nil
	}

	query := `
INSERT INTO volcengine_assets (url_sha256, original_url, asset_id, file_type, status, error_message)
VALUES (?, ?, ?, ?, ?, ?)
ON DUPLICATE KEY UPDATE
    asset_id = VALUES(asset_id),
    file_type = VALUES(file_type),
    status = VALUES(status),
    error_message = VALUES(error_message);`

	args := []any{rec.URLSHA256, rec.OriginalURL, rec.AssetID, rec.FileType, rec.Status, rec.ErrorMessage}
	if s.scope != "" {
		query = `INSERT INTO channel_assets (scope, url_sha256, original_url, asset_id, file_type, status, error_message)
   VALUES (?, ?, ?, ?, ?, ?, ?) ON DUPLICATE KEY UPDATE asset_id = VALUES(asset_id), file_type = VALUES(file_type), status = VALUES(status), error_message = VALUES(error_message)`
		args = append([]any{s.scope}, args...)
	}
	_, _, errExec := s.db.Exec(ctx, query, args...)
	if errExec != nil {
		return fmt.Errorf("save asset: %w", errExec)
	}

	s.putCache(rec.URLSHA256, rec)
	return nil
}

// UpdateStatus updates the status and error message of an asset by its asset ID.
func (s *AssetStore) UpdateStatus(ctx context.Context, assetID, status, errorMessage string) error {
	if err := s.ensureScopedSchema(ctx); err != nil {
		return err
	}
	if assetID == "" {
		return nil
	}

	query := "UPDATE volcengine_assets SET status = ?, error_message = ? WHERE asset_id = ?"
	args := []any{status, errorMessage, assetID}
	if s.scope != "" {
		query = "UPDATE channel_assets SET status = ?, error_message = ? WHERE scope = ? AND asset_id = ?"
		args = []any{status, errorMessage, s.scope, assetID}
	}
	if s.db != nil {
		_, _, errExec := s.db.Exec(ctx, query, args...)
		if errExec != nil {
			return fmt.Errorf("update asset status: %w", errExec)
		}
	}

	// Update cached record if present
	s.mu.Lock()
	for _, entry := range s.cache {
		if entry.record != nil && entry.record.AssetID == assetID {
			entry.record.Status = status
			entry.record.ErrorMessage = errorMessage
		}
	}
	s.mu.Unlock()

	return nil
}

func (s *AssetStore) ensureScopedSchema(ctx context.Context) error {
	if s.scope == "" || s.db == nil {
		return nil
	}
	s.schemaOnce.Do(func() { _, _, s.schemaErr = s.db.Exec(ctx, SchemaChannelAssets) })
	if s.schemaErr != nil {
		return fmt.Errorf("ensure scoped asset cache: %w", s.schemaErr)
	}
	return nil
}

func (s *AssetStore) putCache(hash string, rec *AssetRecord) {
	if hash == "" || rec == nil {
		return
	}
	s.mu.Lock()
	copyRecord := *rec
	s.cache[hash] = cachedEntry{
		record:    &copyRecord,
		expiresAt: time.Now().Add(s.ttl),
	}
	s.mu.Unlock()
}

func parseAssetRow(row map[string]any) *AssetRecord {
	rec := &AssetRecord{}
	if v, ok := row["id"].(int64); ok {
		rec.ID = v
	}
	if v, ok := row["url_sha256"].(string); ok {
		rec.URLSHA256 = v
	}
	if v, ok := row["original_url"].(string); ok {
		rec.OriginalURL = v
	}
	if v, ok := row["asset_id"].(string); ok {
		rec.AssetID = v
	}
	if v, ok := row["file_type"].(string); ok {
		rec.FileType = v
	}
	if v, ok := row["status"].(string); ok {
		rec.Status = v
	}
	if v, ok := row["error_message"].(string); ok {
		rec.ErrorMessage = v
	}
	return rec
}
