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

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/aigc"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginabi"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
)

const (
	pluginName       = "volcengine-asset"
	pluginAuthor     = "FenghuiLee"
	pluginRepository = "https://github.com/fenghuilee/cpa-plugin-volcengine-asset"
)

var (
	pluginVersion = "v2026-01-01 (build 00:00:00)"
)

var (
	pluginInstance *Plugin
	pluginOnce     sync.Once
)

// Plugin manages the Volcengine Asset Mutator lifecycle and host integration.
type Plugin struct {
	mu      sync.RWMutex
	cfg     Config
	mutator *AssetRouter
}

func getPlugin() *Plugin {
	pluginOnce.Do(func() {
		cfg := DefaultConfig()
		cfg.Normalize()
		db := &hostDBClient{}
		pluginInstance = &Plugin{
			cfg:     cfg,
			mutator: NewAssetRouter(cfg, db),
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

	var requestBytes []byte
	if request != nil && requestLen > 0 {
		requestBytes = C.GoBytes(unsafe.Pointer(request), C.int(requestLen))
	}
	raw, code := handleMethod(C.GoString(method), requestBytes)
	writeResponse(response, raw)
	return C.int(code)
}

//export cliproxy_plugin_free
func cliproxy_plugin_free(ptr unsafe.Pointer, _ C.size_t) {
	if ptr != nil {
		C.free(ptr)
	}
}

//export cliproxy_plugin_shutdown
func cliproxy_plugin_shutdown() {}

func handleMethod(method string, request []byte) ([]byte, int) {
	p := getPlugin()

	switch method {
	case pluginabi.MethodPluginRegister:
		return p.handleRegister(request), 0
	case pluginabi.MethodPluginReconfigure:
		return p.handleRegister(request), 0
	case pluginabi.MethodPluginShutdown:
		return mustEnvelope(nil), 0

	case pluginabi.MethodContentGenerationMutate:
		return p.handleMutate(request), 0

	default:
		return errorEnvelope("unsupported_method", fmt.Sprintf("method not supported: %s", method)), 0
	}
}

type rpcLifecycleRequest struct {
	ConfigYAML    []byte `json:"config_yaml"`
	SchemaVersion uint32 `json:"schema_version"`
}

type rpcRegistration struct {
	SchemaVersion uint32             `json:"schema_version"`
	Metadata      pluginapi.Metadata `json:"metadata"`
	Capabilities  map[string]any     `json:"capabilities"`
}

func (p *Plugin) handleRegister(request []byte) []byte {
	var req rpcLifecycleRequest
	if len(request) > 0 {
		if err := json.Unmarshal(request, &req); err != nil {
			return errorEnvelope("invalid_request", fmt.Sprintf("unmarshal registration request: %v", err))
		}
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	cfg, errConfig := ParseConfig(req.ConfigYAML)
	if errConfig != nil {
		return errorEnvelope("invalid_config", errConfig.Error())
	}

	db := &hostDBClient{}
	mutator := NewAssetRouter(cfg, db)
	for _, route := range mutator.routes {
		if route.initErr != nil {
			return errorEnvelope("invalid_config", route.initErr.Error())
		}
	}

	p.cfg = cfg
	p.mutator = mutator

	meta := pluginapi.Metadata{
		Name:             pluginName,
		Version:          pluginVersion,
		Author:           pluginAuthor,
		GitHubRepository: pluginRepository,
		ConfigFields: []pluginapi.ConfigField{
			{Name: "volcengine", Type: pluginapi.ConfigFieldTypeObject, Description: "火山素材渠道 (enabled/models/access_key/secret_key/project/group_id/region)"},
			{Name: "chinadatapay", Type: pluginapi.ConfigFieldTypeObject, Description: "中转素材渠道 (enabled/models/token/group_id)，地址内置，group_id 可选"},
			{Name: "runtime", Type: pluginapi.ConfigFieldTypeObject, Description: "共享运行参数 (poll_interval_ms/poll_timeout_seconds/cache_ttl_seconds)"},
		},
	}

	caps := map[string]any{
		"content_generation_mutator": true,
	}

	reg := rpcRegistration{
		SchemaVersion: pluginabi.SchemaVersionAIGC,
		Metadata:      meta,
		Capabilities:  caps,
	}

	return mustEnvelope(reg)
}

func (p *Plugin) handleMutate(request []byte) []byte {
	var req aigc.GenerationMutationRequest
	if err := json.Unmarshal(request, &req); err != nil {
		return errorEnvelope("invalid_request", fmt.Sprintf("unmarshal mutation request: %v", err))
	}

	p.mu.RLock()
	mutator := p.mutator
	p.mu.RUnlock()

	if mutator == nil {
		return mustEnvelope(aigc.GenerationMutationResponse{Draft: req.Draft})
	}

	resp, errMutate := mutator.MutateContentGeneration(context.Background(), req)
	if errMutate != nil {
		return errorEnvelope("mutation_failed", errMutate.Error())
	}

	return mustEnvelope(resp)
}

type hostDBClient struct{}

func (h *hostDBClient) Query(_ context.Context, query string, args ...any) ([]map[string]any, error) {
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

func (h *hostDBClient) Exec(_ context.Context, query string, args ...any) (int64, int64, error) {
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
			return nil, fmt.Errorf("host error [%s]: %s", env.Error.Code, env.Error.Message)
		}
		return nil, fmt.Errorf("host error: unknown")
	}
	return env.Result, nil
}

func writeResponse(target *C.cliproxy_buffer, data []byte) {
	if target == nil || len(data) == 0 {
		return
	}
	ptr := C.malloc(C.size_t(len(data)))
	if ptr == nil {
		return
	}
	C.memcpy(ptr, unsafe.Pointer(&data[0]), C.size_t(len(data)))
	target.ptr = ptr
	target.len = C.size_t(len(data))
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
