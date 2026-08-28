package pluginhost

import (
	"fmt"
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
)

// TestCapabilityRecord describes a capability record for test host setups.
type TestCapabilityRecord struct {
	ID       string
	Priority int
	Plugin   pluginapi.Plugin
}

// NewTestHost creates a Host initialized with the provided test capabilities.
func NewTestHost(records ...TestCapabilityRecord) *Host {
	h := New()
	capRecords := make([]capabilityRecord, 0, len(records))
	for _, r := range records {
		id := strings.TrimSpace(r.ID)
		version := strings.TrimSpace(r.Plugin.Metadata.Version)
		if version == "" {
			version = "test-version"
		}
		capRecords = append(capRecords, capabilityRecord{
			id:       id,
			path:     fmt.Sprintf("testdata/%s.plugin", id),
			version:  version,
			priority: r.Priority,
			meta:     r.Plugin.Metadata,
			plugin:   r.Plugin,
		})
	}
	sortRecords(capRecords)
	h.mu.Lock()
	h.rebuildActivePluginMapsLocked(capRecords)
	h.snapshot.Store(&Snapshot{
		enabled: true,
		records: capRecords,
	})
	h.mu.Unlock()
	return h
}
