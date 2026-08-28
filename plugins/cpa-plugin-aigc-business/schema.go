package main

import (
	"context"
	"fmt"
)

// DDL for AIGC content generation core tables
const (
	SchemaContentGenerations = `
CREATE TABLE IF NOT EXISTS ` + "`content_generations`" + ` (
    ` + "`generation_id`" + ` VARCHAR(64) NOT NULL COMMENT '内容生成全局唯一ID (cg_xxxx)',
    ` + "`request_id`" + ` VARCHAR(64) NULL COMMENT '关联发起请求的唯一追踪 ID (req_xxx)',
    ` + "`kind`" + ` VARCHAR(32) NOT NULL COMMENT 'video / image / audio',
    ` + "`model`" + ` VARCHAR(128) NOT NULL,
    ` + "`status`" + ` VARCHAR(32) NOT NULL,
    ` + "`stage`" + ` VARCHAR(32) NOT NULL,
    ` + "`progress`" + ` INT NOT NULL DEFAULT 0,
    ` + "`revision`" + ` BIGINT UNSIGNED NOT NULL DEFAULT 1,
    ` + "`api_key`" + ` VARCHAR(255) NULL,
    ` + "`client_ip`" + ` VARCHAR(64) NULL,
    ` + "`input`" + ` LONGTEXT NULL,
    ` + "`prepared_input`" + ` LONGTEXT NULL,
    ` + "`provider_request`" + ` LONGTEXT NULL,
    ` + "`provider_response`" + ` LONGTEXT NULL,
    ` + "`output`" + ` LONGTEXT NULL,
    ` + "`provider`" + ` VARCHAR(64) NULL,
    ` + "`provider_task_id`" + ` VARCHAR(255) NULL,
    ` + "`auth_id`" + ` VARCHAR(255) NULL,
    ` + "`error_code`" + ` VARCHAR(64) NULL,
    ` + "`error_message`" + ` TEXT NULL,
    ` + "`metadata`" + ` LONGTEXT NULL,
    ` + "`billing_usage`" + ` LONGTEXT NULL,
    ` + "`worker_id`" + ` VARCHAR(64) NULL,
    ` + "`lease_until`" + ` DATETIME(3) NULL,
    ` + "`created_at`" + ` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    ` + "`updated_at`" + ` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    ` + "`expires_at`" + ` DATETIME(3) NULL,
    PRIMARY KEY (` + "`generation_id`" + `),
    INDEX ` + "`idx_cg_request_id`" + ` (` + "`request_id`" + `),
    INDEX ` + "`idx_cg_kind_status`" + ` (` + "`kind`" + `, ` + "`status`" + `),
    INDEX ` + "`idx_cg_status_created`" + ` (` + "`status`" + `, ` + "`created_at`" + `),
    INDEX ` + "`idx_cg_api_key`" + ` (` + "`api_key`" + `),
    INDEX ` + "`idx_cg_provider_task`" + ` (` + "`provider_task_id`" + `),
    INDEX ` + "`idx_cg_lease`" + ` (` + "`lease_until`" + `),
    INDEX ` + "`idx_cg_expires`" + ` (` + "`expires_at`" + `)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='AIGC 内容生成任务主表';`

	SchemaContentGenerationEvents = `
CREATE TABLE IF NOT EXISTS ` + "`content_generation_events`" + ` (
    ` + "`id`" + ` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    ` + "`generation_id`" + ` VARCHAR(64) NOT NULL,
    ` + "`revision`" + ` BIGINT UNSIGNED NOT NULL,
    ` + "`event_type`" + ` VARCHAR(64) NOT NULL,
    ` + "`stage`" + ` VARCHAR(32) NOT NULL,
    ` + "`status`" + ` VARCHAR(32) NOT NULL,
    ` + "`plugin_id`" + ` VARCHAR(64) NULL,
    ` + "`payload`" + ` LONGTEXT NULL,
    ` + "`created_at`" + ` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (` + "`id`" + `),
    INDEX ` + "`idx_cge_gen_id`" + ` (` + "`generation_id`" + `),
    INDEX ` + "`idx_cge_event_type`" + ` (` + "`event_type`" + `, ` + "`created_at`" + `)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='AIGC 内容生成事件流表';`

	SchemaContentGenerationArtifacts = `
CREATE TABLE IF NOT EXISTS ` + "`content_generation_artifacts`" + ` (
    ` + "`id`" + ` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    ` + "`generation_id`" + ` VARCHAR(64) NOT NULL,
    ` + "`artifact_type`" + ` VARCHAR(64) NOT NULL,
    ` + "`storage_provider`" + ` VARCHAR(64) NOT NULL,
    ` + "`uri`" + ` VARCHAR(1024) NOT NULL,
    ` + "`mime_type`" + ` VARCHAR(64) NULL,
    ` + "`size_bytes`" + ` BIGINT NOT NULL DEFAULT 0,
    ` + "`sha256`" + ` VARCHAR(64) NULL,
    ` + "`metadata`" + ` LONGTEXT NULL,
    ` + "`created_at`" + ` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    ` + "`expires_at`" + ` DATETIME(3) NULL,
    PRIMARY KEY (` + "`id`" + `),
    INDEX ` + "`idx_cga_gen_id`" + ` (` + "`generation_id`" + `),
    INDEX ` + "`idx_cga_artifact_type`" + ` (` + "`artifact_type`" + `),
    INDEX ` + "`idx_cga_expires`" + ` (` + "`expires_at`" + `)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='AIGC 内容生成产物元数据表';`
)

// EnsureSchema executes DDL statements to ensure all AIGC tables exist.
func EnsureSchema(ctx context.Context, bridge DBBridge) error {
	if bridge == nil {
		return fmt.Errorf("db bridge is nil")
	}

	statements := []string{
		SchemaContentGenerations,
		SchemaContentGenerationEvents,
		SchemaContentGenerationArtifacts,
	}

	for _, stmt := range statements {
		if _, _, err := bridge.Exec(ctx, stmt); err != nil {
			return fmt.Errorf("ensure aigc schema: %w", err)
		}
	}
	return nil
}
