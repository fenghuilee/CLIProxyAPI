package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/aigc"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type AdminService struct {
	db *gorm.DB
}

func NewAdminService(db *gorm.DB) *AdminService {
	return &AdminService{db: db}
}

// -----------------------------------------------------------------------------
// 1. Dashboard Overview
// -----------------------------------------------------------------------------

type DashboardSummary struct {
	TotalUsers       int64             `json:"total_users"`
	ActiveUsers      int64             `json:"active_users"`
	TotalRechargeCNY decimal.Decimal   `json:"total_recharge_cny"`
	TotalUsedQuota   decimal.Decimal   `json:"total_used_quota"`
	TotalRequests    int64             `json:"total_requests"`
	TodayRequests    int64             `json:"today_requests"`
	TotalTokens      int64             `json:"total_tokens"`
	TodayTokens      int64             `json:"today_tokens"`
	TotalAIGCTasks   int64             `json:"total_aigc_tasks"`
	RunningAIGCTasks int64             `json:"running_aigc_tasks"`
	TopUsers         []TopUserSummary  `json:"top_users"`
	TopModels        []TopModelSummary `json:"top_models"`
	DailyTrend       []DailyTrendPoint `json:"daily_trend"`
}

type TopUserSummary struct {
	UserID    uint64          `json:"user_id"`
	Username  string          `json:"username"`
	UsedQuota decimal.Decimal `json:"used_quota"`
	Quota     decimal.Decimal `json:"quota"`
}

type TopModelSummary struct {
	Model        string          `json:"model"`
	RequestCount int64           `json:"request_count"`
	TotalTokens  int64           `json:"total_tokens"`
	TotalCost    decimal.Decimal `json:"total_cost"`
}

type DailyTrendPoint struct {
	Date         string `json:"date"`
	RequestCount int64  `json:"request_count"`
	Tokens       int64  `json:"tokens"`
}

func (s *AdminService) GetDashboardSummary(ctx context.Context) (DashboardSummary, error) {
	var summary DashboardSummary
	todayStart := time.Now().Truncate(24 * time.Hour)

	// Total and active users
	_ = s.db.WithContext(ctx).Model(&User{}).Count(&summary.TotalUsers).Error
	_ = s.db.WithContext(ctx).Model(&User{}).Where("status = 1").Count(&summary.ActiveUsers).Error

	// Total recharge amount in CNY
	var rechargeRow struct {
		Sum decimal.Decimal
	}
	_ = s.db.WithContext(ctx).Model(&RechargeOrder{}).
		Select("COALESCE(SUM(amount_cny), 0) as sum").
		Where("status = 'paid'").
		Scan(&rechargeRow).Error
	summary.TotalRechargeCNY = rechargeRow.Sum

	// Total used quota
	var usedRow struct {
		Sum decimal.Decimal
	}
	_ = s.db.WithContext(ctx).Model(&User{}).
		Select("COALESCE(SUM(used_quota), 0) as sum").
		Scan(&usedRow).Error
	summary.TotalUsedQuota = usedRow.Sum

	// Total and today requests & tokens
	_ = s.db.WithContext(ctx).Model(&UsageStatistic{}).Count(&summary.TotalRequests).Error
	_ = s.db.WithContext(ctx).Model(&UsageStatistic{}).Where("created_at >= ?", todayStart).Count(&summary.TodayRequests).Error

	var totalTokenRow struct {
		Sum int64
	}
	_ = s.db.WithContext(ctx).Model(&UsageStatistic{}).Select("COALESCE(SUM(total_tokens), 0) as sum").Scan(&totalTokenRow).Error
	summary.TotalTokens = totalTokenRow.Sum

	var todayTokenRow struct {
		Sum int64
	}
	_ = s.db.WithContext(ctx).Model(&UsageStatistic{}).Where("created_at >= ?", todayStart).Select("COALESCE(SUM(total_tokens), 0) as sum").Scan(&todayTokenRow).Error
	summary.TodayTokens = todayTokenRow.Sum

	// AIGC tasks
	_ = s.db.WithContext(ctx).Model(&ContentGeneration{}).Count(&summary.TotalAIGCTasks).Error
	_ = s.db.WithContext(ctx).Model(&ContentGeneration{}).Where("status = 'running' OR status = 'pending'").Count(&summary.RunningAIGCTasks).Error

	// Top users
	var topUsers []User
	_ = s.db.WithContext(ctx).Model(&User{}).Order("used_quota DESC").Limit(5).Find(&topUsers).Error
	for _, u := range topUsers {
		summary.TopUsers = append(summary.TopUsers, TopUserSummary{
			UserID:    u.ID,
			Username:  u.Username,
			UsedQuota: u.UsedQuota,
			Quota:     u.Quota,
		})
	}

	// Top models
	type modelAgg struct {
		Model    string
		ReqCount int64
		Tokens   int64
		Cost     decimal.Decimal
	}
	var modelAggs []modelAgg
	_ = s.db.WithContext(ctx).Table("usage_statistics_daily").
		Select("model, COALESCE(SUM(request_count), 0) as req_count, COALESCE(SUM(total_tokens), 0) as tokens, COALESCE(SUM(total_cost), 0) as cost").
		Group("model").
		Order("req_count DESC").
		Limit(5).
		Scan(&modelAggs).Error
	for _, m := range modelAggs {
		summary.TopModels = append(summary.TopModels, TopModelSummary{
			Model:        m.Model,
			RequestCount: m.ReqCount,
			TotalTokens:  m.Tokens,
			TotalCost:    m.Cost,
		})
	}

	// 14-day trend
	type trendRow struct {
		StatDate string
		Reqs     int64
		Tokens   int64
	}
	var trendRows []trendRow
	sinceDate := time.Now().Add(-14 * 24 * time.Hour).Format("2006-01-02")
	_ = s.db.WithContext(ctx).Table("usage_statistics_daily").
		Select("stat_date, COALESCE(SUM(request_count), 0) as reqs, COALESCE(SUM(total_tokens), 0) as tokens").
		Where("stat_date >= ?", sinceDate).
		Group("stat_date").
		Order("stat_date ASC").
		Scan(&trendRows).Error
	for _, tr := range trendRows {
		summary.DailyTrend = append(summary.DailyTrend, DailyTrendPoint{
			Date:         tr.StatDate,
			RequestCount: tr.Reqs,
			Tokens:       tr.Tokens,
		})
	}

	return summary, nil
}

// -----------------------------------------------------------------------------
// 2. User Management
// -----------------------------------------------------------------------------

func (s *AdminService) ListUsers(ctx context.Context, search string, status *int8, offset, limit int) ([]User, int64, error) {
	var users []User
	var total int64

	tx := s.db.WithContext(ctx).Model(&User{})
	if search = strings.TrimSpace(search); search != "" {
		tx = tx.Where("username LIKE ?", "%"+search+"%")
	}
	if status != nil {
		tx = tx.Where("status = ?", *status)
	}

	if err := tx.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	if limit <= 0 {
		limit = 20
	}
	err := tx.Order("id DESC").Offset(offset).Limit(limit).Find(&users).Error
	return users, total, err
}

func (s *AdminService) GetUser(ctx context.Context, id uint64) (*User, error) {
	var user User
	err := s.db.WithContext(ctx).Preload("APIKeys").First(&user, id).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &user, nil
}

func (s *AdminService) UpdateUserStatus(ctx context.Context, id uint64, status int8) error {
	return s.db.WithContext(ctx).Model(&User{}).Where("id = ?", id).Update("status", status).Error
}

func (s *AdminService) UpdateUserGroup(ctx context.Context, id uint64, groupName string) error {
	groupName = strings.TrimSpace(groupName)
	if groupName == "" {
		groupName = "default"
	}
	return s.db.WithContext(ctx).Model(&User{}).Where("id = ?", id).Update("group_name", groupName).Error
}

// -----------------------------------------------------------------------------
// 2.1 Model Groups Management
// -----------------------------------------------------------------------------

func (s *AdminService) ListModelGroups(ctx context.Context) ([]ModelGroup, error) {
	var groups []ModelGroup
	err := s.db.WithContext(ctx).Order("id ASC").Find(&groups).Error
	return groups, err
}

func (s *AdminService) CreateModelGroup(ctx context.Context, name, displayName, models string) (*ModelGroup, error) {
	name = strings.TrimSpace(name)
	displayName = strings.TrimSpace(displayName)
	models = strings.TrimSpace(models)
	if name == "" {
		return nil, errors.New("group name is required")
	}
	if displayName == "" {
		displayName = name
	}
	if models == "" {
		models = `["*"]`
	} else {
		parsed := ParseModelsJSON(models)
		b, _ := json.Marshal(parsed)
		models = string(b)
	}
	group := &ModelGroup{
		Name:        name,
		DisplayName: displayName,
		Models:      models,
		Status:      1,
	}
	if err := s.db.WithContext(ctx).Create(group).Error; err != nil {
		return nil, err
	}
	return group, nil
}

func (s *AdminService) UpdateModelGroup(ctx context.Context, id uint64, displayName, models string, status int8) error {
	updates := map[string]any{
		"status": status,
	}
	if displayName = strings.TrimSpace(displayName); displayName != "" {
		updates["display_name"] = displayName
	}
	if models = strings.TrimSpace(models); models != "" {
		parsed := ParseModelsJSON(models)
		b, _ := json.Marshal(parsed)
		updates["models"] = string(b)
	}
	return s.db.WithContext(ctx).Model(&ModelGroup{}).Where("id = ?", id).Updates(updates).Error
}

func (s *AdminService) DeleteModelGroup(ctx context.Context, id uint64) error {
	var group ModelGroup
	if err := s.db.WithContext(ctx).First(&group, id).Error; err != nil {
		return err
	}
	if strings.EqualFold(group.Name, "default") {
		return errors.New("cannot delete default model group")
	}
	return s.db.WithContext(ctx).Delete(&group).Error
}

func (s *AdminService) AdjustUserQuota(ctx context.Context, id uint64, delta decimal.Decimal, remark string) (*User, error) {
	if delta.IsZero() {
		return nil, errors.New("调整金额不能为0")
	}

	var updatedUser User
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var user User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&user, id).Error; err != nil {
			return fmt.Errorf("lock user: %w", err)
		}

		newBalance := user.Quota.Add(delta)
		if newBalance.IsNegative() {
			return fmt.Errorf("调整后可用余额不足 (当前: %s, 扣减: %s)", user.Quota.StringFixed(8), delta.Abs().StringFixed(8))
		}

		user.Quota = newBalance
		if err := tx.Model(&User{}).Where("id = ?", id).Update("quota", newBalance).Error; err != nil {
			return fmt.Errorf("update user quota: %w", err)
		}

		now := time.Now()
		opType := LedgerTypeManualAdjust
		if delta.IsPositive() && strings.Contains(remark, "充值") {
			opType = LedgerTypeRecharge
		}

		ledger := WalletLedger{
			UserID:       user.ID,
			Type:         opType,
			DeltaQuota:   delta,
			DeltaFrozen:  decimal.Zero,
			BalanceAfter: newBalance,
			RefID:        fmt.Sprintf("ADJ_%d_%d", user.ID, now.Unix()),
			Remark:       RemarkAdminAdjust(remark, delta),
			CreatedAt:    now,
		}
		if err := tx.Create(&ledger).Error; err != nil {
			return fmt.Errorf("create wallet ledger: %w", err)
		}

		updatedUser = user
		return nil
	})

	if err != nil {
		return nil, err
	}
	return &updatedUser, nil
}

func (s *AdminService) ListUserAPIKeys(ctx context.Context, userID uint64) ([]APIKey, error) {
	var keys []APIKey
	err := s.db.WithContext(ctx).Where("user_id = ? AND deleted_at IS NULL", userID).Order("id DESC").Find(&keys).Error
	return keys, err
}

// -----------------------------------------------------------------------------
// 3. Recharge & Package & Ledger Management
// -----------------------------------------------------------------------------

func (s *AdminService) ListRechargeOrders(ctx context.Context, search, channel, status, dateFrom, dateTo string, offset, limit int) ([]RechargeOrder, int64, error) {
	var orders []RechargeOrder
	var total int64

	tx := s.db.WithContext(ctx).Table("recharge_orders o").
		Joins("LEFT JOIN users u ON o.user_id = u.id")

	if search = strings.TrimSpace(search); search != "" {
		tx = tx.Where("o.order_no LIKE ? OR o.trade_no LIKE ? OR u.username LIKE ?", "%"+search+"%", "%"+search+"%", "%"+search+"%")
	}
	if channel = strings.TrimSpace(channel); channel != "" {
		tx = tx.Where("o.channel = ?", channel)
	}
	if status = strings.TrimSpace(status); status != "" {
		tx = tx.Where("o.status = ?", status)
	}
	if dateFrom = strings.TrimSpace(dateFrom); dateFrom != "" {
		if len(dateFrom) == 10 {
			dateFrom += " 00:00:00"
		}
		tx = tx.Where("o.created_at >= ?", dateFrom)
	}
	if dateTo = strings.TrimSpace(dateTo); dateTo != "" {
		if len(dateTo) == 10 {
			dateTo += " 23:59:59"
		}
		tx = tx.Where("o.created_at <= ?", dateTo)
	}

	if err := tx.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	if limit <= 0 {
		limit = 20
	}

	err := tx.Select("o.*, COALESCE(u.username, '') as username").
		Order("o.id DESC").
		Offset(offset).
		Limit(limit).
		Scan(&orders).Error
	if err != nil {
		return nil, 0, err
	}

	if len(orders) > 0 {
		userIDs := make([]uint64, 0, len(orders))
		for _, o := range orders {
			if o.Username == "" && o.UserID > 0 {
				userIDs = append(userIDs, o.UserID)
			}
		}
		if len(userIDs) > 0 {
			var users []User
			_ = s.db.WithContext(ctx).Where("id IN ?", userIDs).Find(&users).Error
			uMap := make(map[uint64]string, len(users))
			for _, u := range users {
				uMap[u.ID] = u.Username
			}
			for i := range orders {
				if orders[i].Username == "" {
					if name, ok := uMap[orders[i].UserID]; ok && name != "" {
						orders[i].Username = name
					}
				}
			}
		}
	}

	return orders, total, nil
}

func (s *AdminService) CompleteRechargeOrder(ctx context.Context, orderNo, tradeNo, remark string) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var order RechargeOrder
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("order_no = ?", orderNo).
			First(&order).Error; err != nil {
			return fmt.Errorf("lock order: %w", err)
		}

		if order.Status == "paid" {
			return nil
		}

		var user User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ?", order.UserID).
			First(&user).Error; err != nil {
			return fmt.Errorf("lock user: %w", err)
		}

		now := time.Now()
		order.Status = "paid"
		if tradeNo != "" {
			order.TradeNo = tradeNo
		} else if order.TradeNo == "" {
			order.TradeNo = fmt.Sprintf("MANUAL_%d", now.Unix())
		}
		order.PaidAt = &now
		if err := tx.Save(&order).Error; err != nil {
			return fmt.Errorf("save order: %w", err)
		}

		newBalance := user.Quota.Add(order.Points)
		if err := tx.Model(&User{}).Where("id = ?", user.ID).Update("quota", newBalance).Error; err != nil {
			return fmt.Errorf("credit user quota: %w", err)
		}

		ledger := WalletLedger{
			UserID:       user.ID,
			Type:         LedgerTypeRecharge,
			DeltaQuota:   order.Points,
			DeltaFrozen:  decimal.Zero,
			BalanceAfter: newBalance,
			RefID:        order.OrderNo,
			Remark:       RemarkManualRecharge(order.Channel, remark, order.AmountCNY, order.Points),
			CreatedAt:    now,
		}
		if err := tx.Create(&ledger).Error; err != nil {
			return fmt.Errorf("create wallet ledger: %w", err)
		}

		return nil
	})
}

func (s *AdminService) ListRechargePackages(ctx context.Context) ([]RechargePackage, error) {
	var packages []RechargePackage
	err := s.db.WithContext(ctx).Order("sort_order ASC, id ASC").Find(&packages).Error
	return packages, err
}

func (s *AdminService) SaveRechargePackage(ctx context.Context, pkg RechargePackage) error {
	if pkg.ID > 0 {
		return s.db.WithContext(ctx).Model(&pkg).Updates(map[string]any{
			"code":       pkg.Code,
			"name":       pkg.Name,
			"channel":    pkg.Channel,
			"amount_cny": pkg.AmountCNY,
			"points":     pkg.Points,
			"sort_order": pkg.SortOrder,
			"status":     pkg.Status,
		}).Error
	}
	return s.db.WithContext(ctx).Create(&pkg).Error
}

func (s *AdminService) DeleteRechargePackage(ctx context.Context, id uint64) error {
	return s.db.WithContext(ctx).Delete(&RechargePackage{}, id).Error
}

func (s *AdminService) ListWalletLedgers(ctx context.Context, search, opType, refID, dateFrom, dateTo string, offset, limit int) ([]WalletLedger, int64, error) {
	var ledgers []WalletLedger
	var total int64

	tx := s.db.WithContext(ctx).Table("wallet_ledger l").
		Joins("LEFT JOIN users u ON l.user_id = u.id")

	if search = strings.TrimSpace(search); search != "" {
		tx = tx.Where("u.username LIKE ? OR l.remark LIKE ?", "%"+search+"%", "%"+search+"%")
	}
	if opType = strings.TrimSpace(opType); opType != "" {
		tx = tx.Where("l.type = ?", opType)
	}
	if refID = strings.TrimSpace(refID); refID != "" {
		tx = tx.Where("l.ref_id LIKE ?", "%"+refID+"%")
	}
	if dateFrom = strings.TrimSpace(dateFrom); dateFrom != "" {
		if len(dateFrom) == 10 {
			dateFrom += " 00:00:00"
		}
		tx = tx.Where("l.created_at >= ?", dateFrom)
	}
	if dateTo = strings.TrimSpace(dateTo); dateTo != "" {
		if len(dateTo) == 10 {
			dateTo += " 23:59:59"
		}
		tx = tx.Where("l.created_at <= ?", dateTo)
	}

	if err := tx.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	if limit <= 0 {
		limit = 20
	}

	err := tx.Select("l.*, COALESCE(u.username, '') as username").
		Order("l.id DESC").
		Offset(offset).
		Limit(limit).
		Scan(&ledgers).Error
	if err != nil {
		return nil, 0, err
	}

	if len(ledgers) > 0 {
		userIDs := make([]uint64, 0, len(ledgers))
		for _, l := range ledgers {
			if l.Username == "" && l.UserID > 0 {
				userIDs = append(userIDs, l.UserID)
			}
		}
		if len(userIDs) > 0 {
			var users []User
			_ = s.db.WithContext(ctx).Where("id IN ?", userIDs).Find(&users).Error
			uMap := make(map[uint64]string, len(users))
			for _, u := range users {
				uMap[u.ID] = u.Username
			}
			for i := range ledgers {
				if ledgers[i].Username == "" {
					if name, ok := uMap[ledgers[i].UserID]; ok && name != "" {
						ledgers[i].Username = name
					}
				}
			}
		}
	}

	return ledgers, total, nil
}

// -----------------------------------------------------------------------------
// 4. Usage Statistics & Logs
// -----------------------------------------------------------------------------

type UsageLogFilter struct {
	Search   string
	Model    string
	Provider string
	Outcome  string // "succeeded", "failed"
	DateFrom string
	DateTo   string
	Offset   int
	Limit    int
}

func (s *AdminService) ListUsageLogs(ctx context.Context, filter UsageLogFilter) ([]UsageStatistic, int64, error) {
	var logs []UsageStatistic
	var total int64

	tx := s.db.WithContext(ctx).Table("usage_statistics u").
		Joins("LEFT JOIN api_keys k ON u.api_key_id = k.id").
		Joins("LEFT JOIN users usr ON u.user_id = usr.id")

	if search := strings.TrimSpace(filter.Search); search != "" {
		tx = tx.Where("k.api_key LIKE ? OR k.name LIKE ? OR usr.username LIKE ? OR u.client_ip LIKE ? OR u.model LIKE ?", "%"+search+"%", "%"+search+"%", "%"+search+"%", "%"+search+"%", "%"+search+"%")
	}
	if filter.Model != "" {
		tx = tx.Where("u.model LIKE ?", "%"+filter.Model+"%")
	}
	if filter.Provider != "" {
		tx = tx.Where("u.protocol = ?", filter.Provider)
	}
	if filter.Outcome == "succeeded" {
		tx = tx.Where("u.failed = 0")
	} else if filter.Outcome == "failed" {
		tx = tx.Where("u.failed = 1")
	}
	if filter.DateFrom != "" {
		df := strings.TrimSpace(filter.DateFrom)
		if len(df) == 10 {
			df += " 00:00:00"
		}
		tx = tx.Where("u.created_at >= ?", df)
	}
	if filter.DateTo != "" {
		dt := strings.TrimSpace(filter.DateTo)
		if len(dt) == 10 {
			dt += " 23:59:59"
		}
		tx = tx.Where("u.created_at <= ?", dt)
	}

	if err := tx.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	limit := filter.Limit
	if limit <= 0 {
		limit = 20
	}

	err := tx.Select("u.*, COALESCE(usr.username, '') as username, COALESCE(k.api_key, '') as api_key, COALESCE(k.name, '') as key_name").
		Order("u.created_at DESC").
		Offset(filter.Offset).
		Limit(limit).
		Scan(&logs).Error

	return logs, total, err
}

func (s *AdminService) GetUsageStats(ctx context.Context, days int) ([]UsageStatisticDaily, error) {
	if days <= 0 {
		days = 30
	}
	sinceDate := time.Now().Add(-time.Duration(days) * 24 * time.Hour).Format("2006-01-02")
	var stats []UsageStatisticDaily
	err := s.db.WithContext(ctx).Where("stat_date >= ?", sinceDate).Order("stat_date ASC").Find(&stats).Error
	return stats, err
}

// -----------------------------------------------------------------------------
// 5. AIGC Task Management
// -----------------------------------------------------------------------------

type AIGCTaskFilter struct {
	Search   string
	Kind     string
	Status   string
	Model    string
	DateFrom string
	DateTo   string
	Offset   int
	Limit    int
}

func (s *AdminService) ListAIGCTasks(ctx context.Context, filter AIGCTaskFilter) ([]ContentGeneration, int64, error) {
	var tasks []ContentGeneration
	var total int64

	tx := s.db.WithContext(ctx).Table("content_generations cg").
		Joins("LEFT JOIN api_keys k ON cg.api_key = k.api_key").
		Joins("LEFT JOIN users u ON k.user_id = u.id")

	if search := strings.TrimSpace(filter.Search); search != "" {
		tx = tx.Where("cg.generation_id LIKE ? OR u.username LIKE ? OR cg.api_key LIKE ?", "%"+search+"%", "%"+search+"%", "%"+search+"%")
	}
	if filter.Kind != "" {
		tx = tx.Where("cg.kind = ?", filter.Kind)
	}
	if filter.Status != "" {
		tx = tx.Where("cg.status = ?", filter.Status)
	}
	if filter.Model != "" {
		tx = tx.Where("cg.model LIKE ?", "%"+filter.Model+"%")
	}
	if filter.DateFrom != "" {
		df := strings.TrimSpace(filter.DateFrom)
		if len(df) == 10 {
			df += " 00:00:00"
		}
		tx = tx.Where("cg.created_at >= ?", df)
	}
	if filter.DateTo != "" {
		dt := strings.TrimSpace(filter.DateTo)
		if len(dt) == 10 {
			dt += " 23:59:59"
		}
		tx = tx.Where("cg.created_at <= ?", dt)
	}

	if err := tx.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	limit := filter.Limit
	if limit <= 0 {
		limit = 20
	}

	err := tx.Select("cg.generation_id, cg.request_id, cg.kind, cg.model, cg.status, cg.stage, cg.progress, cg.revision, cg.api_key, cg.client_ip, cg.input, cg.output, cg.provider, cg.provider_task_id, cg.error_code, cg.error_message, cg.billing_usage, cg.created_at, cg.updated_at, u.username").
		Order("cg.created_at DESC").
		Offset(filter.Offset).
		Limit(limit).
		Scan(&tasks).Error
	if err != nil {
		return nil, 0, err
	}

	// Preload artifacts for these tasks
	if len(tasks) > 0 {
		genIDs := make([]string, len(tasks))
		taskMap := make(map[string]*ContentGeneration, len(tasks))
		for i := range tasks {
			genIDs[i] = tasks[i].GenerationID
			taskMap[tasks[i].GenerationID] = &tasks[i]
		}
		var artifacts []ContentGenerationArtifact
		_ = s.db.WithContext(ctx).Where("generation_id IN ?", genIDs).Find(&artifacts).Error
		for _, art := range artifacts {
			if t, ok := taskMap[art.GenerationID]; ok {
				t.Artifacts = append(t.Artifacts, art)
			}
		}
	}

	return tasks, total, err
}

func (s *AdminService) GetAIGCTaskDetail(ctx context.Context, genID string) (*ContentGeneration, error) {
	var task ContentGeneration
	err := s.db.WithContext(ctx).
		Preload("Artifacts").
		Where("generation_id = ?", genID).
		First(&task).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}

	// Resolve username
	if task.APIKey != "" {
		var user User
		_ = s.db.WithContext(ctx).Table("users u").
			Joins("INNER JOIN api_keys k ON u.id = k.user_id").
			Where("k.api_key = ?", task.APIKey).
			Select("u.username").
			Scan(&user).Error
		task.Username = user.Username
	}

	return &task, nil
}

func (s *AdminService) CancelAIGCTask(ctx context.Context, genID string) error {
	return s.db.WithContext(ctx).Model(&ContentGeneration{}).
		Where("generation_id = ?", genID).
		Updates(map[string]any{
			"status":        "canceled",
			"stage":         "canceled",
			"error_message": "管理员手动取消任务",
			"updated_at":    time.Now(),
		}).Error
}

func (s *AdminService) RefundAIGCTask(ctx context.Context, genID string, amount decimal.Decimal, remark string) error {
	if amount.LessThanOrEqual(decimal.Zero) {
		return errors.New("退还金额必须大于0")
	}

	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var task ContentGeneration
		if err := tx.Where("generation_id = ?", genID).First(&task).Error; err != nil {
			return fmt.Errorf("find task: %w", err)
		}

		if task.APIKey == "" {
			return errors.New("任务未关联 API Key，无法退还至用户")
		}

		var key APIKey
		if err := tx.Where("api_key = ?", task.APIKey).First(&key).Error; err != nil {
			return fmt.Errorf("find key: %w", err)
		}

		var user User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&user, key.UserID).Error; err != nil {
			return fmt.Errorf("lock user: %w", err)
		}

		newBalance := user.Quota.Add(amount)
		// Also reduce frozen quota if frozen_quota >= amount
		newFrozen := user.FrozenQuota.Sub(amount)
		if newFrozen.IsNegative() {
			newFrozen = decimal.Zero
		}

		if err := tx.Model(&User{}).Where("id = ?", user.ID).Updates(map[string]any{
			"quota":        newBalance,
			"frozen_quota": newFrozen,
		}).Error; err != nil {
			return fmt.Errorf("update user quota: %w", err)
		}

		now := time.Now()
		ledger := WalletLedger{
			UserID:       user.ID,
			Type:         LedgerTypeAIGCRefund,
			DeltaQuota:   amount,
			DeltaFrozen:  newFrozen.Sub(user.FrozenQuota),
			BalanceAfter: newBalance,
			RefID:        task.GenerationID,
			Remark:       RemarkAdminRefund(remark, task.GenerationID),
			CreatedAt:    now,
		}
		if err := tx.Create(&ledger).Error; err != nil {
			return fmt.Errorf("create wallet ledger: %w", err)
		}

		return nil
	})
}

// -----------------------------------------------------------------------------
// 7. Error Mapping Rules & Triage
// -----------------------------------------------------------------------------

func (s *AdminService) ListErrorRules(ctx context.Context, provider string, status *int8, search string, offset, limit int) ([]ErrorMappingRule, int64, error) {
	var rules []ErrorMappingRule
	var total int64

	tx := s.db.WithContext(ctx).Model(&ErrorMappingRule{})

	if provider != "" && provider != "all" {
		tx = tx.Where("provider = ? OR provider = '*'", provider)
	}
	if status != nil {
		tx = tx.Where("status = ?", *status)
	}
	if search != "" {
		sWild := "%" + search + "%"
		tx = tx.Where("match_pattern LIKE ? OR standard_code LIKE ? OR user_message LIKE ? OR description LIKE ?", sWild, sWild, sWild, sWild)
	}

	if err := tx.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	if limit <= 0 {
		limit = 20
	}
	if err := tx.Order("priority ASC, id ASC").Offset(offset).Limit(limit).Find(&rules).Error; err != nil {
		return nil, 0, err
	}

	return rules, total, nil
}

func (s *AdminService) GetErrorRule(ctx context.Context, id uint64) (*ErrorMappingRule, error) {
	var rule ErrorMappingRule
	if err := s.db.WithContext(ctx).First(&rule, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &rule, nil
}

func (s *AdminService) CreateErrorRule(ctx context.Context, rule *ErrorMappingRule) error {
	if rule.MatchPattern == "" || rule.StandardCode == "" || rule.UserMessage == "" {
		return errors.New("匹配特征、标准错误码和中文提示文案为必填项")
	}
	if rule.Provider == "" {
		rule.Provider = "*"
	}
	if rule.MatchType == "" {
		rule.MatchType = "code_exact"
	}
	if rule.StandardType == "" {
		rule.StandardType = "invalid_request_error"
	}
	if rule.HTTPStatus <= 0 {
		rule.HTTPStatus = 400
	}
	if rule.Priority <= 0 {
		rule.Priority = 100
	}

	if err := s.db.WithContext(ctx).Create(rule).Error; err != nil {
		return err
	}
	_ = s.SyncErrorRulesToEngine(ctx)
	return nil
}

func (s *AdminService) UpdateErrorRule(ctx context.Context, rule *ErrorMappingRule) error {
	if rule.ID == 0 {
		return errors.New("规则 ID 不能为空")
	}
	if rule.MatchPattern == "" || rule.StandardCode == "" || rule.UserMessage == "" {
		return errors.New("匹配特征、标准错误码和中文提示文案为必填项")
	}

	if err := s.db.WithContext(ctx).Save(rule).Error; err != nil {
		return err
	}
	_ = s.SyncErrorRulesToEngine(ctx)
	return nil
}

func (s *AdminService) DeleteErrorRule(ctx context.Context, id uint64) error {
	if err := s.db.WithContext(ctx).Delete(&ErrorMappingRule{}, id).Error; err != nil {
		return err
	}
	_ = s.SyncErrorRulesToEngine(ctx)
	return nil
}

func (s *AdminService) TestErrorRule(ctx context.Context, provider string, statusCode int, rawBody string) aigc.NormalizedError {
	return aigc.NormalizeError(provider, statusCode, []byte(rawBody), nil)
}

func (s *AdminService) ListUnclassifiedErrors(ctx context.Context, limit int) ([]map[string]any, error) {
	if limit <= 0 {
		limit = 30
	}

	// Query recent failed tasks from content_generations
	type rawErrRow struct {
		Provider     string `json:"provider"`
		Model        string `json:"model"`
		ErrorCode    string `json:"error_code"`
		ErrorMessage string `json:"error_message"`
		Count        int64  `json:"count"`
		LastOccurred string `json:"last_occurred"`
	}

	var rows []rawErrRow
	err := s.db.WithContext(ctx).Raw(`
		SELECT 
			COALESCE(provider, 'unknown') as provider,
			COALESCE(model, '') as model,
			COALESCE(error_code, 'NO_CODE') as error_code,
			LEFT(error_message, 255) as error_message,
			COUNT(*) as count,
			MAX(created_at) as last_occurred
		FROM content_generations
		WHERE status = 'failed'
		GROUP BY provider, model, error_code, LEFT(error_message, 255)
		ORDER BY count DESC
		LIMIT ?
	`, limit).Scan(&rows).Error

	if err != nil {
		return nil, err
	}

	results := make([]map[string]any, 0, len(rows))
	for _, r := range rows {
		results = append(results, map[string]any{
			"provider":      r.Provider,
			"model":         r.Model,
			"error_code":    r.ErrorCode,
			"error_message": r.ErrorMessage,
			"count":         r.Count,
			"last_occurred": r.LastOccurred,
		})
	}

	return results, nil
}

func (s *AdminService) SyncErrorRulesToEngine(ctx context.Context) error {
	var dbRules []ErrorMappingRule
	if err := s.db.WithContext(ctx).Where("status = ?", 1).Order("priority ASC, id ASC").Find(&dbRules).Error; err != nil {
		return err
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

	aigc.SetDynamicRules(rules)
	return nil
}

// -----------------------------------------------------------------------------
// 9. API Doc Articles Management
// -----------------------------------------------------------------------------

func (s *AdminService) ListDocArticles(ctx context.Context, category string, status *int8, search string) ([]DocArticle, error) {
	query := s.db.WithContext(ctx).Model(&DocArticle{})
	if category != "" && category != "all" {
		query = query.Where("category = ?", category)
	}
	if status != nil {
		query = query.Where("status = ?", *status)
	}
	if search != "" {
		sPattern := "%" + search + "%"
		query = query.Where("title LIKE ? OR slug LIKE ? OR badge LIKE ?", sPattern, sPattern, sPattern)
	}

	var list []DocArticle
	err := query.Order("sort_order ASC, id ASC").Find(&list).Error
	return list, err
}

func (s *AdminService) GetDocArticle(ctx context.Context, id uint64) (*DocArticle, error) {
	var doc DocArticle
	err := s.db.WithContext(ctx).First(&doc, id).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &doc, nil
}

func (s *AdminService) GetDocArticleBySlug(ctx context.Context, slug string) (*DocArticle, error) {
	var doc DocArticle
	err := s.db.WithContext(ctx).Where("slug = ?", slug).First(&doc).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &doc, nil
}

func (s *AdminService) CreateDocArticle(ctx context.Context, doc *DocArticle) error {
	doc.Title = strings.TrimSpace(doc.Title)
	doc.Slug = strings.ToLower(strings.TrimSpace(doc.Slug))
	doc.Badge = strings.TrimSpace(doc.Badge)
	doc.Category = strings.TrimSpace(doc.Category)
	doc.Icon = strings.TrimSpace(doc.Icon)

	if doc.Title == "" {
		return errors.New("文档标题不能为空")
	}
	if doc.Slug == "" {
		return errors.New("文档标识 Slug 不能为空")
	}
	if strings.TrimSpace(doc.ContentMD) == "" {
		return errors.New("文档 Markdown 内容不能为空")
	}

	existing, err := s.GetDocArticleBySlug(ctx, doc.Slug)
	if err != nil {
		return fmt.Errorf("check slug duplicate: %w", err)
	}
	if existing != nil {
		return errors.New("文档标识 Slug 已存在，请更换")
	}

	return s.db.WithContext(ctx).Create(doc).Error
}

func (s *AdminService) UpdateDocArticle(ctx context.Context, doc *DocArticle) error {
	doc.Title = strings.TrimSpace(doc.Title)
	doc.Slug = strings.ToLower(strings.TrimSpace(doc.Slug))
	doc.Badge = strings.TrimSpace(doc.Badge)
	doc.Category = strings.TrimSpace(doc.Category)
	doc.Icon = strings.TrimSpace(doc.Icon)

	if doc.ID == 0 {
		return errors.New("文档 ID 不能为空")
	}
	if doc.Title == "" {
		return errors.New("文档标题不能为空")
	}
	if doc.Slug == "" {
		return errors.New("文档标识 Slug 不能为空")
	}
	if strings.TrimSpace(doc.ContentMD) == "" {
		return errors.New("文档 Markdown 内容不能为空")
	}

	existing, err := s.GetDocArticleBySlug(ctx, doc.Slug)
	if err != nil {
		return fmt.Errorf("check slug duplicate: %w", err)
	}
	if existing != nil && existing.ID != doc.ID {
		return errors.New("文档标识 Slug 已存在，请更换")
	}

	return s.db.WithContext(ctx).Save(doc).Error
}

func (s *AdminService) ToggleDocArticleStatus(ctx context.Context, id uint64) error {
	doc, err := s.GetDocArticle(ctx, id)
	if err != nil || doc == nil {
		return errors.New("文档不存在")
	}
	newStatus := int8(1)
	if doc.Status == 1 {
		newStatus = 0
	}
	return s.db.WithContext(ctx).Model(&DocArticle{}).Where("id = ?", id).Update("status", newStatus).Error
}

func (s *AdminService) DeleteDocArticle(ctx context.Context, id uint64) error {
	return s.db.WithContext(ctx).Delete(&DocArticle{}, id).Error
}
