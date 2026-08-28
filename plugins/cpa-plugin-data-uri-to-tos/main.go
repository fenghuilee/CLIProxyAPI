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

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/aigc"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginabi"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
	"gopkg.in/yaml.v3"
)

const (
	pluginName       = "data-uri-to-tos"
	pluginAuthor     = "FenghuiLee"
	pluginRepository = "https://github.com/fenghuilee/cpa-plugin-data-uri-to-tos"
)

var (
	pluginVersion = "v2026-01-01 (build 00:00:00)"
)

var (
	pluginInstance *Plugin
	pluginOnce     sync.Once
)

// Plugin manages the TOS uploader and handles mutation calls.
type Plugin struct {
	mu       sync.Mutex
	cfg      Config
	mutator  *Mutator
	uploader ObjectUploader
}

func getPlugin() *Plugin {
	pluginOnce.Do(func() {
		cfg := DefaultConfig()
		pluginInstance = &Plugin{
			cfg: cfg,
		}
	})
	return pluginInstance
}

func main() {}

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
func cliproxy_plugin_call(method *C.char, request *C.uint8_t, requestLen C.size_t, response *C.cliproxy_buffer) C.int {
	if response != nil {
		response.ptr = nil
		response.len = 0
	}
	if method == nil {
		writeResponse(response, errorEnvelope("invalid_method", "method is required"))
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
		_ = json.Unmarshal(request, &req)
	}

	cfg := DefaultConfig()
	if len(req.ConfigYAML) > 0 {
		_ = yaml.Unmarshal(req.ConfigYAML, &cfg)
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	p.cfg = cfg
	uploader, errUploader := NewTOSUploader(cfg)
	if errUploader != nil {
		fmt.Printf("data-uri-to-tos init error: %v\n", errUploader)
	} else {
		p.uploader = uploader
		p.mutator = NewMutator(cfg, uploader)
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
				Description: "火山引擎 TOS 服务 Endpoint，例如 https://tos-cn-beijing.volces.com",
			},
			{
				Name:        "region",
				Type:        pluginapi.ConfigFieldTypeString,
				Description: "火山引擎 TOS 所在区域，例如 cn-beijing",
			},
			{
				Name:        "bucket",
				Type:        pluginapi.ConfigFieldTypeString,
				Description: "火山引擎 TOS 存储桶 Bucket 名称",
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
				Name:        "input-prefix",
				Type:        pluginapi.ConfigFieldTypeString,
				Description: "TOS 请求垫图/参考图路径前缀，默认 aigc/inputs",
			},
			{
				Name:        "output-prefix",
				Type:        pluginapi.ConfigFieldTypeString,
				Description: "TOS 产物图片/视频路径前缀，默认 aigc/outputs",
			},
			{
				Name:        "object-prefix",
				Type:        pluginapi.ConfigFieldTypeString,
				Description: "TOS 对象 Key 路径前缀（兼容旧版，默认 aigc/inputs）",
			},
			{
				Name:        "max-image-bytes",
				Type:        pluginapi.ConfigFieldTypeInteger,
				Description: "单个媒体文件最大字节大小，默认 20971520 (20MB)",
			},
			{
				Name:        "max-total-bytes",
				Type:        pluginapi.ConfigFieldTypeInteger,
				Description: "单次请求所有媒体文件总字节上限，默认 52428800 (50MB)",
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
		// If TOS credentials not configured, return as-is without rejecting
		return mustEnvelope(aigc.GenerationMutationResponse{Draft: req.Draft})
	}

	resp, errMutate := mutator.MutateContentGeneration(context.Background(), req)
	if errMutate != nil {
		return errorEnvelope("mutate_failed", errMutate.Error())
	}
	return mustEnvelope(resp)
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
