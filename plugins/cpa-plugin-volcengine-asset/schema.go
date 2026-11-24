package main

import (
	"context"
	"fmt"
)

// SchemaVolcengineAssets contains the DDL statement to create the volcengine_assets table.
const SchemaVolcengineAssets = `
CREATE TABLE IF NOT EXISTS ` + "`volcengine_assets`" + ` (
    ` + "`id`" + ` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT COMMENT '自增主键 ID',
    ` + "`url_sha256`" + ` CHAR(64) NOT NULL COMMENT '原始 URL 的 SHA256 哈希值，用于快速唯一查找',
    ` + "`original_url`" + ` TEXT NOT NULL COMMENT '原始 HTTP/HTTPS 链接',
    ` + "`asset_id`" + ` VARCHAR(128) NOT NULL DEFAULT '' COMMENT '火山方舟 Asset ID (如 asset-20260818...)',
    ` + "`file_type`" + ` VARCHAR(32) NOT NULL DEFAULT 'image' COMMENT '资源类型: image, video, audio',
    ` + "`status`" + ` VARCHAR(32) NOT NULL DEFAULT 'Processing' COMMENT '资源状态: Processing, Active, Failed',
    ` + "`error_message`" + ` TEXT NULL COMMENT '失败时的错误信息',
    ` + "`created_at`" + ` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间',
    ` + "`updated_at`" + ` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP COMMENT '更新时间',
    PRIMARY KEY (` + "`id`" + `),
    UNIQUE KEY ` + "`uk_url_sha256`" + ` (` + "`url_sha256`" + `),
    INDEX ` + "`idx_asset_id`" + ` (` + "`asset_id`" + `),
    INDEX ` + "`idx_status`" + ` (` + "`status`" + `)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='火山方舟多模态 Asset 资源映射表';`

// EnsureSchema creates the volcengine_assets table if it does not exist.
func EnsureSchema(ctx context.Context, client DBClient) error {
	if client == nil {
		return fmt.Errorf("db client is nil")
	}
	if _, _, err := client.Exec(ctx, SchemaVolcengineAssets); err != nil {
		return fmt.Errorf("ensure volcengine_assets schema: %w", err)
	}
	return nil
}
