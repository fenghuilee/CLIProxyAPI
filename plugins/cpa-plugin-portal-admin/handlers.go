package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/shopspring/decimal"
)

//go:embed ui/index.html
var uiIndexHTML []byte

type Handler struct {
	adminSvc *AdminService
}

func NewHandler(adminSvc *AdminService) *Handler {
	return &Handler{adminSvc: adminSvc}
}

func (h *Handler) Dispatch(ctx context.Context, req managementRequest) (managementResponse, error) {
	path := strings.TrimSpace(req.Path)
	method := strings.ToUpper(strings.TrimSpace(req.Method))
	query := req.Query
	if query == nil {
		query = make(url.Values)
	}

	// 1. UI Resource endpoint (/v0/resource/plugins/portal-admin/* or legacy /status /dashboard)
	if strings.HasPrefix(path, "/v0/resource/plugins/portal-admin") || strings.HasPrefix(path, "/plugins/portal-admin") || path == "/dashboard" || path == "/status" || path == "/" {
		if method == http.MethodGet {
			return managementResponse{
				StatusCode: http.StatusOK,
				Headers: http.Header{
					"Content-Type": []string{"text/html; charset=utf-8"},
				},
				Body: uiIndexHTML,
			}, nil
		}
	}

	if h.adminSvc == nil {
		return errorResponse(http.StatusServiceUnavailable, "service_unavailable", "admin service not initialized")
	}

	// 2. Summary Endpoint
	if path == "/v0/management/portal/summary" && method == http.MethodGet {
		summary, err := h.adminSvc.GetDashboardSummary(ctx)
		if err != nil {
			return errorResponse(http.StatusInternalServerError, "db_error", err.Error())
		}
		return jsonResponse(http.StatusOK, summary)
	}

	// 3. User Endpoints
	if path == "/v0/management/portal/users" && method == http.MethodGet {
		search := query.Get("search")
		var statusPtr *int8
		if s := query.Get("status"); s != "" {
			if st, err := strconv.Atoi(s); err == nil {
				val := int8(st)
				statusPtr = &val
			}
		}
		offset, limit := parsePagination(query)
		users, total, err := h.adminSvc.ListUsers(ctx, search, statusPtr, offset, limit)
		if err != nil {
			return errorResponse(http.StatusInternalServerError, "db_error", err.Error())
		}
		return jsonResponse(http.StatusOK, map[string]any{
			"total": total,
			"items": users,
		})
	}

	if path == "/v0/management/portal/users/detail" || path == "/v0/management/portal/users/get" {
		userIDStr := query.Get("id")
		if userIDStr == "" {
			userIDStr = query.Get("user_id")
		}
		userID, errID := strconv.ParseUint(userIDStr, 10, 64)
		if errID != nil || userID == 0 {
			return errorResponse(http.StatusBadRequest, "invalid_user_id", "invalid user id")
		}
		user, err := h.adminSvc.GetUser(ctx, userID)
		if err != nil {
			return errorResponse(http.StatusInternalServerError, "db_error", err.Error())
		}
		if user == nil {
			return errorResponse(http.StatusNotFound, "user_not_found", "user not found")
		}
		return jsonResponse(http.StatusOK, user)
	}

	if path == "/v0/management/portal/users/adjust-quota" && method == http.MethodPost {
		var body struct {
			UserID uint64 `json:"user_id"`
			Delta  string `json:"delta"`
			Remark string `json:"remark"`
		}
		if err := json.Unmarshal(req.Body, &body); err != nil {
			return errorResponse(http.StatusBadRequest, "invalid_body", err.Error())
		}
		if body.UserID == 0 {
			if idStr := query.Get("user_id"); idStr != "" {
				if id, err := strconv.ParseUint(idStr, 10, 64); err == nil {
					body.UserID = id
				}
			}
		}
		if body.UserID == 0 {
			return errorResponse(http.StatusBadRequest, "invalid_user_id", "user_id is required")
		}
		delta, errParse := decimal.NewFromString(body.Delta)
		if errParse != nil || delta.IsZero() {
			return errorResponse(http.StatusBadRequest, "invalid_amount", "invalid adjustment amount")
		}
		user, err := h.adminSvc.AdjustUserQuota(ctx, body.UserID, delta, body.Remark)
		if err != nil {
			return errorResponse(http.StatusBadRequest, "adjust_failed", err.Error())
		}
		return jsonResponse(http.StatusOK, map[string]any{
			"status": "ok",
			"user":   user,
		})
	}

	if (path == "/v0/management/portal/users/status" || path == "/v0/management/portal/users/update-status") && (method == http.MethodPost || method == http.MethodPatch) {
		var body struct {
			UserID uint64 `json:"user_id"`
			Status int8   `json:"status"`
		}
		if err := json.Unmarshal(req.Body, &body); err != nil {
			return errorResponse(http.StatusBadRequest, "invalid_body", err.Error())
		}
		if body.UserID == 0 {
			if idStr := query.Get("user_id"); idStr != "" {
				if id, err := strconv.ParseUint(idStr, 10, 64); err == nil {
					body.UserID = id
				}
			}
		}
		if body.UserID == 0 {
			return errorResponse(http.StatusBadRequest, "invalid_user_id", "user_id is required")
		}
		if err := h.adminSvc.UpdateUserStatus(ctx, body.UserID, body.Status); err != nil {
			return errorResponse(http.StatusInternalServerError, "db_error", err.Error())
		}
		return jsonResponse(http.StatusOK, map[string]any{"status": "ok"})
	}

	if (path == "/v0/management/portal/users/group") && (method == http.MethodPost || method == http.MethodPatch || method == http.MethodPut) {
		var body struct {
			UserID    uint64 `json:"user_id"`
			GroupName string `json:"group_name"`
		}
		_ = json.Unmarshal(req.Body, &body)
		if body.UserID == 0 {
			if idStr := query.Get("id"); idStr != "" {
				if id, errID := strconv.ParseUint(idStr, 10, 64); errID == nil {
					body.UserID = id
				}
			}
		}
		if body.GroupName == "" {
			body.GroupName = query.Get("group_name")
		}
		if body.UserID == 0 {
			return errorResponse(http.StatusBadRequest, "invalid_user_id", "user_id is required")
		}
		if err := h.adminSvc.UpdateUserGroup(ctx, body.UserID, body.GroupName); err != nil {
			return errorResponse(http.StatusInternalServerError, "db_error", err.Error())
		}
		return jsonResponse(http.StatusOK, map[string]any{"status": "ok"})
	}

	// 2.1 Model Groups Endpoints
	if path == "/v0/management/portal/model-groups" {
		if method == http.MethodGet {
			groups, err := h.adminSvc.ListModelGroups(ctx)
			if err != nil {
				return errorResponse(http.StatusInternalServerError, "db_error", err.Error())
			}
			items := make([]ModelGroupDTO, len(groups))
			for i, g := range groups {
				items[i] = g.ToDTO()
			}
			return jsonResponse(http.StatusOK, map[string]any{"items": items})
		}
		if method == http.MethodPost {
			var body struct {
				Name        string          `json:"name"`
				DisplayName string          `json:"display_name"`
				Models      json.RawMessage `json:"models"`
			}
			_ = json.Unmarshal(req.Body, &body)
			modelsStr := normalizeModelsInput(body.Models)
			group, err := h.adminSvc.CreateModelGroup(ctx, body.Name, body.DisplayName, modelsStr)
			if err != nil {
				return errorResponse(http.StatusBadRequest, "create_group_failed", err.Error())
			}
			return jsonResponse(http.StatusOK, map[string]any{"status": "ok", "group": group.ToDTO()})
		}
	}

	if strings.HasPrefix(path, "/v0/management/portal/model-groups/") {
		idStr := strings.TrimPrefix(path, "/v0/management/portal/model-groups/")
		id, errID := strconv.ParseUint(idStr, 10, 64)
		if errID != nil || id == 0 {
			return errorResponse(http.StatusBadRequest, "invalid_id", "invalid group id")
		}

		if method == http.MethodPut || method == http.MethodPatch || method == http.MethodPost {
			var body struct {
				DisplayName string          `json:"display_name"`
				Models      json.RawMessage `json:"models"`
				Status      int8            `json:"status"`
			}
			_ = json.Unmarshal(req.Body, &body)
			modelsStr := ""
			if len(body.Models) > 0 {
				modelsStr = normalizeModelsInput(body.Models)
			}
			if err := h.adminSvc.UpdateModelGroup(ctx, id, body.DisplayName, modelsStr, body.Status); err != nil {
				return errorResponse(http.StatusInternalServerError, "db_error", err.Error())
			}
			return jsonResponse(http.StatusOK, map[string]any{"status": "ok"})
		}

		if method == http.MethodDelete {
			if err := h.adminSvc.DeleteModelGroup(ctx, id); err != nil {
				return errorResponse(http.StatusBadRequest, "delete_group_failed", err.Error())
			}
			return jsonResponse(http.StatusOK, map[string]any{"status": "ok"})
		}
	}

	if (path == "/v0/management/portal/users/keys") && method == http.MethodGet {
		userIDStr := query.Get("user_id")
		if userIDStr == "" {
			userIDStr = query.Get("id")
		}
		userID, errID := strconv.ParseUint(userIDStr, 10, 64)
		if errID != nil || userID == 0 {
			return errorResponse(http.StatusBadRequest, "invalid_user_id", "invalid user id")
		}
		keys, err := h.adminSvc.ListUserAPIKeys(ctx, userID)
		if err != nil {
			return errorResponse(http.StatusInternalServerError, "db_error", err.Error())
		}
		return jsonResponse(http.StatusOK, map[string]any{"items": keys})
	}

	// 4. Recharge Orders Endpoints
	if path == "/v0/management/portal/recharge-orders" && method == http.MethodGet {
		search := query.Get("search")
		channel := query.Get("channel")
		status := query.Get("status")
		dateFrom := query.Get("date_from")
		dateTo := query.Get("date_to")
		offset, limit := parsePagination(query)
		orders, total, err := h.adminSvc.ListRechargeOrders(ctx, search, channel, status, dateFrom, dateTo, offset, limit)
		if err != nil {
			return errorResponse(http.StatusInternalServerError, "db_error", err.Error())
		}
		return jsonResponse(http.StatusOK, map[string]any{
			"total": total,
			"items": orders,
		})
	}

	if (path == "/v0/management/portal/recharge-orders/complete") && method == http.MethodPost {
		var body struct {
			OrderNo string `json:"order_no"`
			TradeNo string `json:"trade_no"`
			Remark  string `json:"remark"`
		}
		_ = json.Unmarshal(req.Body, &body)
		if body.OrderNo == "" {
			body.OrderNo = query.Get("order_no")
		}
		if body.OrderNo == "" {
			return errorResponse(http.StatusBadRequest, "invalid_order_no", "order_no is required")
		}
		if err := h.adminSvc.CompleteRechargeOrder(ctx, body.OrderNo, body.TradeNo, body.Remark); err != nil {
			return errorResponse(http.StatusBadRequest, "complete_order_failed", err.Error())
		}
		return jsonResponse(http.StatusOK, map[string]any{"status": "ok"})
	}

	// 5. Recharge Packages Endpoints
	if path == "/v0/management/portal/packages" {
		if method == http.MethodGet {
			packages, err := h.adminSvc.ListRechargePackages(ctx)
			if err != nil {
				return errorResponse(http.StatusInternalServerError, "db_error", err.Error())
			}
			return jsonResponse(http.StatusOK, map[string]any{"items": packages})
		} else if method == http.MethodPost || method == http.MethodPut {
			var pkg RechargePackage
			if err := json.Unmarshal(req.Body, &pkg); err != nil {
				return errorResponse(http.StatusBadRequest, "invalid_body", err.Error())
			}
			if err := h.adminSvc.SaveRechargePackage(ctx, pkg); err != nil {
				return errorResponse(http.StatusInternalServerError, "db_error", err.Error())
			}
			return jsonResponse(http.StatusOK, map[string]any{"status": "ok"})
		} else if method == http.MethodDelete {
			idStr := query.Get("id")
			id, err := strconv.ParseUint(idStr, 10, 64)
			if err != nil || id == 0 {
				var body struct {
					ID uint64 `json:"id"`
				}
				_ = json.Unmarshal(req.Body, &body)
				id = body.ID
			}
			if id == 0 {
				return errorResponse(http.StatusBadRequest, "invalid_id", "package id is required")
			}
			if err := h.adminSvc.DeleteRechargePackage(ctx, id); err != nil {
				return errorResponse(http.StatusInternalServerError, "db_error", err.Error())
			}
			return jsonResponse(http.StatusOK, map[string]any{"status": "ok"})
		}
	}

	if path == "/v0/management/portal/packages/delete" && method == http.MethodPost {
		var body struct {
			ID uint64 `json:"id"`
		}
		_ = json.Unmarshal(req.Body, &body)
		if body.ID == 0 {
			if idStr := query.Get("id"); idStr != "" {
				if id, err := strconv.ParseUint(idStr, 10, 64); err == nil {
					body.ID = id
				}
			}
		}
		if body.ID == 0 {
			return errorResponse(http.StatusBadRequest, "invalid_id", "package id is required")
		}
		if err := h.adminSvc.DeleteRechargePackage(ctx, body.ID); err != nil {
			return errorResponse(http.StatusInternalServerError, "db_error", err.Error())
		}
		return jsonResponse(http.StatusOK, map[string]any{"status": "ok"})
	}

	// 6. Wallet Ledger Endpoint
	if path == "/v0/management/portal/ledger" && method == http.MethodGet {
		search := query.Get("search")
		opType := query.Get("type")
		refID := query.Get("ref_id")
		dateFrom := query.Get("date_from")
		dateTo := query.Get("date_to")
		offset, limit := parsePagination(query)
		ledgers, total, err := h.adminSvc.ListWalletLedgers(ctx, search, opType, refID, dateFrom, dateTo, offset, limit)
		if err != nil {
			return errorResponse(http.StatusInternalServerError, "db_error", err.Error())
		}
		return jsonResponse(http.StatusOK, map[string]any{
			"total": total,
			"items": ledgers,
		})
	}

	// 7. Usage Logs & Stats Endpoints
	if path == "/v0/management/portal/usage/logs" && method == http.MethodGet {
		offset, limit := parsePagination(query)
		filter := UsageLogFilter{
			Search:   query.Get("search"),
			Model:    query.Get("model"),
			Provider: query.Get("provider"),
			Outcome:  query.Get("outcome"),
			DateFrom: query.Get("date_from"),
			DateTo:   query.Get("date_to"),
			Offset:   offset,
			Limit:    limit,
		}
		logs, total, err := h.adminSvc.ListUsageLogs(ctx, filter)
		if err != nil {
			return errorResponse(http.StatusInternalServerError, "db_error", err.Error())
		}
		items := make([]UsageStatisticDTO, len(logs))
		for i, l := range logs {
			items[i] = l.ToDTO()
		}
		return jsonResponse(http.StatusOK, map[string]any{
			"total": total,
			"items": items,
		})
	}

	if path == "/v0/management/portal/usage/stats" && method == http.MethodGet {
		days := 30
		if d := query.Get("days"); d != "" {
			if parsed, err := strconv.Atoi(d); err == nil && parsed > 0 {
				days = parsed
			}
		}
		stats, err := h.adminSvc.GetUsageStats(ctx, days)
		if err != nil {
			return errorResponse(http.StatusInternalServerError, "db_error", err.Error())
		}
		return jsonResponse(http.StatusOK, map[string]any{"items": stats})
	}

	// 8. AIGC Task Endpoints
	if path == "/v0/management/portal/aigc/tasks" && method == http.MethodGet {
		offset, limit := parsePagination(query)
		filter := AIGCTaskFilter{
			Search:   query.Get("search"),
			Kind:     query.Get("kind"),
			Status:   query.Get("status"),
			Model:    query.Get("model"),
			DateFrom: query.Get("date_from"),
			DateTo:   query.Get("date_to"),
			Offset:   offset,
			Limit:    limit,
		}
		tasks, total, err := h.adminSvc.ListAIGCTasks(ctx, filter)
		if err != nil {
			return errorResponse(http.StatusInternalServerError, "db_error", err.Error())
		}
		items := make([]ContentGenerationDTO, len(tasks))
		for i, t := range tasks {
			items[i] = t.ToDTO()
		}
		return jsonResponse(http.StatusOK, map[string]any{
			"total": total,
			"items": items,
		})
	}

	if (path == "/v0/management/portal/aigc/task-detail" || path == "/v0/management/portal/aigc/tasks/detail") && method == http.MethodGet {
		genID := query.Get("id")
		if genID == "" {
			genID = query.Get("generation_id")
		}
		if genID == "" {
			return errorResponse(http.StatusBadRequest, "invalid_id", "generation_id is required")
		}
		task, err := h.adminSvc.GetAIGCTaskDetail(ctx, genID)
		if err != nil {
			return errorResponse(http.StatusInternalServerError, "db_error", err.Error())
		}
		if task == nil {
			return errorResponse(http.StatusNotFound, "task_not_found", "task not found")
		}
		return jsonResponse(http.StatusOK, task.ToDTO())
	}

	if (path == "/v0/management/portal/aigc/task-cancel" || path == "/v0/management/portal/aigc/tasks/cancel") && method == http.MethodPost {
		var body struct {
			GenerationID string `json:"generation_id"`
			ID           string `json:"id"`
		}
		_ = json.Unmarshal(req.Body, &body)
		genID := body.GenerationID
		if genID == "" {
			genID = body.ID
		}
		if genID == "" {
			genID = query.Get("id")
		}
		if genID == "" {
			return errorResponse(http.StatusBadRequest, "invalid_id", "generation_id is required")
		}
		if err := h.adminSvc.CancelAIGCTask(ctx, genID); err != nil {
			return errorResponse(http.StatusInternalServerError, "cancel_failed", err.Error())
		}
		return jsonResponse(http.StatusOK, map[string]any{"status": "ok"})
	}

	if (path == "/v0/management/portal/aigc/task-refund" || path == "/v0/management/portal/aigc/tasks/refund") && method == http.MethodPost {
		var body struct {
			GenerationID string `json:"generation_id"`
			ID           string `json:"id"`
			Amount       string `json:"amount"`
			Remark       string `json:"remark"`
		}
		if err := json.Unmarshal(req.Body, &body); err != nil {
			return errorResponse(http.StatusBadRequest, "invalid_body", err.Error())
		}
		genID := body.GenerationID
		if genID == "" {
			genID = body.ID
		}
		if genID == "" {
			genID = query.Get("id")
		}
		if genID == "" {
			return errorResponse(http.StatusBadRequest, "invalid_id", "generation_id is required")
		}
		amt, errParse := decimal.NewFromString(body.Amount)
		if errParse != nil || amt.LessThanOrEqual(decimal.Zero) {
			return errorResponse(http.StatusBadRequest, "invalid_amount", "invalid refund amount")
		}
		if err := h.adminSvc.RefundAIGCTask(ctx, genID, amt, body.Remark); err != nil {
			return errorResponse(http.StatusBadRequest, "refund_failed", err.Error())
		}
		return jsonResponse(http.StatusOK, map[string]any{"status": "ok"})
	}

	// 7. Error Mapping Rules Endpoints
	if path == "/v0/management/portal/error-rules" && method == http.MethodGet {
		provider := query.Get("provider")
		var statusPtr *int8
		if s := query.Get("status"); s != "" && s != "all" {
			if st, err := strconv.Atoi(s); err == nil {
				val := int8(st)
				statusPtr = &val
			}
		}
		search := query.Get("search")
		offset, limit := parsePagination(query)
		rules, total, err := h.adminSvc.ListErrorRules(ctx, provider, statusPtr, search, offset, limit)
		if err != nil {
			return errorResponse(http.StatusInternalServerError, "db_error", err.Error())
		}
		return jsonResponse(http.StatusOK, map[string]any{
			"total": total,
			"items": rules,
		})
	}

	if (path == "/v0/management/portal/error-rules/detail" || path == "/v0/management/portal/error-rules/get") && method == http.MethodGet {
		idStr := query.Get("id")
		id, errParse := strconv.ParseUint(idStr, 10, 64)
		if errParse != nil || id == 0 {
			return errorResponse(http.StatusBadRequest, "invalid_id", "rule id is required")
		}
		rule, err := h.adminSvc.GetErrorRule(ctx, id)
		if err != nil {
			return errorResponse(http.StatusInternalServerError, "db_error", err.Error())
		}
		if rule == nil {
			return errorResponse(http.StatusNotFound, "not_found", "rule not found")
		}
		return jsonResponse(http.StatusOK, rule)
	}

	if path == "/v0/management/portal/error-rules/create" || (path == "/v0/management/portal/error-rules" && method == http.MethodPost) {
		var rule ErrorMappingRule
		if err := json.Unmarshal(req.Body, &rule); err != nil {
			return errorResponse(http.StatusBadRequest, "invalid_body", err.Error())
		}
		if err := h.adminSvc.CreateErrorRule(ctx, &rule); err != nil {
			return errorResponse(http.StatusBadRequest, "create_failed", err.Error())
		}
		return jsonResponse(http.StatusOK, map[string]any{"status": "ok", "id": rule.ID})
	}

	if path == "/v0/management/portal/error-rules/update" && method == http.MethodPost {
		var rule ErrorMappingRule
		if err := json.Unmarshal(req.Body, &rule); err != nil {
			return errorResponse(http.StatusBadRequest, "invalid_body", err.Error())
		}
		if err := h.adminSvc.UpdateErrorRule(ctx, &rule); err != nil {
			return errorResponse(http.StatusBadRequest, "update_failed", err.Error())
		}
		return jsonResponse(http.StatusOK, map[string]any{"status": "ok"})
	}

	if path == "/v0/management/portal/error-rules/delete" && method == http.MethodPost {
		var body struct {
			ID uint64 `json:"id"`
		}
		_ = json.Unmarshal(req.Body, &body)
		if body.ID == 0 {
			if idStr := query.Get("id"); idStr != "" {
				body.ID, _ = strconv.ParseUint(idStr, 10, 64)
			}
		}
		if body.ID == 0 {
			return errorResponse(http.StatusBadRequest, "invalid_id", "rule id is required")
		}
		if err := h.adminSvc.DeleteErrorRule(ctx, body.ID); err != nil {
			return errorResponse(http.StatusInternalServerError, "delete_failed", err.Error())
		}
		return jsonResponse(http.StatusOK, map[string]any{"status": "ok"})
	}

	if path == "/v0/management/portal/error-rules/test" && method == http.MethodPost {
		var body struct {
			Provider   string `json:"provider"`
			StatusCode int    `json:"status_code"`
			RawBody    string `json:"raw_body"`
		}
		if err := json.Unmarshal(req.Body, &body); err != nil {
			return errorResponse(http.StatusBadRequest, "invalid_body", err.Error())
		}
		result := h.adminSvc.TestErrorRule(ctx, body.Provider, body.StatusCode, body.RawBody)
		return jsonResponse(http.StatusOK, result)
	}

	if path == "/v0/management/portal/error-rules/unclassified" && method == http.MethodGet {
		limit := 30
		if l := query.Get("limit"); l != "" {
			if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 {
				limit = parsed
			}
		}
		unclassified, err := h.adminSvc.ListUnclassifiedErrors(ctx, limit)
		if err != nil {
			return errorResponse(http.StatusInternalServerError, "db_error", err.Error())
		}
		return jsonResponse(http.StatusOK, map[string]any{"items": unclassified})
	}

	// 10. API Docs Management Endpoints
	if path == "/v0/management/portal/docs" && method == http.MethodGet {
		category := query.Get("category")
		search := query.Get("search")
		var statusPtr *int8
		if s := query.Get("status"); s != "" && s != "all" {
			if st, err := strconv.Atoi(s); err == nil {
				val := int8(st)
				statusPtr = &val
			}
		}
		list, err := h.adminSvc.ListDocArticles(ctx, category, statusPtr, search)
		if err != nil {
			return errorResponse(http.StatusInternalServerError, "db_error", err.Error())
		}
		return jsonResponse(http.StatusOK, map[string]any{
			"total": len(list),
			"items": list,
		})
	}

	if (path == "/v0/management/portal/docs/get" || path == "/v0/management/portal/docs/detail") && method == http.MethodGet {
		idStr := query.Get("id")
		id, _ := strconv.ParseUint(idStr, 10, 64)
		if id == 0 {
			slug := query.Get("slug")
			if slug != "" {
				doc, err := h.adminSvc.GetDocArticleBySlug(ctx, slug)
				if err != nil {
					return errorResponse(http.StatusInternalServerError, "db_error", err.Error())
				}
				if doc == nil {
					return errorResponse(http.StatusNotFound, "doc_not_found", "document not found")
				}
				return jsonResponse(http.StatusOK, doc)
			}
			return errorResponse(http.StatusBadRequest, "invalid_id", "id or slug required")
		}
		doc, err := h.adminSvc.GetDocArticle(ctx, id)
		if err != nil {
			return errorResponse(http.StatusInternalServerError, "db_error", err.Error())
		}
		if doc == nil {
			return errorResponse(http.StatusNotFound, "doc_not_found", "document not found")
		}
		return jsonResponse(http.StatusOK, doc)
	}

	if path == "/v0/management/portal/docs/create" && method == http.MethodPost {
		var doc DocArticle
		if err := json.Unmarshal(req.Body, &doc); err != nil {
			return errorResponse(http.StatusBadRequest, "invalid_json", err.Error())
		}
		if err := h.adminSvc.CreateDocArticle(ctx, &doc); err != nil {
			return errorResponse(http.StatusBadRequest, "create_error", err.Error())
		}
		return jsonResponse(http.StatusOK, map[string]any{
			"success": true,
			"id":      doc.ID,
		})
	}

	if path == "/v0/management/portal/docs/update" && method == http.MethodPost {
		var doc DocArticle
		if err := json.Unmarshal(req.Body, &doc); err != nil {
			return errorResponse(http.StatusBadRequest, "invalid_json", err.Error())
		}
		if err := h.adminSvc.UpdateDocArticle(ctx, &doc); err != nil {
			return errorResponse(http.StatusBadRequest, "update_error", err.Error())
		}
		return jsonResponse(http.StatusOK, map[string]any{"success": true})
	}

	if path == "/v0/management/portal/docs/toggle" && method == http.MethodPost {
		var body struct {
			ID uint64 `json:"id"`
		}
		_ = json.Unmarshal(req.Body, &body)
		if body.ID == 0 {
			if idStr := query.Get("id"); idStr != "" {
				body.ID, _ = strconv.ParseUint(idStr, 10, 64)
			}
		}
		if body.ID == 0 {
			return errorResponse(http.StatusBadRequest, "invalid_id", "id is required")
		}
		if err := h.adminSvc.ToggleDocArticleStatus(ctx, body.ID); err != nil {
			return errorResponse(http.StatusBadRequest, "toggle_error", err.Error())
		}
		return jsonResponse(http.StatusOK, map[string]any{"success": true})
	}

	if path == "/v0/management/portal/docs/delete" && method == http.MethodPost {
		var body struct {
			ID uint64 `json:"id"`
		}
		_ = json.Unmarshal(req.Body, &body)
		if body.ID == 0 {
			if idStr := query.Get("id"); idStr != "" {
				body.ID, _ = strconv.ParseUint(idStr, 10, 64)
			}
		}
		if body.ID == 0 {
			return errorResponse(http.StatusBadRequest, "invalid_id", "id is required")
		}
		if err := h.adminSvc.DeleteDocArticle(ctx, body.ID); err != nil {
			return errorResponse(http.StatusBadRequest, "delete_error", err.Error())
		}
		return jsonResponse(http.StatusOK, map[string]any{"success": true})
	}

	return errorResponse(http.StatusNotFound, "not_found", "endpoint not found: "+method+" "+path)
}

func parsePagination(query url.Values) (int, int) {
	page := 1
	limit := 20
	if p := query.Get("page"); p != "" {
		if parsed, err := strconv.Atoi(p); err == nil && parsed > 0 {
			page = parsed
		}
	}
	if l := query.Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 {
			limit = parsed
		}
	}
	offset := (page - 1) * limit
	return offset, limit
}

func jsonResponse(statusCode int, data any) (managementResponse, error) {
	raw, err := json.Marshal(data)
	if err != nil {
		return managementResponse{}, err
	}
	return managementResponse{
		StatusCode: statusCode,
		Headers: http.Header{
			"Content-Type": []string{"application/json; charset=utf-8"},
		},
		Body: raw,
	}, nil
}

func errorResponse(statusCode int, code, message string) (managementResponse, error) {
	return jsonResponse(statusCode, map[string]any{
		"error":   code,
		"message": message,
	})
}

func normalizeModelsInput(raw json.RawMessage) string {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return `["*"]`
	}
	var arr []string
	if err := json.Unmarshal(raw, &arr); err == nil {
		var cleaned []string
		for _, item := range arr {
			if s := strings.TrimSpace(item); s != "" {
				cleaned = append(cleaned, s)
			}
		}
		if len(cleaned) == 0 {
			return `["*"]`
		}
		b, _ := json.Marshal(cleaned)
		return string(b)
	}
	var str string
	if err := json.Unmarshal(raw, &str); err == nil {
		parsed := ParseModelsJSON(str)
		b, _ := json.Marshal(parsed)
		return string(b)
	}
	return `["*"]`
}
