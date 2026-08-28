package pluginhost

import (
	"context"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
)

// DatabaseProvider returns the active DatabaseProvider plugin.
func (h *Host) DatabaseProvider() (pluginapi.DatabaseProvider, string, bool) {
	if h == nil {
		return nil, "", false
	}
	records := h.activeRecords()
	for _, record := range records {
		if record.plugin.Capabilities.DatabaseProvider != nil && !h.isPluginFused(record.id) {
			return record.plugin.Capabilities.DatabaseProvider, record.id, true
		}
	}
	return nil, "", false
}

// DatabaseQuery executes a query against the active DatabaseProvider plugin.
func (h *Host) DatabaseQuery(ctx context.Context, query string, args ...any) (pluginapi.DatabaseQueryResponse, error) {
	provider, _, ok := h.DatabaseProvider()
	if !ok || provider == nil {
		return pluginapi.DatabaseQueryResponse{}, ErrNoDatabaseProvider
	}
	return provider.Query(ctx, pluginapi.DatabaseQueryRequest{
		Query: query,
		Args:  args,
	})
}

// DatabaseExec executes a statement against the active DatabaseProvider plugin.
func (h *Host) DatabaseExec(ctx context.Context, query string, args ...any) (pluginapi.DatabaseExecResponse, error) {
	provider, _, ok := h.DatabaseProvider()
	if !ok || provider == nil {
		return pluginapi.DatabaseExecResponse{}, ErrNoDatabaseProvider
	}
	return provider.Exec(ctx, pluginapi.DatabaseExecRequest{
		Query: query,
		Args:  args,
	})
}
