package main

import (
	"context"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
	"gorm.io/gorm"
)

func setupTestDBProvider(t *testing.T) (*DatabaseProviderService, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open sqlite in-memory: %v", err)
	}

	provider := NewDatabaseProvider(db)
	return provider, db
}

func TestDatabaseProvider_DynamicDDLAndCRUD(t *testing.T) {
	provider, _ := setupTestDBProvider(t)
	ctx := context.Background()

	// 1. Test Dynamic DDL CREATE TABLE
	createTableReq := pluginapi.DatabaseExecRequest{
		Query: `CREATE TABLE IF NOT EXISTS test_custom_table (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			item_name TEXT NOT NULL,
			item_val INTEGER DEFAULT 0
		)`,
	}
	_, errDDL := provider.Exec(ctx, createTableReq)
	if errDDL != nil {
		t.Fatalf("Dynamic DDL CREATE TABLE failed: %v", errDDL)
	}

	// 2. Test Exec INSERT
	insertReq := pluginapi.DatabaseExecRequest{
		Query: "INSERT INTO test_custom_table (item_name, item_val) VALUES (?, ?)",
		Args:  []any{"item-alpha", 42},
	}
	execResp, errExec := provider.Exec(ctx, insertReq)
	if errExec != nil {
		t.Fatalf("Exec INSERT failed: %v", errExec)
	}
	if execResp.RowsAffected != 1 {
		t.Fatalf("Exec INSERT rows affected = %d, want 1", execResp.RowsAffected)
	}

	// 3. Test Query SELECT
	queryReq := pluginapi.DatabaseQueryRequest{
		Query: "SELECT item_name, item_val FROM test_custom_table WHERE item_name = ?",
		Args:  []any{"item-alpha"},
	}
	queryResp, errQuery := provider.Query(ctx, queryReq)
	if errQuery != nil {
		t.Fatalf("Query SELECT failed: %v", errQuery)
	}
	if len(queryResp.Rows) != 1 {
		t.Fatalf("Query rows count = %d, want 1", len(queryResp.Rows))
	}
	if queryResp.Rows[0]["item_name"] != "item-alpha" {
		t.Fatalf("Query row data mismatch: %+v", queryResp.Rows[0])
	}
}
