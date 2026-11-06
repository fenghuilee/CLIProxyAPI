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
	"strings"
	"sync"
	"unsafe"

	_ "github.com/router-for-me/CLIProxyAPI/v8/plugins/cpa-plugin-aigc-openai-compat/adapters/chinadatapay"
	_ "github.com/router-for-me/CLIProxyAPI/v8/plugins/cpa-plugin-aigc-openai-compat/adapters/lk888"
	_ "github.com/router-for-me/CLIProxyAPI/v8/plugins/cpa-plugin-aigc-openai-compat/adapters/openaicompat"
	_ "github.com/router-for-me/CLIProxyAPI/v8/plugins/cpa-plugin-aigc-openai-compat/adapters/qwen"
	_ "github.com/router-for-me/CLIProxyAPI/v8/plugins/cpa-plugin-aigc-openai-compat/adapters/volcengine"
	driver "github.com/router-for-me/CLIProxyAPI/v8/plugins/cpa-plugin-aigc-openai-compat/driver"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/aigc"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginabi"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
)

const (
	pluginName       = "aigc-openai-compat"
	pluginAuthor     = "FenghuiLee"
	pluginRepository = "https://github.com/fenghuilee/cpa-plugin-aigc-openai-compat"
)

var (
	driverInstance *driver.Driver
	driverOnce     sync.Once
)

// getDriver 延迟创建驱动，确保并发进入的宿主调用共享同一套路由注册表。
func getDriver() *driver.Driver {
	driverOnce.Do(func() {
		driverInstance = driver.NewDriver()
	})
	return driverInstance
}

func main() {}

// cliproxy_plugin_init 向宿主提供 ABI 版本和调用、释放、关闭回调。
// 导出指令必须保留，供 cgo 生成宿主可访问的 C 符号。
//
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

// cliproxy_plugin_call 将宿主的 C 请求复制到 Go 内存，分发后写回独立的 C 响应缓冲区。
// 返回码表示 ABI 调用状态；业务失败由 JSON 信封中的错误字段表达。
//
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

// cliproxy_plugin_free 释放插件分配的响应内存，宿主消费完成后应调用此回调。
//
//export cliproxy_plugin_free
func cliproxy_plugin_free(ptr unsafe.Pointer, _ C.size_t) {
	if ptr != nil {
		C.free(ptr)
	}
}

// cliproxy_plugin_shutdown 当前无需清理额外资源，保留回调以满足宿主 ABI。
//
//export cliproxy_plugin_shutdown
func cliproxy_plugin_shutdown() {}

// handleMethod 按宿主 RPC 方法分发注册及生成任务的提交、查询、取消操作。
// 适配器仅组装请求或解析响应，上游 HTTP 请求由宿主执行。
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

	case pluginabi.MethodContentGenerationDriverParseCallback:
		var req struct {
			Response aigc.GenerationExecutionResponse `json:"response"`
		}
		_ = json.Unmarshal(request, &req)
		res, err := driver.ParseCallback(ctx, req.Response)
		if err != nil {
			return errorEnvelope("parse_callback_failed", err.Error()), 0
		}
		return mustEnvelope(res), 0

	default:
		return errorEnvelope("unsupported_method", fmt.Sprintf("method not supported: %s", method)), 0
	}
}

// rpcRegistration 描述注册响应的模式版本、插件信息及宿主可发现的能力。
type rpcRegistration struct {
	SchemaVersion uint32             `json:"schema_version"`
	Metadata      pluginapi.Metadata `json:"metadata"`
	Capabilities  map[string]any     `json:"capabilities"`
}

type rpcLifecycleRequest struct {
	ConfigYAML    []byte `json:"config_yaml"`
	SchemaVersion uint32 `json:"schema_version"`
}

func extractCallbackBaseURL(yamlBytes []byte) string {
	lines := strings.Split(string(yamlBytes), "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "callback-base-url:") || strings.HasPrefix(trimmed, "callback_base_url:") {
			parts := strings.SplitN(trimmed, ":", 2)
			if len(parts) == 2 {
				val := strings.TrimSpace(parts[1])
				val = strings.Trim(val, `"'`)
				return val
			}
		}
	}
	return ""
}

// handleRegister 为注册和重新配置调用返回能力声明并同步插件配置。
func handleRegister(request []byte) []byte {
	var req rpcLifecycleRequest
	if len(request) > 0 {
		_ = json.Unmarshal(request, &req)
	}

	cbURL := extractCallbackBaseURL(req.ConfigYAML)
	getDriver().SetCallbackBaseURL(cbURL)

	meta := pluginapi.Metadata{
		Name:             pluginName,
		Version:          driver.PluginVersion,
		Author:           pluginAuthor,
		GitHubRepository: pluginRepository,
		ConfigFields: []pluginapi.ConfigField{
			{Name: "callback-base-url", Type: pluginapi.ConfigFieldTypeString, Description: "公共 Webhook 回调基础地址（配置后向厂商注入回调，未配置则走轮询）"},
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

// writeResponse 复制响应到 C 堆，避免宿主持有可能失效的 Go 内存地址。
// 缓冲区所有权交给宿主，最终通过 cliproxy_plugin_free 释放。
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

// mustEnvelope 将业务结果封装为成功信封；结果序列化失败时改为错误信封。
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

// errorEnvelope 统一输出业务错误码和说明，与 ABI 层返回码区分。
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
