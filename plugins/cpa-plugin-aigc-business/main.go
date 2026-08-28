package main

/*
#include <stdint.h>
#include <stdlib.h>
#include <string.h>

typedef struct {
	void* ptr;
	size_t len;
} cliproxy_buffer;

typedef int (*cliproxy_host_call_fn)(void*, const char*, const uint8_t*, size_t, cliproxy_buffer*);
typedef void (*cliproxy_host_free_fn)(void*, size_t);

typedef struct {
	uint32_t abi_version;
	void* host_ctx;
	cliproxy_host_call_fn call;
	cliproxy_host_free_fn free_buffer;
} cliproxy_host_api;

typedef int (*cliproxy_plugin_call_fn)(char*, uint8_t*, size_t, cliproxy_buffer*);
typedef void (*cliproxy_plugin_free_fn)(void*, size_t);
typedef void (*cliproxy_plugin_shutdown_fn)(void);

typedef struct {
	uint32_t abi_version;
	cliproxy_plugin_call_fn call;
	cliproxy_plugin_free_fn free_buffer;
	cliproxy_plugin_shutdown_fn shutdown;
} cliproxy_plugin_api;

static const cliproxy_host_api* stored_host;

static void store_host_api(const cliproxy_host_api* host) {
	stored_host = host;
}

static int call_host_api(const char* method, const uint8_t* request, size_t request_len, cliproxy_buffer* response) {
	if (stored_host == NULL || stored_host->call == NULL) {
		return 1;
	}
	return stored_host->call(stored_host->host_ctx, method, request, request_len, response);
}

static void free_host_buffer(void* ptr, size_t len) {
	if (stored_host != NULL && stored_host->free_buffer != NULL && ptr != NULL) {
		stored_host->free_buffer(ptr, len);
	}
}

extern int cliproxy_plugin_call(char*, uint8_t*, size_t, cliproxy_buffer*);
extern void cliproxy_plugin_free(void*, size_t);
extern void cliproxy_plugin_shutdown(void);
*/
import "C"

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"unsafe"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/aigc"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginabi"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
)

const (
	pluginName       = "aigc-business"
	pluginAuthor     = "FenghuiLee"
	pluginRepository = "https://github.com/fenghuilee/cpa-plugin-aigc-business"
)

var (
	pluginVersion = "v2026-01-01 (build 00:00:00)"
)

var (
	pluginInstance *Plugin
	pluginOnce     sync.Once
)

type Plugin struct {
	mu    sync.RWMutex
	store *Store
}

func getPlugin() *Plugin {
	pluginOnce.Do(func() {
		bridge := &hostDBBridge{}
		pluginInstance = &Plugin{
			store: NewStore(bridge),
		}
	})
	return pluginInstance
}

func main() {}

//export cliproxy_plugin_init
func cliproxy_plugin_init(host *C.cliproxy_host_api, plugin *C.cliproxy_plugin_api) C.int {
	if plugin == nil {
		return 1
	}
	C.store_host_api(host)
	plugin.abi_version = C.uint32_t(pluginabi.ABIVersion)
	plugin.call = C.cliproxy_plugin_call_fn(C.cliproxy_plugin_call)
	plugin.free_buffer = C.cliproxy_plugin_free_fn(C.cliproxy_plugin_free)
	plugin.shutdown = C.cliproxy_plugin_shutdown_fn(C.cliproxy_plugin_shutdown)
	return 0
}

//export cliproxy_plugin_call
func cliproxy_plugin_call(method *C.char, request *C.uint8_t, requestLen C.size_t, response *C.cliproxy_buffer) C.int {
	if response != nil {
		response.ptr = nil
		response.len = 0
	}
	if method == nil {
		writeResponse(response, errorEnvelope("invalid_method", "method name is required"))
		return 1
	}

	var reqBytes []byte
	if request != nil && requestLen > 0 {
		reqBytes = C.GoBytes(unsafe.Pointer(request), C.int(requestLen))
	}

	methodStr := C.GoString(method)
	respBytes, rc := getPlugin().dispatch(context.Background(), methodStr, reqBytes)
	writeResponse(response, respBytes)
	return C.int(rc)
}

//export cliproxy_plugin_free
func cliproxy_plugin_free(ptr unsafe.Pointer, _ C.size_t) {
	if ptr != nil {
		C.free(ptr)
	}
}

//export cliproxy_plugin_shutdown
func cliproxy_plugin_shutdown() {}

func (p *Plugin) dispatch(ctx context.Context, method string, request []byte) ([]byte, int) {
	switch method {
	case pluginabi.MethodPluginRegister:
		return p.handleRegister(request, false), 0
	case pluginabi.MethodPluginReconfigure:
		return p.handleRegister(request, true), 0

	case pluginabi.MethodContentGenerationStoreCreate:
		return p.handleCreate(ctx, request), 0
	case pluginabi.MethodContentGenerationStoreGet:
		return p.handleGet(ctx, request), 0
	case pluginabi.MethodContentGenerationStorePatch:
		return p.handlePatch(ctx, request), 0
	case pluginabi.MethodContentGenerationStoreClaim:
		return p.handleClaim(ctx, request), 0
	case pluginabi.MethodContentGenerationStoreRelease:
		return p.handleRelease(ctx, request), 0
	case pluginabi.MethodContentGenerationStoreDeleteExpired:
		return p.handleDeleteExpired(ctx, request), 0

	default:
		return errorEnvelope("unsupported_method", fmt.Sprintf("method not supported: %s", method)), 0
	}
}

type rpcRegistration struct {
	SchemaVersion uint32             `json:"schema_version"`
	Metadata      pluginapi.Metadata `json:"metadata"`
	Capabilities  capabilities       `json:"capabilities"`
}

type capabilities struct {
	ContentGenerationStore bool `json:"content_generation_store"`
}

func (p *Plugin) handleRegister(_ []byte, _ bool) []byte {
	meta := pluginapi.Metadata{
		Name:             pluginName,
		Version:          pluginVersion,
		Author:           pluginAuthor,
		GitHubRepository: pluginRepository,
		ConfigFields:     []pluginapi.ConfigField{},
	}

	reg := rpcRegistration{
		SchemaVersion: pluginabi.SchemaVersionAIGC,
		Metadata:      meta,
		Capabilities: capabilities{
			ContentGenerationStore: true,
		},
	}

	return mustEnvelope(reg)
}

type rpcCreateRequest struct {
	aigc.GenerationCreateRequest
}

type rpcGetRequest struct {
	aigc.GenerationGetRequest
}

type rpcPatchRequest struct {
	aigc.GenerationPatchRequest
}

type rpcClaimRequest struct {
	aigc.GenerationClaimRequest
}

type rpcReleaseRequest struct {
	aigc.GenerationReleaseRequest
}

type rpcDeleteExpiredRequest struct {
	aigc.GenerationDeleteExpiredRequest
}

type rpcDeleteExpiredResponse struct {
	Deleted int64 `json:"deleted"`
}

func (p *Plugin) handleCreate(ctx context.Context, request []byte) []byte {
	var req rpcCreateRequest
	if len(request) > 0 {
		if err := json.Unmarshal(request, &req); err != nil {
			return errorEnvelope("invalid_request", err.Error())
		}
	}
	res, err := p.store.Create(ctx, req.GenerationCreateRequest)
	if err != nil {
		return errorEnvelope("create_failed", err.Error())
	}
	return mustEnvelope(res)
}

func (p *Plugin) handleGet(ctx context.Context, request []byte) []byte {
	var req rpcGetRequest
	if len(request) > 0 {
		if err := json.Unmarshal(request, &req); err != nil {
			return errorEnvelope("invalid_request", err.Error())
		}
	}
	res, err := p.store.Get(ctx, req.GenerationGetRequest)
	if err != nil {
		return errorEnvelope("get_failed", err.Error())
	}
	return mustEnvelope(res)
}

func (p *Plugin) handlePatch(ctx context.Context, request []byte) []byte {
	var req rpcPatchRequest
	if len(request) > 0 {
		if err := json.Unmarshal(request, &req); err != nil {
			return errorEnvelope("invalid_request", err.Error())
		}
	}
	res, err := p.store.Patch(ctx, req.GenerationPatchRequest)
	if err != nil {
		return errorEnvelope("patch_failed", err.Error())
	}
	return mustEnvelope(res)
}

func (p *Plugin) handleClaim(ctx context.Context, request []byte) []byte {
	var req rpcClaimRequest
	if len(request) > 0 {
		if err := json.Unmarshal(request, &req); err != nil {
			return errorEnvelope("invalid_request", err.Error())
		}
	}
	res, err := p.store.Claim(ctx, req.GenerationClaimRequest)
	if err != nil {
		return errorEnvelope("claim_failed", err.Error())
	}
	return mustEnvelope(res)
}

func (p *Plugin) handleRelease(ctx context.Context, request []byte) []byte {
	var req rpcReleaseRequest
	if len(request) > 0 {
		if err := json.Unmarshal(request, &req); err != nil {
			return errorEnvelope("invalid_request", err.Error())
		}
	}
	if err := p.store.Release(ctx, req.GenerationReleaseRequest); err != nil {
		return errorEnvelope("release_failed", err.Error())
	}
	return mustEnvelope(map[string]any{"ok": true})
}

func (p *Plugin) handleDeleteExpired(ctx context.Context, request []byte) []byte {
	var req rpcDeleteExpiredRequest
	if len(request) > 0 {
		if err := json.Unmarshal(request, &req); err != nil {
			return errorEnvelope("invalid_request", err.Error())
		}
	}
	deleted, err := p.store.DeleteExpired(ctx, req.GenerationDeleteExpiredRequest)
	if err != nil {
		return errorEnvelope("delete_expired_failed", err.Error())
	}
	return mustEnvelope(rpcDeleteExpiredResponse{Deleted: deleted})
}

type hostDBBridge struct{}

func (h *hostDBBridge) Query(_ context.Context, query string, args ...any) ([]map[string]any, error) {
	req := pluginapi.HostDatabaseQueryRequest{
		Query: query,
		Args:  args,
	}
	rawReq, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	result, errCall := callHost(pluginabi.MethodHostDatabaseQuery, rawReq)
	if errCall != nil {
		return nil, errCall
	}

	var resp pluginapi.HostDatabaseQueryResponse
	if err := json.Unmarshal(result, &resp); err != nil {
		return nil, err
	}
	return resp.Rows, nil
}

func (h *hostDBBridge) Exec(_ context.Context, query string, args ...any) (int64, int64, error) {
	req := pluginapi.HostDatabaseExecRequest{
		Query: query,
		Args:  args,
	}
	rawReq, err := json.Marshal(req)
	if err != nil {
		return 0, 0, err
	}

	result, errCall := callHost(pluginabi.MethodHostDatabaseExec, rawReq)
	if errCall != nil {
		return 0, 0, errCall
	}

	var resp pluginapi.HostDatabaseExecResponse
	if err := json.Unmarshal(result, &resp); err != nil {
		return 0, 0, err
	}
	return resp.RowsAffected, resp.LastInsertID, nil
}

func callHost(method string, payload []byte) (json.RawMessage, error) {
	cMethod := C.CString(method)
	defer C.free(unsafe.Pointer(cMethod))

	var reqPtr unsafe.Pointer
	var reqLen C.size_t
	if len(payload) > 0 {
		reqPtr = C.CBytes(payload)
		defer C.free(reqPtr)
		reqLen = C.size_t(len(payload))
	}

	var response C.cliproxy_buffer
	if C.call_host_api(cMethod, (*C.uint8_t)(reqPtr), reqLen, &response) != 0 || response.ptr == nil {
		return nil, fmt.Errorf("host callback failed")
	}
	defer C.free_host_buffer(response.ptr, response.len)

	raw := C.GoBytes(response.ptr, C.int(response.len))
	var env pluginabi.Envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, err
	}
	if !env.OK {
		if env.Error != nil {
			return nil, fmt.Errorf("host error: %s", env.Error.Message)
		}
		return nil, fmt.Errorf("host call returned error status")
	}
	return env.Result, nil
}

func mustEnvelope(v any) []byte {
	var raw json.RawMessage
	if v != nil {
		b, err := json.Marshal(v)
		if err != nil {
			return errorEnvelope("marshal_failed", fmt.Sprintf("failed to marshal envelope result: %v", err))
		}
		raw = json.RawMessage(b)
	}
	b, _ := json.Marshal(pluginabi.Envelope{
		OK:     true,
		Result: raw,
	})
	return b
}

func errorEnvelope(code, message string) []byte {
	b, _ := json.Marshal(pluginabi.Envelope{
		OK: false,
		Error: &pluginabi.Error{
			Code:    code,
			Message: message,
		},
	})
	return b
}

func writeResponse(target *C.cliproxy_buffer, data []byte) {
	if target == nil || len(data) == 0 {
		return
	}
	ptr := C.CBytes(data)
	if ptr == nil {
		return
	}
	target.ptr = ptr
	target.len = C.size_t(len(data))
}
