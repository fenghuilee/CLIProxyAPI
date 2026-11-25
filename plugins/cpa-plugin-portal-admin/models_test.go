package main

import (
	"sync"
	"testing"

	"github.com/MoeForge/moego/json"
	"gorm.io/gorm/schema"
)

func TestUsageCacheTokenColumns(t *testing.T) {
	for _, model := range []interface{ TableName() string }{UsageStatistic{}} {
		t.Run(model.TableName(), func(t *testing.T) {
			mapped, err := schema.Parse(model, &sync.Map{}, schema.NamingStrategy{})
			if err != nil {
				t.Fatal(err)
			}
			if mapped.FieldsByDBName["cached_tokens"] != nil {
				t.Fatal("legacy cached_tokens must not be mapped to a database column")
			}
			for _, column := range []string{"cache_read_tokens", "cache_creation_tokens"} {
				if mapped.FieldsByDBName[column] == nil {
					t.Errorf("missing database column mapping for %q", column)
				}
			}
		})
	}
}

func TestUsageCacheTokenJSON(t *testing.T) {
	for _, tc := range []struct {
		name string
		data any
		read int64
	}{
		{"detail", (UsageStatistic{CacheReadTokens: 17, CacheCreationTokens: 29}).ToDTO(), 17},
		{"creation only", (UsageStatistic{CacheCreationTokens: 29}).ToDTO(), 0},
		{"daily aggregate", UsageDailyStat{CacheReadTokens: 17, CacheCreationTokens: 29}, 17},
	} {
		t.Run(tc.name, func(t *testing.T) {
			payload, err := json.Marshal(tc.data)
			if err != nil {
				t.Fatal(err)
			}
			var fields struct {
				Read     *int64 `json:"cache_read_tokens"`
				Creation *int64 `json:"cache_creation_tokens"`
				Legacy   *int64 `json:"cached_tokens"`
			}
			if err := json.Unmarshal(payload, &fields); err != nil {
				t.Fatal(err)
			}
			if fields.Read == nil || *fields.Read != tc.read || fields.Creation == nil || *fields.Creation != 29 {
				t.Fatalf("incorrect canonical cache token counts: %s", payload)
			}
			if _, detail := tc.data.(UsageStatisticDTO); detail {
				if fields.Legacy == nil || *fields.Legacy != tc.read {
					t.Fatalf("legacy alias must equal cache read tokens: %s", payload)
				}
			} else if fields.Legacy != nil {
				t.Fatalf("unexpected legacy alias in aggregate: %s", payload)
			}
		})
	}
}
