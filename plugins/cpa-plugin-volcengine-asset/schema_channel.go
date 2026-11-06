package main

// A separate scoped table leaves legacy records intact and prevents cross-account reuse.
const SchemaChannelAssets = `CREATE TABLE IF NOT EXISTS channel_assets (
 id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
 scope VARCHAR(96) NOT NULL,
 url_sha256 CHAR(64) NOT NULL,
 original_url TEXT NOT NULL,
 asset_id VARCHAR(128) NOT NULL,
 file_type VARCHAR(32) NOT NULL,
 status VARCHAR(32) NOT NULL,
 error_message TEXT NULL,
 created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
 updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
 UNIQUE KEY uk_scope_url (scope, url_sha256),
 INDEX idx_scope_asset (scope, asset_id)
 ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;`
