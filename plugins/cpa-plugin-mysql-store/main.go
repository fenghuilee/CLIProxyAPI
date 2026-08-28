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

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginabi"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
	"gopkg.in/yaml.v3"
	"gorm.io/gorm"
)

const (
	pluginName       = "mysql-store"
	pluginAuthor     = "FenghuiLee"
	pluginRepository = "https://github.com/fenghuilee/cpa-plugin-mysql-store"
)

var (
	pluginVersion = "v2026-01-01 (build 00:00:00)"
)

var (
	pluginInstance *Plugin
	pluginOnce     sync.Once
)

// Plugin manages the database connection and handles generic DatabaseProvider RPC calls.
type Plugin struct {
	mu         sync.Mutex
	cfg        Config
	db         *gorm.DB
	dbProvider *DatabaseProviderService
}

func getPlugin() *Plugin {
	pluginOnce.Do(func() {
		pluginInstance = &Plugin{
			cfg: DefaultConfig(),
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
func cliproxy_plugin_shutdown() {
	p := getPlugin()
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.db != nil {
		if sqlDB, err := p.db.DB(); err == nil {
			_ = sqlDB.Close()
		}
		p.db = nil
		p.dbProvider = nil
	}
}

func handleMethod(method string, request []byte) ([]byte, int) {
	ctx := context.Background()
	p := getPlugin()

	switch method {
	case pluginabi.MethodPluginRegister:
		return p.handleRegister(request, false), 0
	case pluginabi.MethodPluginReconfigure:
		return p.handleRegister(request, true), 0
	case pluginabi.MethodPluginShutdown:
		cliproxy_plugin_shutdown()
		return mustEnvelope(nil), 0

	case pluginabi.MethodDatabaseProviderQuery:
		return p.handleDatabaseQuery(ctx, request), 0
	case pluginabi.MethodDatabaseProviderExec:
		return p.handleDatabaseExec(ctx, request), 0

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

func (p *Plugin) handleRegister(request []byte, _ bool) []byte {
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
	if p.db != nil {
		if sqlDB, err := p.db.DB(); err == nil {
			_ = sqlDB.Close()
		}
		p.db = nil
		p.dbProvider = nil
	}

	db, errDB := OpenMySQL(cfg)
	if errDB != nil {
		fmt.Printf("mysql store open error: %v\n", errDB)
	} else {
		p.db = db
		p.dbProvider = NewDatabaseProvider(db)
	}

	meta := pluginapi.Metadata{
		Name:             pluginName,
		Version:          pluginVersion,
		Author:           pluginAuthor,
		GitHubRepository: pluginRepository,
		ConfigFields: []pluginapi.ConfigField{
			{
				Name:        "host",
				Type:        pluginapi.ConfigFieldTypeString,
				Description: "MySQL 主机地址，默认 127.0.0.1",
			},
			{
				Name:        "port",
				Type:        pluginapi.ConfigFieldTypeInteger,
				Description: "MySQL 端口号，默认 3306",
			},
			{
				Name:        "username",
				Type:        pluginapi.ConfigFieldTypeString,
				Description: "MySQL 用户名，默认 root",
			},
			{
				Name:        "password",
				Type:        pluginapi.ConfigFieldTypeString,
				Description: "MySQL 密码",
			},
			{
				Name:        "database",
				Type:        pluginapi.ConfigFieldTypeString,
				Description: "数据库名称，默认 cliproxyapi",
			},
			{
				Name:        "charset",
				Type:        pluginapi.ConfigFieldTypeString,
				Description: "数据库字符集，默认 utf8mb4",
			},
			{
				Name:        "loc",
				Type:        pluginapi.ConfigFieldTypeString,
				Description: "时区设置，默认 Local",
			},
			{
				Name:        "parse-time",
				Type:        pluginapi.ConfigFieldTypeBoolean,
				Description: "是否解析时间类型，默认 true",
			},
			{
				Name:        "max-open-conns",
				Type:        pluginapi.ConfigFieldTypeInteger,
				Description: "连接池最大打开连接数，默认 25",
			},
			{
				Name:        "max-idle-conns",
				Type:        pluginapi.ConfigFieldTypeInteger,
				Description: "连接池最大空闲连接数，默认 10",
			},
			{
				Name:        "conn-max-lifetime",
				Type:        pluginapi.ConfigFieldTypeString,
				Description: "连接最大生命周期，例如 30m",
			},
			{
				Name:        "conn-max-idle-time",
				Type:        pluginapi.ConfigFieldTypeString,
				Description: "连接最大空闲时长，例如 10m",
			},
		},
	}

	caps := map[string]any{
		"database_provider": true,
	}

	reg := rpcRegistration{
		SchemaVersion: pluginabi.SchemaVersionAIGC,
		Metadata:      meta,
		Capabilities:  caps,
	}

	return mustEnvelope(reg)
}

func (p *Plugin) handleDatabaseQuery(ctx context.Context, request []byte) []byte {
	var req pluginapi.DatabaseQueryRequest
	if len(request) > 0 {
		if err := json.Unmarshal(request, &req); err != nil {
			return errorEnvelope("invalid_request", fmt.Sprintf("failed to parse database query request: %v", err))
		}
	}
	p.mu.Lock()
	provider := p.dbProvider
	p.mu.Unlock()
	if provider == nil {
		return errorEnvelope("database_unavailable", "database provider is not initialized")
	}
	resp, err := provider.Query(ctx, req)
	if err != nil {
		return errorEnvelope("query_failed", err.Error())
	}
	return mustEnvelope(resp)
}

func (p *Plugin) handleDatabaseExec(ctx context.Context, request []byte) []byte {
	var req pluginapi.DatabaseExecRequest
	if len(request) > 0 {
		if err := json.Unmarshal(request, &req); err != nil {
			return errorEnvelope("invalid_request", fmt.Sprintf("failed to parse database exec request: %v", err))
		}
	}
	p.mu.Lock()
	provider := p.dbProvider
	p.mu.Unlock()
	if provider == nil {
		return errorEnvelope("database_unavailable", "database provider is not initialized")
	}
	resp, err := provider.Exec(ctx, req)
	if err != nil {
		return errorEnvelope("exec_failed", err.Error())
	}
	return mustEnvelope(resp)
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
	ptr := C.malloc(C.size_t(len(data)))
	if ptr == nil {
		return
	}
	C.memcpy(ptr, unsafe.Pointer(&data[0]), C.size_t(len(data)))
	target.ptr = ptr
	target.len = C.size_t(len(data))
}
