package main

import (
	"context"
	"fmt"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
	"gorm.io/gorm"
)

// DatabaseProviderService provides generic database query and exec capabilities.
type DatabaseProviderService struct {
	db *gorm.DB
}

// NewDatabaseProvider creates a new DatabaseProviderService instance.
func NewDatabaseProvider(db *gorm.DB) *DatabaseProviderService {
	return &DatabaseProviderService{db: db}
}

// sanitizeSQLArgs converts whole-number float64 values to int64 for safe MySQL parameter binding (e.g., LIMIT ?).
func sanitizeSQLArgs(args []any) []any {
	if len(args) == 0 {
		return args
	}
	clean := make([]any, len(args))
	for i, a := range args {
		if f, ok := a.(float64); ok && f == float64(int64(f)) {
			clean[i] = int64(f)
		} else {
			clean[i] = a
		}
	}
	return clean
}

// Query executes a parameterized SQL SELECT query and returns rows as key-value maps.
func (s *DatabaseProviderService) Query(ctx context.Context, req pluginapi.DatabaseQueryRequest) (pluginapi.DatabaseQueryResponse, error) {
	if s == nil || s.db == nil {
		return pluginapi.DatabaseQueryResponse{}, fmt.Errorf("database not initialized")
	}

	sqlDB, err := s.db.DB()
	if err != nil {
		return pluginapi.DatabaseQueryResponse{}, fmt.Errorf("get underlying db: %w", err)
	}

	args := sanitizeSQLArgs(req.Args)
	rows, errQuery := sqlDB.QueryContext(ctx, req.Query, args...)
	if errQuery != nil {
		return pluginapi.DatabaseQueryResponse{}, fmt.Errorf("execute query: %w", errQuery)
	}
	defer rows.Close()

	columns, errCols := rows.Columns()
	if errCols != nil {
		return pluginapi.DatabaseQueryResponse{}, fmt.Errorf("get columns: %w", errCols)
	}

	var results []map[string]any
	colTypes, _ := rows.ColumnTypes()

	for rows.Next() {
		values := make([]any, len(columns))
		valuePtrs := make([]any, len(columns))
		for i := range columns {
			valuePtrs[i] = &values[i]
		}

		if errScan := rows.Scan(valuePtrs...); errScan != nil {
			return pluginapi.DatabaseQueryResponse{}, fmt.Errorf("scan row: %w", errScan)
		}

		rowMap := make(map[string]any, len(columns))
		for i, col := range columns {
			val := values[i]
			if b, ok := val.([]byte); ok {
				if colTypes != nil && i < len(colTypes) {
					typeName := colTypes[i].DatabaseTypeName()
					if typeName == "JSON" || typeName == "TEXT" || typeName == "VARCHAR" || typeName == "CHAR" || typeName == "LONGTEXT" {
						rowMap[col] = string(b)
						continue
					}
				}
				rowMap[col] = string(b)
			} else {
				rowMap[col] = val
			}
		}
		results = append(results, rowMap)
	}

	if errRows := rows.Err(); errRows != nil {
		return pluginapi.DatabaseQueryResponse{}, fmt.Errorf("rows iteration: %w", errRows)
	}

	return pluginapi.DatabaseQueryResponse{
		Rows:    results,
		Columns: columns,
	}, nil
}

// Exec executes a parameterized SQL modification (INSERT, UPDATE, DELETE, DDL).
func (s *DatabaseProviderService) Exec(ctx context.Context, req pluginapi.DatabaseExecRequest) (pluginapi.DatabaseExecResponse, error) {
	if s == nil || s.db == nil {
		return pluginapi.DatabaseExecResponse{}, fmt.Errorf("database not initialized")
	}

	sqlDB, err := s.db.DB()
	if err != nil {
		return pluginapi.DatabaseExecResponse{}, fmt.Errorf("get underlying db: %w", err)
	}

	args := sanitizeSQLArgs(req.Args)
	result, errExec := sqlDB.ExecContext(ctx, req.Query, args...)
	if errExec != nil {
		return pluginapi.DatabaseExecResponse{}, fmt.Errorf("exec statement: %w", errExec)
	}

	rowsAffected, _ := result.RowsAffected()
	lastInsertID, _ := result.LastInsertId()

	return pluginapi.DatabaseExecResponse{
		RowsAffected: rowsAffected,
		LastInsertID: lastInsertID,
	}, nil
}

var _ pluginapi.DatabaseProvider = (*DatabaseProviderService)(nil)
