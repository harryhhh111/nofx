package store

import (
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// setupMemoryTestDB 创建一个用于记忆功能测试的内存 SQLite DB
// 同时迁移 DecisionRecordDB 和 TraderPosition 两张表
func setupMemoryTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Skipf("skipping memory test (CGO may be disabled): %v", err)
	}
	if err := db.AutoMigrate(&DecisionRecordDB{}, &TraderPosition{}); err != nil {
		t.Fatalf("AutoMigrate failed: %v", err)
	}
	return db
}

// insertDecisionRecord 向 decision_records 插入一条测试记录
func insertDecisionRecord(t *testing.T, db *gorm.DB, traderID string, cycle int, ts time.Time, cotSummary string, success bool) {
	t.Helper()
	record := &DecisionRecordDB{
		TraderID:    traderID,
		CycleNumber: cycle,
		Timestamp:   ts,
		CotSummary:  cotSummary,
		Success:     success,
	}
	if err := db.Create(record).Error; err != nil {
		t.Fatalf("insertDecisionRecord failed: %v", err)
	}
}

// ============================================================================
// Phase 1 — GetCotSummaryByCycle 单元测试
// ============================================================================

// TestGetCotSummaryByCycle_Found 验证按 traderID+cycleNumber 能正确取到 CotSummary
func TestGetCotSummaryByCycle_Found(t *testing.T) {
	db := setupMemoryTestDB(t)
	s := NewDecisionStore(db)
	traderID := "trader-001"

	insertDecisionRecord(t, db, traderID, 5, time.Now().UTC(), "BTC bullish: EMA cross confirmed, RSI 62", true)
	insertDecisionRecord(t, db, traderID, 6, time.Now().UTC(), "BTC hold: momentum fading", true)

	// 取 cycle 5 的摘要
	got := s.GetCotSummaryByCycle(traderID, 5)
	if got != "BTC bullish: EMA cross confirmed, RSI 62" {
		t.Errorf("GetCotSummaryByCycle(5) = %q, want specific summary", got)
	}

	// 取 cycle 6 的摘要
	got6 := s.GetCotSummaryByCycle(traderID, 6)
	if got6 != "BTC hold: momentum fading" {
		t.Errorf("GetCotSummaryByCycle(6) = %q, want different summary", got6)
	}
}

// TestGetCotSummaryByCycle_NotFound 验证不存在时返回空字符串而不报错
func TestGetCotSummaryByCycle_NotFound(t *testing.T) {
	db := setupMemoryTestDB(t)
	s := NewDecisionStore(db)

	got := s.GetCotSummaryByCycle("nonexistent", 99)
	if got != "" {
		t.Errorf("expected empty string for nonexistent cycle, got %q", got)
	}
}

// TestGetCotSummaryByCycle_ZeroCycle 验证 cycleNumber=0 快速返回空字符串
func TestGetCotSummaryByCycle_ZeroCycle(t *testing.T) {
	db := setupMemoryTestDB(t)
	s := NewDecisionStore(db)

	got := s.GetCotSummaryByCycle("any-trader", 0)
	if got != "" {
		t.Errorf("expected empty string for cycle=0, got %q", got)
	}
}

// TestGetCotSummaryByCycle_CrossTrader 验证不同 trader 之间的数据不互相污染
func TestGetCotSummaryByCycle_CrossTrader(t *testing.T) {
	db := setupMemoryTestDB(t)
	s := NewDecisionStore(db)

	insertDecisionRecord(t, db, "trader-A", 1, time.Now().UTC(), "Trader A summary", true)
	insertDecisionRecord(t, db, "trader-B", 1, time.Now().UTC(), "Trader B summary", true)

	// trader-A cycle 1 不能取到 trader-B 的摘要
	gotA := s.GetCotSummaryByCycle("trader-A", 1)
	if gotA != "Trader A summary" {
		t.Errorf("trader-A cycle 1 got %q, want 'Trader A summary'", gotA)
	}

	gotB := s.GetCotSummaryByCycle("trader-B", 1)
	if gotB != "Trader B summary" {
		t.Errorf("trader-B cycle 1 got %q, want 'Trader B summary'", gotB)
	}
}

// ============================================================================
// Phase 2 — GetRecentCotSummaries 单元测试
// ============================================================================

// TestGetRecentCotSummaries_WithinWindow 验证时间窗口内的记录能正确返回
func TestGetRecentCotSummaries_WithinWindow(t *testing.T) {
	db := setupMemoryTestDB(t)
	s := NewDecisionStore(db)
	traderID := "trader-001"
	now := time.Now().UTC()

	// 3 条在 6 小时内（从旧到新）
	insertDecisionRecord(t, db, traderID, 1, now.Add(-5*time.Hour), "Old summary", true)
	insertDecisionRecord(t, db, traderID, 2, now.Add(-3*time.Hour), "Mid summary", true)
	insertDecisionRecord(t, db, traderID, 3, now.Add(-1*time.Hour), "Recent summary", true)

	results := s.GetRecentCotSummaries(traderID, 6, 10)
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}
	// 结果应为从旧到新顺序
	if results[0] != "Old summary" {
		t.Errorf("results[0] = %q, want 'Old summary' (oldest first)", results[0])
	}
	if results[2] != "Recent summary" {
		t.Errorf("results[2] = %q, want 'Recent summary' (newest last)", results[2])
	}
}

// TestGetRecentCotSummaries_OutsideWindow 验证超出时间窗口的记录被过滤
func TestGetRecentCotSummaries_OutsideWindow(t *testing.T) {
	db := setupMemoryTestDB(t)
	s := NewDecisionStore(db)
	traderID := "trader-001"
	now := time.Now().UTC()

	insertDecisionRecord(t, db, traderID, 1, now.Add(-10*time.Hour), "Very old - outside window", true)
	insertDecisionRecord(t, db, traderID, 2, now.Add(-1*time.Hour), "Recent - inside window", true)

	results := s.GetRecentCotSummaries(traderID, 6, 10)
	if len(results) != 1 {
		t.Fatalf("expected 1 result (only within 6h), got %d", len(results))
	}
	if results[0] != "Recent - inside window" {
		t.Errorf("got %q, want 'Recent - inside window'", results[0])
	}
}

// TestGetRecentCotSummaries_ExcludesFailed 验证 success=false 的记录被排除
func TestGetRecentCotSummaries_ExcludesFailed(t *testing.T) {
	db := setupMemoryTestDB(t)
	s := NewDecisionStore(db)
	traderID := "trader-001"
	now := time.Now().UTC()

	insertDecisionRecord(t, db, traderID, 1, now.Add(-1*time.Hour), "Failed decision summary", false) // 应被排除
	insertDecisionRecord(t, db, traderID, 2, now.Add(-30*time.Minute), "Success decision summary", true)

	results := s.GetRecentCotSummaries(traderID, 6, 10)
	if len(results) != 1 {
		t.Fatalf("expected 1 result (failed excluded), got %d", len(results))
	}
	if results[0] != "Success decision summary" {
		t.Errorf("got %q, want 'Success decision summary'", results[0])
	}
}

// TestGetRecentCotSummaries_ExcludesEmptySummary 验证 cot_summary 为空的记录被排除
func TestGetRecentCotSummaries_ExcludesEmptySummary(t *testing.T) {
	db := setupMemoryTestDB(t)
	s := NewDecisionStore(db)
	traderID := "trader-001"
	now := time.Now().UTC()

	insertDecisionRecord(t, db, traderID, 1, now.Add(-1*time.Hour), "", true) // 空摘要应排除
	insertDecisionRecord(t, db, traderID, 2, now.Add(-30*time.Minute), "Real summary", true)

	results := s.GetRecentCotSummaries(traderID, 6, 10)
	if len(results) != 1 {
		t.Fatalf("expected 1 result (empty summary excluded), got %d", len(results))
	}
}

// TestGetRecentCotSummaries_LimitRespected 验证 limit 参数生效
func TestGetRecentCotSummaries_LimitRespected(t *testing.T) {
	db := setupMemoryTestDB(t)
	s := NewDecisionStore(db)
	traderID := "trader-001"
	now := time.Now().UTC()

	// 插入 5 条，但 limit=3
	for i := 0; i < 5; i++ {
		insertDecisionRecord(t, db, traderID, i+1,
			now.Add(-time.Duration(5-i)*time.Hour),
			"summary "+string(rune('A'+i)), true)
	}

	results := s.GetRecentCotSummaries(traderID, 24, 3)
	if len(results) != 3 {
		t.Errorf("expected 3 results with limit=3, got %d", len(results))
	}
}

// TestGetRecentCotSummaries_NoWindowFilter 验证 withinHours=0 时不过滤时间
func TestGetRecentCotSummaries_NoWindowFilter(t *testing.T) {
	db := setupMemoryTestDB(t)
	s := NewDecisionStore(db)
	traderID := "trader-001"
	now := time.Now().UTC()

	// 插入一条很久以前的记录
	insertDecisionRecord(t, db, traderID, 1, now.Add(-720*time.Hour), "Ancient summary", true)

	// withinHours=0 不过滤时间
	results := s.GetRecentCotSummaries(traderID, 0, 10)
	if len(results) != 1 {
		t.Errorf("expected 1 result with no time filter, got %d", len(results))
	}
}

// TestGetRecentCotSummaries_Empty 验证无记录时返回空切片而不是 nil
func TestGetRecentCotSummaries_Empty(t *testing.T) {
	db := setupMemoryTestDB(t)
	s := NewDecisionStore(db)

	results := s.GetRecentCotSummaries("nonexistent-trader", 6, 10)
	if results == nil {
		t.Errorf("expected empty slice, got nil")
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

// ============================================================================
// Phase 1 — opening_cycle 字段写入验证（store 层）
// ============================================================================

// TestTraderPosition_OpeningCycleField 验证 TraderPosition 能正确存储 opening_cycle 字段
func TestTraderPosition_OpeningCycleField(t *testing.T) {
	db := setupMemoryTestDB(t)
	posStore := NewPositionStore(db)

	now := time.Now().UTC().UnixMilli()
	pos := &TraderPosition{
		TraderID:     "trader-001",
		ExchangeID:   "exchange-001",
		ExchangeType: "binance",
		Symbol:       "BTCUSDT",
		Side:         "LONG",
		Quantity:     0.01,
		EntryPrice:   95000.0,
		EntryOrderID: "order-123",
		EntryTime:    now,
		Leverage:     5,
		Status:       "OPEN",
		OpeningCycle: 42, // 模拟开仓时的决策周期号
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	if err := posStore.Create(pos); err != nil {
		t.Fatalf("Create position failed: %v", err)
	}

	// 查回来验证 opening_cycle 有正确写入
	dbPos, err := posStore.GetOpenPositionBySymbol("trader-001", "BTCUSDT", "LONG")
	if err != nil {
		t.Fatalf("GetOpenPositionBySymbol failed: %v", err)
	}
	if dbPos == nil {
		t.Fatal("expected position, got nil")
	}
	if dbPos.OpeningCycle != 42 {
		t.Errorf("OpeningCycle = %d, want 42", dbPos.OpeningCycle)
	}
}

// TestPhase1_EndToEnd 端到端验证：写入持仓 opening_cycle → 查询 CotSummary → 链路打通
func TestPhase1_EndToEnd(t *testing.T) {
	db := setupMemoryTestDB(t)
	decisionStore := NewDecisionStore(db)
	posStore := NewPositionStore(db)

	traderID := "trader-e2e"
	openingCycle := 10
	now := time.Now().UTC()

	// 1. 模拟开仓决策写入 cot_summary
	insertDecisionRecord(t, db, traderID, openingCycle, now.Add(-2*time.Hour),
		"BTC breakout confirmed: EMA20 > EMA50, MACD positive, RSI=65. Opening long.", true)

	// 2. 模拟开仓时写入 opening_cycle
	nowMs := now.UnixMilli()
	err := posStore.Create(&TraderPosition{
		TraderID:     traderID,
		ExchangeID:   "ex-001",
		ExchangeType: "binance",
		Symbol:       "BTCUSDT",
		Side:         "LONG",
		Quantity:     0.01,
		EntryPrice:   95000,
		EntryTime:    nowMs,
		Leverage:     5,
		Status:       "OPEN",
		OpeningCycle: openingCycle, // 关键字段
		CreatedAt:    nowMs,
		UpdatedAt:    nowMs,
	})
	if err != nil {
		t.Fatalf("Create position failed: %v", err)
	}

	// 3. 模拟 buildTradingContext: 通过持仓查 opening_cycle，再查 cot_summary
	dbPos, err := posStore.GetOpenPositionBySymbol(traderID, "BTCUSDT", "LONG")
	if err != nil {
		t.Fatalf("GetOpenPositionBySymbol failed: %v", err)
	}
	if dbPos.OpeningCycle != openingCycle {
		t.Fatalf("OpeningCycle = %d, want %d", dbPos.OpeningCycle, openingCycle)
	}

	summary := decisionStore.GetCotSummaryByCycle(traderID, dbPos.OpeningCycle)
	if summary == "" {
		t.Fatal("GetCotSummaryByCycle returned empty string, expected position memory")
	}
	if summary != "BTC breakout confirmed: EMA20 > EMA50, MACD positive, RSI=65. Opening long." {
		t.Errorf("summary = %q, mismatch", summary)
	}

	t.Logf("✅ Phase 1 end-to-end chain verified: position opening_cycle=%d → cot_summary=%q", openingCycle, summary)
}

// ============================================================================
// Phase 6 — GetRecentClosedPositions 单元测试
// ============================================================================

// insertClosedPosition 向 trader_positions 插入一条已平仓记录，供测试复用
func insertClosedPosition(t *testing.T, db *gorm.DB, traderID, symbol string, openingCycle int, entryTime, exitTime int64, pnl float64) {
	t.Helper()
	pos := &TraderPosition{
		TraderID:     traderID,
		ExchangeID:   "ex-test",
		ExchangeType: "binance",
		Symbol:       symbol,
		Side:         "LONG",
		Quantity:     0.01,
		EntryPrice:   90000,
		ExitPrice:    90000 + pnl*100, // 粗略模拟出价
		EntryTime:    entryTime,
		ExitTime:     exitTime,
		Leverage:     5,
		Status:       "CLOSED",
		RealizedPnL:  pnl,
		OpeningCycle: openingCycle,
		CreatedAt:    entryTime,
		UpdatedAt:    exitTime,
	}
	if err := db.Create(pos).Error; err != nil {
		t.Fatalf("insertClosedPosition failed: %v", err)
	}
}

// TestGetRecentClosedPositions_OnlyWithOpeningCycle 验证无 opening_cycle 的平仓记录不返回
func TestGetRecentClosedPositions_OnlyWithOpeningCycle(t *testing.T) {
	db := setupMemoryTestDB(t)
	posStore := NewPositionStore(db)
	traderID := "trader-001"
	now := time.Now().UTC().UnixMilli()

	// opening_cycle=0 的旧记录（Phase 1 之前）
	db.Create(&TraderPosition{
		TraderID: traderID, Symbol: "ETHUSDT", Side: "LONG",
		Status: "CLOSED", OpeningCycle: 0,
		EntryTime: now - 10000, ExitTime: now, RealizedPnL: 50,
		ExchangeID: "ex", ExchangeType: "binance", Leverage: 5,
	})
	// opening_cycle>0 的新记录
	insertClosedPosition(t, db, traderID, "BTCUSDT", 5, now-20000, now, 200)

	results, err := posStore.GetRecentClosedPositions(traderID, 10)
	if err != nil {
		t.Fatalf("GetRecentClosedPositions error: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("expected 1 result (opening_cycle>0 only), got %d", len(results))
	}
	if results[0].Symbol != "BTCUSDT" {
		t.Errorf("got symbol %q, want 'BTCUSDT'", results[0].Symbol)
	}
}

// TestGetRecentClosedPositions_OrderByExitTime 验证最新平仓的记录排在最前
func TestGetRecentClosedPositions_OrderByExitTime(t *testing.T) {
	db := setupMemoryTestDB(t)
	posStore := NewPositionStore(db)
	traderID := "trader-001"
	now := time.Now().UTC().UnixMilli()

	insertClosedPosition(t, db, traderID, "BTCUSDT", 1, now-30000, now-10000, 100) // 较早平仓
	insertClosedPosition(t, db, traderID, "ETHUSDT", 2, now-20000, now-5000, 200)  // 较晚平仓
	insertClosedPosition(t, db, traderID, "SOLUSDT", 3, now-10000, now-1000, -50)  // 最新平仓

	results, err := posStore.GetRecentClosedPositions(traderID, 10)
	if err != nil {
		t.Fatalf("GetRecentClosedPositions error: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}
	// 最新平仓 (SOLUSDT) 应排第一
	if results[0].Symbol != "SOLUSDT" {
		t.Errorf("results[0].Symbol = %q, want 'SOLUSDT' (latest exit_time first)", results[0].Symbol)
	}
	if results[2].Symbol != "BTCUSDT" {
		t.Errorf("results[2].Symbol = %q, want 'BTCUSDT' (earliest exit_time last)", results[2].Symbol)
	}
}

// TestPhase6_EndToEnd 端到端验证完整平仓复盘链路：
// 开仓写 opening_cycle → 写 cot_summary → 平仓 → GetRecentClosedPositions → GetCotSummaryByCycle → 构建 ClosedTradeReflection
func TestPhase6_EndToEnd(t *testing.T) {
	db := setupMemoryTestDB(t)
	decisionStore := NewDecisionStore(db)
	posStore := NewPositionStore(db)

	traderID := "trader-p6"
	openingCycle := 7
	now := time.Now().UTC()
	nowMs := now.UnixMilli()
	openReason := "ETH bearish: MACD negative cross, OI dropping, RSI=38. Opening short."

	// Step 1: 开仓决策写 cot_summary
	insertDecisionRecord(t, db, traderID, openingCycle, now.Add(-3*time.Hour), openReason, true)

	// Step 2: 开仓，写入 opening_cycle
	entryTime := now.Add(-3 * time.Hour).UnixMilli()
	pos := &TraderPosition{
		TraderID: traderID, ExchangeID: "ex", ExchangeType: "binance",
		Symbol: "ETHUSDT", Side: "SHORT", Quantity: 0.1,
		EntryPrice: 2500, Leverage: 5,
		EntryTime: entryTime, Status: "OPEN",
		OpeningCycle: openingCycle,
		CreatedAt:    nowMs, UpdatedAt: nowMs,
	}
	if err := posStore.Create(pos); err != nil {
		t.Fatalf("Create position failed: %v", err)
	}

	// Step 3: 模拟平仓（直接 DB 更新）
	exitTime := now.Add(-1 * time.Hour).UnixMilli()
	db.Model(&TraderPosition{}).
		Where("trader_id = ? AND symbol = ? AND status = ?", traderID, "ETHUSDT", "OPEN").
		Updates(map[string]interface{}{
			"status": "CLOSED", "exit_price": 2600.0,
			"realized_pnl": -100.0, "exit_time": exitTime,
		})

	// Step 4: 查最近已平仓持仓
	closedPositions, err := posStore.GetRecentClosedPositions(traderID, 3)
	if err != nil {
		t.Fatalf("GetRecentClosedPositions error: %v", err)
	}
	if len(closedPositions) != 1 {
		t.Fatalf("expected 1 closed position, got %d", len(closedPositions))
	}

	cp := closedPositions[0]
	if cp.OpeningCycle != openingCycle {
		t.Errorf("OpeningCycle = %d, want %d", cp.OpeningCycle, openingCycle)
	}

	// Step 5: 通过 opening_cycle 查开仓理由
	reason := decisionStore.GetCotSummaryByCycle(traderID, cp.OpeningCycle)
	if reason != openReason {
		t.Errorf("reason = %q, want %q", reason, openReason)
	}

	// Step 6: 验证可以构建完整 ClosedTradeReflection（模拟 auto_trader 逻辑）
	isProfit := cp.RealizedPnL > 0
	if isProfit {
		t.Error("expected loss (IsProfit=false) for this trade")
	}
	if cp.ExitPrice != 2600.0 {
		t.Errorf("ExitPrice = %.2f, want 2600.00", cp.ExitPrice)
	}

	t.Logf("✅ Phase 6 end-to-end chain verified:")
	t.Logf("   opening_cycle=%d → cot_summary=%q", openingCycle, reason)
	t.Logf("   closed: entry=%.2f exit=%.2f pnl=%.2f IsProfit=%v", cp.EntryPrice, cp.ExitPrice, cp.RealizedPnL, isProfit)
}

// ============================================================================
// Phase 7 — UpdatePositionReviewSummary 单元测试
// ============================================================================

// TestUpdatePositionReviewSummary_Basic 验证写入 → 查询，last_review_summary 和 last_review_cycle 正确存储
func TestUpdatePositionReviewSummary_Basic(t *testing.T) {
	db := setupMemoryTestDB(t)
	posStore := NewPositionStore(db)
	traderID := "trader-001"
	now := time.Now().UTC().UnixMilli()

	// 创建一个 OPEN 持仓
	pos := &TraderPosition{
		TraderID: traderID, ExchangeID: "ex", ExchangeType: "binance",
		Symbol: "BTCUSDT", Side: "LONG", Quantity: 0.01,
		EntryPrice: 93000, Leverage: 5, Status: "OPEN",
		EntryTime: now, CreatedAt: now, UpdatedAt: now,
	}
	if err := posStore.Create(pos); err != nil {
		t.Fatalf("Create position failed: %v", err)
	}

	snapshot := "[cycle 5, 30m ago, price 93500.0000, pnl +50.00 USDT +0.54%, peak 0.80%] SL:92000.0000 TP:96000.0000 conf:75 | EMA structure intact, holding"

	// 写入审查快照
	if err := posStore.UpdatePositionReviewSummary(traderID, "BTCUSDT", "LONG", 5, snapshot); err != nil {
		t.Fatalf("UpdatePositionReviewSummary failed: %v", err)
	}

	// 查回验证
	dbPos, err := posStore.GetOpenPositionBySymbol(traderID, "BTCUSDT", "LONG")
	if err != nil {
		t.Fatalf("GetOpenPositionBySymbol failed: %v", err)
	}
	if dbPos.LastReviewSummary != snapshot {
		t.Errorf("LastReviewSummary = %q, want %q", dbPos.LastReviewSummary, snapshot)
	}
	if dbPos.LastReviewCycle != 5 {
		t.Errorf("LastReviewCycle = %d, want 5", dbPos.LastReviewCycle)
	}
}

// TestUpdatePositionReviewSummary_Overwrite 验证多次写入时，只保留最新一条
func TestUpdatePositionReviewSummary_Overwrite(t *testing.T) {
	db := setupMemoryTestDB(t)
	posStore := NewPositionStore(db)
	traderID := "trader-001"
	now := time.Now().UTC().UnixMilli()

	pos := &TraderPosition{
		TraderID: traderID, ExchangeID: "ex", ExchangeType: "binance",
		Symbol: "ETHUSDT", Side: "SHORT", Quantity: 0.1,
		EntryPrice: 2500, Leverage: 5, Status: "OPEN",
		EntryTime: now, CreatedAt: now, UpdatedAt: now,
	}
	if err := posStore.Create(pos); err != nil {
		t.Fatalf("Create position failed: %v", err)
	}

	// 第一次写入（cycle 3）
	if err := posStore.UpdatePositionReviewSummary(traderID, "ETHUSDT", "SHORT", 3, "first review"); err != nil {
		t.Fatalf("First update failed: %v", err)
	}
	// 第二次写入（cycle 7）应覆盖
	if err := posStore.UpdatePositionReviewSummary(traderID, "ETHUSDT", "SHORT", 7, "second review, updated"); err != nil {
		t.Fatalf("Second update failed: %v", err)
	}

	dbPos, err := posStore.GetOpenPositionBySymbol(traderID, "ETHUSDT", "SHORT")
	if err != nil {
		t.Fatalf("GetOpenPositionBySymbol failed: %v", err)
	}
	if dbPos.LastReviewSummary != "second review, updated" {
		t.Errorf("LastReviewSummary = %q, want 'second review, updated'", dbPos.LastReviewSummary)
	}
	if dbPos.LastReviewCycle != 7 {
		t.Errorf("LastReviewCycle = %d, want 7", dbPos.LastReviewCycle)
	}
}

// TestPhase7_EndToEnd 端到端验证完整审查快照链路：
// 开仓 → hold 写快照 → GetOpenPositionBySymbol 读出 → 验证 LastReviewSummary 非空且正确
func TestPhase7_EndToEnd(t *testing.T) {
	db := setupMemoryTestDB(t)
	posStore := NewPositionStore(db)

	traderID := "trader-p7"
	now := time.Now().UTC().UnixMilli()

	// Step 1: 开仓
	err := posStore.Create(&TraderPosition{
		TraderID: traderID, ExchangeID: "ex", ExchangeType: "binance",
		Symbol: "SOLUSDT", Side: "LONG", Quantity: 1.0,
		EntryPrice: 150.0, Leverage: 5, Status: "OPEN",
		EntryTime: now, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatalf("Create position failed: %v", err)
	}

	// Step 2: 模拟第一个决策周期 hold，写入审查快照
	snapshot1 := "[cycle 3, 10m ago, price 151.2000, pnl +12.00 USDT +0.80%, peak 0.80%] SL:145.0000 TP:165.0000 conf:72 | Uptrend intact, holding long"
	if err := posStore.UpdatePositionReviewSummary(traderID, "SOLUSDT", "LONG", 3, snapshot1); err != nil {
		t.Fatalf("UpdatePositionReviewSummary (cycle 3) failed: %v", err)
	}

	// Step 3: 模拟第二个决策周期 hold，覆盖快照
	snapshot2 := "[cycle 6, 10m ago, price 153.5000, pnl +35.00 USDT +2.33%, peak 2.40%] SL:148.0000 TP:168.0000 conf:80 | Momentum accelerating, raised SL"
	if err := posStore.UpdatePositionReviewSummary(traderID, "SOLUSDT", "LONG", 6, snapshot2); err != nil {
		t.Fatalf("UpdatePositionReviewSummary (cycle 6) failed: %v", err)
	}

	// Step 4: 模拟 buildTradingContext 步骤 7b 读取
	dbPos, err := posStore.GetOpenPositionBySymbol(traderID, "SOLUSDT", "LONG")
	if err != nil {
		t.Fatalf("GetOpenPositionBySymbol failed: %v", err)
	}
	if dbPos.LastReviewSummary == "" {
		t.Fatal("LastReviewSummary is empty, expected snapshot to be written")
	}
	if dbPos.LastReviewSummary != snapshot2 {
		t.Errorf("LastReviewSummary = %q, want latest snapshot", dbPos.LastReviewSummary)
	}
	if dbPos.LastReviewCycle != 6 {
		t.Errorf("LastReviewCycle = %d, want 6", dbPos.LastReviewCycle)
	}

	t.Logf("✅ Phase 7 end-to-end chain verified:")
	t.Logf("   LastReviewCycle=%d → LastReviewSummary=%q", dbPos.LastReviewCycle, dbPos.LastReviewSummary)
}
