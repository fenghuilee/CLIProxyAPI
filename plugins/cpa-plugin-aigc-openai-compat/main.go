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
	pluginName       = "aigc-openai-compat"
	pluginAuthor     = "FenghuiLee"
	pluginRepository = "https://github.com/fenghuilee/cpa-plugin-aigc-openai-compat"
)

var (
	pluginVersion = "v2026-01-01 (build 00:00:00)"
)

var (
	driverInstance *Driver
	driverOnce     sync.Once
)

func getDriver() *Driver {
	driverOnce.Do(func() {
		driverInstance = NewDriver()
	})
	return driverInstance
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
	ctx := context.Background()
	driver := getDriver()

	switch method {
	case pluginabi.MethodPluginRegister, pluginabi.MethodPluginReconfigure:
		return handleRegister(request), 0
	case pluginabi.MethodPluginShutdown:
		return mustEnvelope(nil), 0

	case pluginabi.MethodContentGenerationDriverSupports:
		var req aigc.GenerationSupportRequest
		_ = json.Unmarshal(request, &req)
		res, err := driver.Supports(ctx, req)
		if err != nil {
			return errorEnvelope("supports_failed", err.Error()), 0
		}
		return mustEnvelope(res), 0

	case pluginabi.MethodContentGenerationDriverPrepareSubmit:
		var req struct {
			Generation aigc.ContentGeneration `json:"generation"`
		}
		_ = json.Unmarshal(request, &req)
		res, err := driver.PrepareSubmit(ctx, aigc.GenerationSubmitInput{Generation: req.Generation})
		if err != nil {
			return errorEnvelope("prepare_submit_failed", err.Error()), 0
		}
		return mustEnvelope(res), 0

	case pluginabi.MethodContentGenerationDriverParseSubmit:
		var req struct {
			Response aigc.GenerationExecutionResponse `json:"response"`
		}
		_ = json.Unmarshal(request, &req)
		res, err := driver.ParseSubmit(ctx, req.Response)
		if err != nil {
			return errorEnvelope("parse_submit_failed", err.Error()), 0
		}
		return mustEnvelope(res), 0

	case pluginabi.MethodContentGenerationDriverPreparePoll:
		var req struct {
			Generation aigc.ContentGeneration `json:"generation"`
		}
		_ = json.Unmarshal(request, &req)
		res, err := driver.PreparePoll(ctx, aigc.GenerationPollInput{Generation: req.Generation})
		if err != nil {
			return errorEnvelope("prepare_poll_failed", err.Error()), 0
		}
		return mustEnvelope(res), 0

	case pluginabi.MethodContentGenerationDriverParsePoll:
		var req struct {
			Response aigc.GenerationExecutionResponse `json:"response"`
		}
		_ = json.Unmarshal(request, &req)
		res, err := driver.ParsePoll(ctx, req.Response)
		if err != nil {
			return errorEnvelope("parse_poll_failed", err.Error()), 0
		}
		return mustEnvelope(res), 0

	case pluginabi.MethodContentGenerationDriverPrepareCancel:
		var req struct {
			Generation aigc.ContentGeneration `json:"generation"`
		}
		_ = json.Unmarshal(request, &req)
		res, err := driver.PrepareCancel(ctx, aigc.GenerationCancelInput{Generation: req.Generation})
		if err != nil {
			return errorEnvelope("prepare_cancel_failed", err.Error()), 0
		}
		return mustEnvelope(res), 0

	case pluginabi.MethodContentGenerationDriverParseCancel:
		var req struct {
			Response aigc.GenerationExecutionResponse `json:"response"`
		}
		_ = json.Unmarshal(request, &req)
		err := driver.ParseCancel(ctx, req.Response)
		if err != nil {
			return errorEnvelope("parse_cancel_failed", err.Error()), 0
		}
		return mustEnvelope(nil), 0

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

func handleRegister(request []byte) []byte {
	var req rpcLifecycleRequest
	if len(request) > 0 {
		_ = json.Unmarshal(request, &req)
	}

	cfg := DefaultConfig()
	if len(req.ConfigYAML) > 0 {
		_ = yaml.Unmarshal(req.ConfigYAML, &cfg)
	}
	getDriver().SetConfig(cfg)

	meta := pluginapi.Metadata{
		Name:             pluginName,
		Version:          pluginVersion,
		Author:           pluginAuthor,
		GitHubRepository: pluginRepository,
		ConfigFields: []pluginapi.ConfigField{
			{
				Name:        "qwen-image",
				Type:        pluginapi.ConfigFieldTypeObject,
				Description: "阿里 DashScope Qwen-Image 生图配置",
			},
			{
				Name:        "qwen-wan",
				Type:        pluginapi.ConfigFieldTypeObject,
				Description: "阿里 DashScope Wan 视频生成配置",
			},
			{
				Name:        "volcengine-seedance",
				Type:        pluginapi.ConfigFieldTypeObject,
				Description: "火山方舟 Doubao Seedance 视频生成配置",
			},
			{
				Name:        "volcengine-seedream",
				Type:        pluginapi.ConfigFieldTypeObject,
				Description: "火山方舟 Doubao Seedream 生图与图层拆分配置",
			},
			{
				Name:        "openai-compat",
				Type:        pluginapi.ConfigFieldTypeObject,
				Description: "通用 OpenAI-Compatible 生图与视频兼容层配置",
			},
		},
	}

	caps := map[string]any{
		"content_generation_driver": true,
	}

	reg := rpcRegistration{
		SchemaVersion: pluginabi.SchemaVersionAIGC,
		Metadata:      meta,
		Capabilities:  caps,
	}

	return mustEnvelope(reg)
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
