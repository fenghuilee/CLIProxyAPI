package main

/*
#include <stdint.h>
#include <stdlib.h>
#include <string.h>

typedef struct {
	void* ptr;
	size_t len;
} cliproxy_buffer;

typedef struct {
	uint32_t abi_version;
	void* host_ctx;
	void* call;
	void* free_buffer;
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
	pluginName       = "aigc-media-persist"
	pluginAuthor     = "FenghuiLee"
	pluginRepository = "https://github.com/fenghuilee/cpa-plugin-aigc-media-persist"
)

var pluginVersion = "v2026-01-01 (build 00:00:00)"

var (
	pluginInstance *Plugin
	pluginOnce     sync.Once
)

// Plugin implements CPA C-ABI plugin interface.
type Plugin struct {
	mu         sync.Mutex
	cfg        Config
	uploader   ObjectUploader
	transferer MediaTransferer
	mutator    *Mutator
}

func getPlugin() *Plugin {
	pluginOnce.Do(func() {
		pluginInstance = &Plugin{}
	})
	return pluginInstance
}

//export cliproxy_plugin_init
func cliproxy_plugin_init(_ *C.cliproxy_host_api, plugin *C.cliproxy_plugin_api) C.int {
	if plugin == nil {
		return 1
	}
	plugin.abi_version = C.uint32_t(pluginabi.ABIVersion)
	plugin.call = C.cliproxy_plugin_call_fn(C.cliproxy_plugin_call)
	plugin.free_buffer = C.cliproxy_plugin_free_fn(C.cliproxy_plugin_free)
	plugin.shutdown = C.cliproxy_plugin_shutdown_fn(C.cliproxy_plugin_shutdown)
	return 0
}

//export cliproxy_plugin_call
func cliproxy_plugin_call(method *C.char, requestData *C.uint8_t, requestLen C.size_t, responseBuffer *C.cliproxy_buffer) C.int {
	if responseBuffer != nil {
		responseBuffer.ptr = nil
		responseBuffer.len = 0
	}
	if method == nil {
		writeResponse(responseBuffer, errorEnvelope("invalid_method", "method is required"))
		return 1
	}

	var reqBytes []byte
	if requestData != nil && requestLen > 0 {
		reqBytes = C.GoBytes(unsafe.Pointer(requestData), C.int(requestLen))
	}

	raw, code := handleMethod(C.GoString(method), reqBytes)
	writeResponse(responseBuffer, raw)
	return C.int(code)
}

//export cliproxy_plugin_free
func cliproxy_plugin_free(ptr unsafe.Pointer, _ C.size_t) {
	if ptr != nil {
		C.free(ptr)
	}
}

//export cliproxy_plugin_shutdown
func cliproxy_plugin_shutdown() {
	p := getPlugin()
	p.mu.Lock()
	defer p.mu.Unlock()
	p.uploader = nil
	p.transferer = nil
	p.mutator = nil
}

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
		return errorEnvelope("method_not_found", fmt.Sprintf("unsupported method %q", method)), 0
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
		_ = json.Unmarshal(request, &req)
	}

	cfg := LoadConfig(req.ConfigYAML)

	p.mu.Lock()
	defer p.mu.Unlock()

	p.cfg = cfg
	uploader, errUploader := NewTOSUploader(cfg)
	if errUploader != nil {
		fmt.Printf("aigc-media-persist: tos init note: %v (will graceful fallback to original URLs)\n", errUploader)
		p.uploader = nil
		p.transferer = nil
		p.mutator = nil
	} else {
		p.uploader = uploader
		p.transferer = NewHTTPMediaTransferer(cfg, uploader)
		p.mutator = NewMutator(cfg, p.transferer)
	}

	meta := pluginapi.Metadata{
		Name:             pluginName,
		Version:          pluginVersion,
		Author:           pluginAuthor,
		GitHubRepository: pluginRepository,
		ConfigFields: []pluginapi.ConfigField{
			{
				Name:        "endpoint",
				Type:        pluginapi.ConfigFieldTypeString,
				Description: "火山引擎 TOS Endpoint，例如 https://tos-cn-beijing.volces.com",
			},
			{
				Name:        "region",
				Type:        pluginapi.ConfigFieldTypeString,
				Description: "火山引擎 TOS 区域，例如 cn-beijing",
			},
			{
				Name:        "bucket",
				Type:        pluginapi.ConfigFieldTypeString,
				Description: "火山引擎 TOS 存储桶名称",
			},
			{
				Name:        "access-key",
				Type:        pluginapi.ConfigFieldTypeString,
				Description: "火山引擎 IAM Access Key",
			},
			{
				Name:        "secret-key",
				Type:        pluginapi.ConfigFieldTypeString,
				Description: "火山引擎 IAM Secret Key",
			},
			{
				Name:        "public-base-url",
				Type:        pluginapi.ConfigFieldTypeString,
				Description: "对象公网访问域名或 CDN 加速域名 (可选)",
			},
			{
				Name:        "output-prefix",
				Type:        pluginapi.ConfigFieldTypeString,
				Description: "TOS 产物视频/图片存储前缀，默认 aigc/outputs",
			},
			{
				Name:        "max-file-bytes",
				Type:        pluginapi.ConfigFieldTypeInteger,
				Description: "单文件下载上限字节数，默认 209715200 (200MB)",
			},
			{
				Name:        "download-timeout-seconds",
				Type:        pluginapi.ConfigFieldTypeInteger,
				Description: "下载超时时间 (秒)，默认 60",
			},
			{
				Name:        "concurrency",
				Type:        pluginapi.ConfigFieldTypeInteger,
				Description: "并发转存上限，默认 4",
			},
			{
				Name:        "system-only",
				Type:        pluginapi.ConfigFieldTypeBoolean,
				Description: "仅转存系统内置任务 (user_id == 0)，默认 false (转存全量)",
			},
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
	p.mu.Lock()
	mutator := p.mutator
	p.mu.Unlock()

	var req aigc.GenerationMutationRequest
	if err := json.Unmarshal(request, &req); err != nil {
		return errorEnvelope("invalid_request", err.Error())
	}

	if mutator == nil {
		// If TOS is not configured, preserve original artifacts without rejecting
		return mustEnvelope(aigc.GenerationMutationResponse{
			Generation: &req.Generation,
			Artifacts:  req.Generation.Artifacts,
		})
	}

	resp, errMutate := mutator.MutateContentGeneration(context.Background(), req)
	if errMutate != nil {
		// Graceful fallback: return original artifacts rather than failing
		return mustEnvelope(aigc.GenerationMutationResponse{
			Generation: &req.Generation,
			Artifacts:  req.Generation.Artifacts,
		})
	}
	return mustEnvelope(resp)
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

func main() {}
