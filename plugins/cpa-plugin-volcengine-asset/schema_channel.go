package main

// A separate scoped table leaves legacy records intact and prevents cross-account reuse.
// Column comments mirror SchemaVolcengineAssets so both asset tables stay
// self-documenting in schema inspection tools.
const SchemaChannelAssets = `CREATE TABLE IF NOT EXISTS channel_assets (
 id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY COMMENT '自增主键 ID',
 scope VARCHAR(96) NOT NULL COMMENT '渠道/账号作用域哈希，凭据不以明文落库',
 url_sha256 CHAR(64) NOT NULL COMMENT '媒体类型与原始 URL 组合的 SHA256 哈希值',
 original_url TEXT NOT NULL COMMENT '原始 HTTP/HTTPS 链接',
 asset_id VARCHAR(128) NOT NULL COMMENT '火山方舟 Asset ID (如 asset-20260818...)',
 file_type VARCHAR(32) NOT NULL COMMENT '资源类型: image, video, audio',
 status VARCHAR(32) NOT NULL COMMENT '资源状态: Processing, Active, Failed',
 error_message TEXT NULL COMMENT '失败时的错误信息',
 created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间',
 updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP COMMENT '更新时间',
 UNIQUE KEY uk_scope_url (scope, url_sha256),
 INDEX idx_scope_asset (scope, asset_id)
 ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='(生产)_渠道级多模态Asset资源映射表';`
