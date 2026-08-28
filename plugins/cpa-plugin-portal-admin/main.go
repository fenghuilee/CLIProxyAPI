package main

/*
#include <stdint.h>
#include <stdlib.h>

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

extern int cliproxyPluginCall(char*, uint8_t*, size_t, cliproxy_buffer*);
extern void cliproxyPluginFree(void*, size_t);
extern void cliproxyPluginShutdown(void);

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
*/
import "C"

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"unsafe"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/aigc"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginabi"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
	"gorm.io/gorm"
)

const (
	pluginName          = "portal-admin"
	pluginAuthor        = "FenghuiLee"
	pluginRepository    = "https://github.com/fenghuilee/cpa-plugin-portal-admin"
	resourcePath        = "/dashboard"
	resourceContentType = "text/html; charset=utf-8"
)

var (
	pluginVersion = "v2026-01-01 (build 00:00:00)"
)

var (
	globalMu       sync.RWMutex
	globalDB       *gorm.DB
	globalHandler  *Handler
	globalAdminSvc *AdminService
	globalConfig   PluginConfig
	lastDBErr      error
)

type rpcLifecycleRequest struct {
	ConfigYAML    []byte `json:"config_yaml"`
	SchemaVersion uint32 `json:"schema_version"`
}

type envelope struct {
	OK     bool            `json:"ok"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *envelopeError  `json:"error,omitempty"`
}

type envelopeError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type registration struct {
	SchemaVersion uint32                   `json:"schema_version"`
	Metadata      pluginapi.Metadata       `json:"metadata"`
	Capabilities  registrationCapabilities `json:"capabilities"`
}

type registrationCapabilities struct {
	ManagementAPI bool `json:"management_api"`
}

type managementRegistration struct {
	Routes    []managementRoute    `json:"routes,omitempty"`
	Resources []managementResource `json:"resources,omitempty"`
}

type managementRoute struct {
	Method      string `json:"Method"`
	Path        string `json:"Path"`
	Description string `json:"Description,omitempty"`
}

type managementResource struct {
	Path        string `json:"Path"`
	Menu        string `json:"Menu"`
	Description string `json:"Description"`
}

type managementRequest struct {
	Method  string      `json:"Method"`
	Path    string      `json:"Path"`
	Headers http.Header `json:"Headers"`
	Query   url.Values  `json:"Query"`
	Body    []byte      `json:"Body"`
}

type managementResponse struct {
	StatusCode int         `json:"StatusCode"`
	Headers    http.Header `json:"Headers"`
	Body       []byte      `json:"Body"`
}

func main() {}

//export cliproxy_plugin_init
func cliproxy_plugin_init(host *C.cliproxy_host_api, plugin *C.cliproxy_plugin_api) C.int {
	if plugin == nil {
		return 1
	}
	C.store_host_api(host)
	plugin.abi_version = C.uint32_t(pluginabi.ABIVersion)
	plugin.call = C.cliproxy_plugin_call_fn(C.cliproxyPluginCall)
	plugin.free_buffer = C.cliproxy_plugin_free_fn(C.cliproxyPluginFree)
	plugin.shutdown = C.cliproxy_plugin_shutdown_fn(C.cliproxyPluginShutdown)

	// Initialize DB with environment or defaults
	initOrReconfigure(nil)
	return 0
}

//export cliproxyPluginCall
func cliproxyPluginCall(method *C.char, request *C.uint8_t, requestLen C.size_t, response *C.cliproxy_buffer) C.int {
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
	raw, errHandle := handleMethod(C.GoString(method), requestBytes)
	if errHandle != nil {
		writeResponse(response, errorEnvelope("plugin_error", errHandle.Error()))
		return 1
	}
	writeResponse(response, raw)
	return 0
}

//export cliproxyPluginFree
func cliproxyPluginFree(ptr unsafe.Pointer, len C.size_t) {
	if ptr != nil {
		C.free(ptr)
	}
	_ = len
}

//export cliproxyPluginShutdown
func cliproxyPluginShutdown() {
	globalMu.Lock()
	defer globalMu.Unlock()
	if globalDB != nil {
		if sqlDB, err := globalDB.DB(); err == nil {
			_ = sqlDB.Close()
		}
		globalDB = nil
	}
	globalHandler = nil
	globalAdminSvc = nil
}

func initOrReconfigure(rawRequest []byte) {
	globalMu.Lock()
	defer globalMu.Unlock()

	var configBytes []byte
	if len(rawRequest) > 0 {
		var req rpcLifecycleRequest
		if err := json.Unmarshal(rawRequest, &req); err == nil && len(req.ConfigYAML) > 0 {
			configBytes = req.ConfigYAML
		} else {
			configBytes = rawRequest
		}
	}

	cfg := parsePluginConfig(configBytes)
	globalConfig = cfg

	if globalDB != nil {
		if sqlDB, err := globalDB.DB(); err == nil {
			_ = sqlDB.Close()
		}
		globalDB = nil
		globalAdminSvc = nil
		globalHandler = nil
	}

	db, errDB := initDB(cfg)
	if errDB != nil {
		lastDBErr = errDB
		fmt.Printf("portal-admin: open mysql error (%s): %v\n", cfg.BuildDSN(), errDB)
	} else {
		lastDBErr = nil
		globalDB = db
		globalAdminSvc = NewAdminService(db)
		globalHandler = NewHandler(globalAdminSvc)

		// Register rule loader and perform initial sync
		aigc.RegisterDynamicRulesLoader(func(ctx context.Context) ([]aigc.DynamicErrorRule, error) {
			var dbRules []ErrorMappingRule
			if err := db.WithContext(ctx).Where("status = ?", 1).Order("priority ASC, id ASC").Find(&dbRules).Error; err != nil {
				return nil, err
			}
			rules := make([]aigc.DynamicErrorRule, 0, len(dbRules))
			for _, r := range dbRules {
				rules = append(rules, aigc.DynamicErrorRule{
					ID:            r.ID,
					Provider:      r.Provider,
					MatchType:     r.MatchType,
					MatchPattern:  r.MatchPattern,
					StandardCode:  r.StandardCode,
					StandardType:  r.StandardType,
					UserMessage:   r.UserMessage,
					UserMessageEN: r.UserMessageEN,
					HTTPStatus:    r.HTTPStatus,
					Priority:      r.Priority,
					Status:        r.Status,
					Description:   r.Description,
				})
			}
			return rules, nil
		})
		_ = globalAdminSvc.SyncErrorRulesToEngine(context.Background())
	}
}

func handleMethod(method string, request []byte) ([]byte, error) {
	switch method {
	case pluginabi.MethodPluginRegister, pluginabi.MethodPluginReconfigure:
		if len(request) > 0 {
			initOrReconfigure(request)
		}
		return okEnvelope(pluginRegistration())

	case pluginabi.MethodManagementRegister:
		return okEnvelope(managementRegistration{
			Resources: []managementResource{
				{
					Path:        resourcePath,
					Menu:        "运营中心",
					Description: "全员用量统计、AIGC 任务监控与充值财务管理",
				},
			},
			Routes: []managementRoute{
				{Method: "GET", Path: "/v0/management/portal/summary", Description: "大盘汇总指标"},
				{Method: "GET", Path: "/v0/management/portal/users", Description: "用户列表"},
				{Method: "GET", Path: "/v0/management/portal/users/detail", Description: "用户详情"},
				{Method: "POST", Path: "/v0/management/portal/users/adjust-quota", Description: "用户额度调账"},
				{Method: "POST", Path: "/v0/management/portal/users/status", Description: "修改用户状态"},
				{Method: "POST", Path: "/v0/management/portal/users/group", Description: "修改用户所属分组"},
				{Method: "GET", Path: "/v0/management/portal/users/keys", Description: "用户APIKey列表"},
				{Method: "GET", Path: "/v0/management/portal/model-groups", Description: "模型分组列表"},
				{Method: "POST", Path: "/v0/management/portal/model-groups", Description: "创建模型分组"},
				{Method: "GET", Path: "/v0/management/portal/recharge-orders", Description: "充值订单列表"},
				{Method: "POST", Path: "/v0/management/portal/recharge-orders/complete", Description: "手动补单入账"},
				{Method: "GET", Path: "/v0/management/portal/packages", Description: "充值套餐列表"},
				{Method: "POST", Path: "/v0/management/portal/packages", Description: "保存充值套餐"},
				{Method: "POST", Path: "/v0/management/portal/packages/delete", Description: "删除充值套餐"},
				{Method: "GET", Path: "/v0/management/portal/ledger", Description: "钱包流水日志"},
				{Method: "GET", Path: "/v0/management/portal/usage/logs", Description: "用量明细日志"},
				{Method: "GET", Path: "/v0/management/portal/usage/stats", Description: "用量趋势统计"},
				{Method: "GET", Path: "/v0/management/portal/aigc/tasks", Description: "AIGC任务列表"},
				{Method: "GET", Path: "/v0/management/portal/aigc/task-detail", Description: "AIGC任务详情"},
				{Method: "POST", Path: "/v0/management/portal/aigc/task-cancel", Description: "取消AIGC任务"},
				{Method: "POST", Path: "/v0/management/portal/aigc/task-refund", Description: "AIGC任务退费"},
				{Method: "GET", Path: "/v0/management/portal/error-rules", Description: "错误映射规则列表"},
				{Method: "GET", Path: "/v0/management/portal/error-rules/detail", Description: "错误映射规则详情"},
				{Method: "GET", Path: "/v0/management/portal/error-rules/get", Description: "错误映射规则详情"},
				{Method: "POST", Path: "/v0/management/portal/error-rules", Description: "新建错误映射规则"},
				{Method: "POST", Path: "/v0/management/portal/error-rules/create", Description: "新建错误映射规则"},
				{Method: "POST", Path: "/v0/management/portal/error-rules/update", Description: "更新错误映射规则"},
				{Method: "POST", Path: "/v0/management/portal/error-rules/delete", Description: "删除错误映射规则"},
				{Method: "POST", Path: "/v0/management/portal/error-rules/test", Description: "测试错误映射规则"},
				{Method: "GET", Path: "/v0/management/portal/error-rules/unclassified", Description: "未分类错误列表"},
				{Method: "GET", Path: "/v0/management/portal/docs", Description: "API文档文章列表"},
				{Method: "GET", Path: "/v0/management/portal/docs/get", Description: "获取单篇API文档"},
				{Method: "GET", Path: "/v0/management/portal/docs/detail", Description: "获取单篇API文档"},
				{Method: "POST", Path: "/v0/management/portal/docs/create", Description: "创建API文档"},
				{Method: "POST", Path: "/v0/management/portal/docs/update", Description: "更新API文档"},
				{Method: "POST", Path: "/v0/management/portal/docs/toggle", Description: "切换API文档发布状态"},
				{Method: "POST", Path: "/v0/management/portal/docs/delete", Description: "删除API文档"},
			},
		})

	case pluginabi.MethodManagementHandle:
		return handleManagement(request)

	default:
		return errorEnvelope("unknown_method", "unknown method: "+method), nil
	}
}

func pluginRegistration() registration {
	return registration{
		SchemaVersion: pluginabi.SchemaVersion,
		Metadata: pluginapi.Metadata{
			Name:             pluginName,
			Version:          pluginVersion,
			Author:           pluginAuthor,
			GitHubRepository: pluginRepository,
			ConfigFields: []pluginapi.ConfigField{
				{
					Name:        "db_host",
					Type:        pluginapi.ConfigFieldTypeString,
					Description: "MySQL 主机地址 (默认 127.0.0.1)",
				},
				{
					Name:        "db_port",
					Type:        pluginapi.ConfigFieldTypeInteger,
					Description: "MySQL 端口 (默认 3306)",
				},
				{
					Name:        "db_user",
					Type:        pluginapi.ConfigFieldTypeString,
					Description: "MySQL 用户名 (默认 root)",
				},
				{
					Name:        "db_password",
					Type:        pluginapi.ConfigFieldTypeString,
					Description: "MySQL 密码",
				},
				{
					Name:        "db_name",
					Type:        pluginapi.ConfigFieldTypeString,
					Description: "MySQL 数据库名 (默认 cliproxyapi)",
				},
				{
					Name:        "dsn",
					Type:        pluginapi.ConfigFieldTypeString,
					Description: "MySQL 自定义 DSN 连接串 (优先级高于单个字段)",
				},
			},
		},
		Capabilities: registrationCapabilities{
			ManagementAPI: true,
		},
	}
}

func handleManagement(raw []byte) ([]byte, error) {
	var req managementRequest
	if len(raw) > 0 {
		if errUnmarshal := json.Unmarshal(raw, &req); errUnmarshal != nil {
			return nil, fmt.Errorf("decode management request: %w", errUnmarshal)
		}
	}

	path := strings.TrimSpace(req.Path)
	method := strings.ToUpper(strings.TrimSpace(req.Method))

	// Always serve UI resource directly even if DB is still establishing connection
	if strings.HasPrefix(path, "/v0/resource/plugins/portal-admin") || strings.HasPrefix(path, "/plugins/portal-admin") || path == "/dashboard" || path == "/status" || path == "/" {
		if method == http.MethodGet {
			return okEnvelope(managementResponse{
				StatusCode: http.StatusOK,
				Headers: http.Header{
					"Content-Type": []string{"text/html; charset=utf-8"},
				},
				Body: uiIndexHTML,
			})
		}
	}

	globalMu.RLock()
	h := globalHandler
	db := globalDB
	globalMu.RUnlock()

	// If DB is not connected yet, try lazy reconnect
	if h == nil || db == nil {
		globalMu.Lock()
		if globalHandler == nil || globalDB == nil {
			if newDB, err := initDB(globalConfig); err == nil {
				lastDBErr = nil
				globalDB = newDB
				globalAdminSvc = NewAdminService(newDB)
				globalHandler = NewHandler(globalAdminSvc)
				h = globalHandler
			} else {
				lastDBErr = err
			}
		}
		globalMu.Unlock()
	}

	if h == nil {
		errMsg := "MySQL database connection not established. Please check plugin db configuration."
		if lastDBErr != nil {
			errMsg = fmt.Sprintf("MySQL connection failed to %s:%d/%s: %v", globalConfig.DBHost, globalConfig.DBPort, globalConfig.DBName, lastDBErr)
		}
		return okEnvelope(managementResponse{
			StatusCode: http.StatusServiceUnavailable,
			Headers: http.Header{
				"Content-Type": []string{"application/json; charset=utf-8"},
			},
			Body: []byte(fmt.Sprintf(`{"error":"db_unavailable","message":%q}`, errMsg)),
		})
	}

	resp, errDispatch := h.Dispatch(context.Background(), req)
	if errDispatch != nil {
		return okEnvelope(managementResponse{
			StatusCode: http.StatusInternalServerError,
			Headers: http.Header{
				"Content-Type": []string{"application/json; charset=utf-8"},
			},
			Body: []byte(fmt.Sprintf(`{"error":"internal_error","message":%q}`, errDispatch.Error())),
		})
	}
	return okEnvelope(resp)
}

func okEnvelope(v any) ([]byte, error) {
	raw, errMarshal := json.Marshal(v)
	if errMarshal != nil {
		return nil, errMarshal
	}
	return json.Marshal(envelope{OK: true, Result: raw})
}

func errorEnvelope(code, message string) []byte {
	raw, _ := json.Marshal(envelope{OK: false, Error: &envelopeError{Code: code, Message: message}})
	return raw
}

func writeResponse(response *C.cliproxy_buffer, raw []byte) {
	if response == nil || len(raw) == 0 {
		return
	}
	ptr := C.CBytes(raw)
	if ptr == nil {
		return
	}
	response.ptr = ptr
	response.len = C.size_t(len(raw))
}
