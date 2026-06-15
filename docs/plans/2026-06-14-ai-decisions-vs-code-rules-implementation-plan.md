# AI 决策与代码规则协作边界落地实施方案

> 对应分析文档 [`docs/analysis/2026-06-14-ai-decisions-vs-code-rules.md`](../analysis/2026-06-14-ai-decisions-vs-code-rules.md)。
> 目标：在代码层实现“AI 产出假设 → 代码规则审查 → 治理闭环追踪”的三层边界。

---

## 一、与现有方案的关系

- [`docs/plans/2026-06-14-strategy-lifecycle-optimization-implementation-plan.md`](./2026-06-14-strategy-lifecycle-optimization-implementation-plan.md) 已经覆盖：UI 收益率口径、方向/币种级 `consecutive_loss_brake`、趋势末期观望、TP 外推 soft guard、R:R 分层。
- 本文档补齐其未覆盖的：**规则命中遥测与治理、Prompt 结构化改造、候选池 ranking 约束、AI 与代码差异看板**。
- 落地时以 lifecycle plan 的 Task 1~5 为主线，本文档的 Phase 1~3 作为基础设施并行或提前做。

---

## 二、通用落地原则

1. **每个任务一个独立 PR**，便于回滚和 review。
2. **不删除旧字段、不改旧接口语义**，只新增字段/配置/表。
3. 配置字段 JSON 用 `snake_case`，Go struct 用 `CamelCase`。
4. 所有 Go 改动必须通过：
   ```bash
   go test ./trader/... ./kernel/... ./store/... -count=1
   ```
5. 前端改动需通过：
   ```bash
   npm run lint
   npm run build
   ```
6. 新增 i18n key 必须补 `zh` / `en` / `es` 三语（`es` 可先用英文占位）。
7. 非资金安全类的新配置，对已有策略**默认关闭**，避免惊喜行为变更。

---

## 三、落地顺序

```text
Phase 1: 规则命中遥测与治理事件表   （基础设施，必须先有数据）
Phase 2: Prompt 结构化输出改造        （让 AI 自检字段可被消费）
Phase 3: 候选池 ranking 约束          （限制 AI 挑币范围）
Phase 4: TP 锚点可信度 soft guard     （复用现有数据，先做粗锚点）
Phase 5: 退出纪律代码化               （trailing_stop / time_stop）
Phase 6: 回测 / paper trading / 看板  （基于已有数据闭环验证）
```

---

## 四、Phase 1：规则命中遥测与治理事件表

### 4.1 目标

每次代码规则命中都要写入事件，而不是只把决策改成 `wait` 或在 `reasoning` 里留一句话。没有遥测，规则只会越加越多，无法评估误杀/漏杀。

### 4.2 新增文件

- `store/guard_event.go`：数据模型、`GuardEventStore`、迁移。
- `kernel/guard_logger.go`（可选）：把 guard 结果转成 `GuardEvent` 的辅助函数。

### 4.3 DB Schema（PostgreSQL）

```sql
CREATE TABLE IF NOT EXISTS guard_events (
    id BIGSERIAL PRIMARY KEY,
    trader_id TEXT NOT NULL,
    decision_record_id BIGINT,
    cycle_number INT NOT NULL,
    strategy_id TEXT,
    guard_type TEXT NOT NULL, -- entry_risk_guard | rr_check | tp_anchor | cooldown | lifecycle_exit | hard_safety | candidate_pool
    action TEXT NOT NULL,     -- allow | reduce | block
    reason TEXT NOT NULL,
    symbol TEXT,
    side TEXT,
    entry_price DOUBLE PRECISION,
    stop_loss DOUBLE PRECISION,
    take_profit DOUBLE PRECISION,
    position_size_before DOUBLE PRECISION,
    position_size_after DOUBLE PRECISION,
    config_snapshot JSONB,
    triggered_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_guard_events_trader_time ON guard_events(trader_id, triggered_at DESC);
CREATE INDEX idx_guard_events_decision ON guard_events(decision_record_id);
CREATE INDEX idx_guard_events_type ON guard_events(guard_type, action, triggered_at DESC);
```

### 4.4 Go 模型

```go
package store

import (
	"encoding/json"
	"time"

	"gorm.io/gorm"
)

// GuardEvent records every rule-check outcome for governance and post-mortem.
type GuardEvent struct {
	ID                 int64           `gorm:"primaryKey;autoIncrement" json:"id"`
	TraderID           string          `gorm:"column:trader_id;not null;index:idx_guard_events_trader_time" json:"trader_id"`
	DecisionRecordID   *int64          `gorm:"column:decision_record_id" json:"decision_record_id"`
	CycleNumber        int             `gorm:"column:cycle_number;not null" json:"cycle_number"`
	StrategyID         string          `gorm:"column:strategy_id" json:"strategy_id"`
	GuardType          string          `gorm:"column:guard_type;not null" json:"guard_type"`
	Action             string          `gorm:"column:action;not null" json:"action"`
	Reason             string          `gorm:"column:reason;not null" json:"reason"`
	Symbol             string          `gorm:"column:symbol" json:"symbol"`
	Side               string          `gorm:"column:side" json:"side"`
	EntryPrice         float64         `gorm:"column:entry_price" json:"entry_price"`
	StopLoss           float64         `gorm:"column:stop_loss" json:"stop_loss"`
	TakeProfit         float64         `gorm:"column:take_profit" json:"take_profit"`
	PositionSizeBefore float64         `gorm:"column:position_size_before" json:"position_size_before"`
	PositionSizeAfter  float64         `gorm:"column:position_size_after" json:"position_size_after"`
	ConfigSnapshot     json.RawMessage `gorm:"column:config_snapshot;type:jsonb" json:"config_snapshot"`
	TriggeredAt        time.Time       `gorm:"column:triggered_at;not null" json:"triggered_at"`
}

func (GuardEvent) TableName() string { return "guard_events" }

type GuardEventStore struct {
	db *gorm.DB
}

func NewGuardEventStore(db *gorm.DB) *GuardEventStore {
	return &GuardEventStore{db: db}
}

func (s *GuardEventStore) initTables() error {
	return s.db.AutoMigrate(&GuardEvent{})
}

// BulkInsert writes a batch of guard events. Should be called after LogDecision.
func (s *GuardEventStore) BulkInsert(events []*GuardEvent) error {
	if len(events) == 0 {
		return nil
	}
	return s.db.CreateInBatches(events, 100).Error
}

// GetRecent returns recent guard events for a trader.
func (s *GuardEventStore) GetRecent(traderID string, limit int) ([]*GuardEvent, error) {
	var out []*GuardEvent
	err := s.db.Where("trader_id = ?", traderID).
		Order("triggered_at DESC").
		Limit(limit).
		Find(&out).Error
	return out, err
}
```

### 4.5 代码层改动

#### `kernel/engine_position.go`

把 `validateDecisions` 的签名改成返回 guard 事件：

```go
func validateDecisions(
	decisions []Decision,
	accountEquity float64,
	btcEthLeverage, altcoinLeverage int,
	btcEthPosRatio, altcoinPosRatio, minRiskRewardRatio float64,
	entryRiskGuard *store.EntryRiskGuardConfig,
	marketDataMap map[string]*market.Data,
	marketPrices map[string]float64,
	minSLDistances map[string]float64,
	strategyID string,
	cycleNumber int,
) (int, []*store.GuardEvent)
```

在 `applyEntryRiskGuard` 内部，每个分支都追加事件：

```go
// 命中 warn_reduce
appendGuardEvent(events, store.GuardEvent{
    GuardType:          "entry_risk_guard",
    Action:             "reduce",
    Reason:             msg,
    Symbol:             d.Symbol,
    Side:               sideFromAction(d.Action),
    EntryPrice:         entryPrice,
    StopLoss:           d.StopLoss,
    TakeProfit:         d.TakeProfit,
    PositionSizeBefore: originalSize,
    PositionSizeAfter:  d.PositionSizeUSD,
    ConfigSnapshot:     cfgSnapshot,
})

// hard block
appendGuardEvent(events, store.GuardEvent{
    GuardType: "entry_risk_guard",
    Action:    "block",
    Reason:    err.Error(),
    // ...
})
```

硬安全规则（杠杆、仓位、SL/TP 方向错误、R:R hard block）用 `guard_type=hard_safety`。

#### `kernel/engine_analysis.go`

`parseFullDecisionResponse` 返回 `[]*store.GuardEvent`：

```go
func parseFullDecisionResponse(
	aiResponse string,
	// ... 原有参数 ...
	strategyID string,
	cycleNumber int,
) (*FullDecision, []*store.GuardEvent, error)
```

在 engine 主流程中，`LogDecision` 成功后，用返回的 `record.ID` 回填 `GuardEvent.DecisionRecordID`，再调用 `guardEventStore.BulkInsert`。

### 4.6 测试

- `store/guard_event_test.go`：插入、批量插入、查询。
- `kernel/validate_test.go`：验证触发 `block`/`reduce` 时产生正确事件字段。
- 验证写入失败不影响主流程（mock 一个会报错的 store）。

### 4.7 验收标准

- [ ] 每次 `entry_risk_guard` 命中都写入 `guard_events`。
- [ ] 硬安全规则（杠杆、仓位、SL/TP 方向错误）写入 `guard_type=hard_safety`。
- [ ] `guard_events` 可通过 `trader_id + time` 查询。
- [ ] 写入失败不阻塞主决策流程。

---

## 五、Phase 2：Prompt 结构化输出改造

### 5.1 目标

让 AI 输出可被代码消费的自检字段，用于**差异分析、复盘归因、prompt 质量评估**，但不作为最终风控依据。

### 5.2 Prompt 改动

在 `kernel/engine_prompt.go` 的系统提示末尾增加固定输出要求（中英双语）：

```text
除了 <reasoning> / <decision> 外，你必须在最后输出 <guard_assessment> 段落，JSON 格式如下：
{
  "market_regime": "trend | range | transition | high_volatility",
  "risk_signals": ["extreme_rsi", "near_support_resistance", "transition_market", "tp_extension"],
  "tp_rationale": {
    "anchor_type": "recent_high_low | boll_band | support_resistance | breakout_extension",
    "anchor_price": 0,
    "breakout_evidence": []
  },
  "ai_self_check": {
    "hard_block_expected": false,
    "override_suggested": false,
    "override_reason": ""
  }
}
```

关键约束：
- `ai_self_check.override_suggested=true` 时，代码仍然**不允许 override hard block**，但会把该请求记录到 `guard_events`。
- 代码只解析、不信任；最终风控仍以代码计算为准。

### 5.3 解析层改动

#### `kernel/engine_analysis.go`

新增提取函数：

```go
var reGuardAssessment = regexp.MustCompile(`(?s)<guard_assessment>(.*?)</guard_assessment>`)

func extractGuardAssessment(response string) string {
	m := reGuardAssessment.FindStringSubmatch(response)
	if len(m) > 1 {
		return strings.TrimSpace(m[1])
	}
	return ""
}
```

`parseFullDecisionResponse` 返回 `GuardAssessment`：

```go
func parseFullDecisionResponse(
	aiResponse string,
	// ... 原有参数 ...
) (*FullDecision, []*store.GuardEvent, error) {
	// ...
	guardAssessment := extractGuardAssessment(aiResponse)
	// ...
	return &FullDecision{
		CoTTrace:          cotTrace,
		CoTSummary:        cotSummary,
		Decisions:         decisions,
		GuardAssessment:   guardAssessment,
	}, events, nil
}
```

#### `kernel/engine.go`

`FullDecision` 增加字段：

```go
type FullDecision struct {
	SystemPrompt      string     `json:"system_prompt"`
	UserPrompt        string     `json:"user_prompt"`
	CoTTrace          string     `json:"cot_trace"`
	CoTSummary        string     `json:"cot_summary"`
	Decisions         []Decision `json:"decisions"`
	GuardAssessment   string     `json:"guard_assessment,omitempty"`
	// ...
}
```

#### `store/decision.go`

- `DecisionRecordDB` / `DecisionRecord` 新增 `GuardAssessment string`。
- `initTables` 中增加：
  ```go
  s.db.Exec(`ALTER TABLE decision_records ADD COLUMN IF NOT EXISTS guard_assessment TEXT DEFAULT ''`)
  ```

### 5.4 遥测消费

在 `guard_events` 表中增加可选字段：

```sql
ALTER TABLE guard_events ADD COLUMN IF NOT EXISTS ai_assessment JSONB;
```

写入时把 AI 的 `ai_self_check` 部分存进去，后续看板对比：
- AI 预计 `hard_block_expected=true` vs 实际 `action=block` → 一致。
- AI 预计 `hard_block_expected=false` vs 实际 `action=block` → 差异样本，用于 prompt 迭代。

### 5.5 测试

- `kernel/engine_analysis_test.go`：解析带 `<guard_assessment>` 的 response。
- 验证缺失段落不报错、不破坏原有决策解析。
- 验证 `guard_assessment` 正确存入 `decision_records`。

### 5.6 验收标准

- [ ] AI response 包含 `<guard_assessment>` 段落。
- [ ] 解析后存入 `decision_records.guard_assessment`。
- [ ] AI 的 `override_suggested=true` 不影响代码最终决策。

---

## 六、Phase 3：候选池 ranking 约束

### 6.1 目标

让 AI 在代码筛选后的候选池内解释、排序，避免全市场自由挑选逆势币。

### 6.2 配置层改动

文件 `store/strategy.go`，在 `CoinSourceConfig` 中新增：

```go
type CandidateRankingFilter struct {
	Enabled          bool `json:"enabled"`
	UsePriceMomentum bool `json:"use_price_momentum"`
	UseOIChange      bool `json:"use_oi_change"`
	UseFundingRate   bool `json:"use_funding_rate"`
	MaxCandidates    int  `json:"max_candidates"` // 默认 10
	Enforce          bool `json:"enforce"`        // true 时 open decision 的 symbol 不在池内会被 block
} `json:"ranking_filter,omitempty"`
```

默认 `Enabled=false`，`MaxCandidates=10`，`Enforce=false`。

### 6.3 数据层改动

文件 `kernel/engine.go`：

- 给 `CandidateCoin` 增加 score：
  ```go
  type CandidateCoin struct {
      Symbol      string             `json:"symbol"`
      Sources     []string           `json:"sources"`
      Score       float64            `json:"score,omitempty"`
      RankFactors map[string]float64 `json:"rank_factors,omitempty"`
  }
  ```
- 在 `GetCandidateCoins()` 返回前，如果 `RankingFilter.Enabled`，调用 `rankCandidateCoins(candidates, filter)`：
  - 复用 `provider/nofxos` 的 `GetPriceRanking`、`GetOIRanking`、FundingRate ranking（如存在）或 `GetCoinDataBatch`。
  - 每个维度打分后归一化到 `[0,1]`，按配置权重合成 `Score`。
  - 按 `Score` 降序截断到 `MaxCandidates`。

简化 scoring 示例：

```go
func scoreCandidate(coin CandidateCoin, price *provider.PriceRankingItem, oi *provider.OIRankingItem, funding float64) CandidateCoin {
	var s float64
	factors := map[string]float64{}
	if price != nil {
		factors["price_momentum"] = normalize(price.ChangePct, -5, 5)
		s += factors["price_momentum"] * 0.4
	}
	if oi != nil {
		factors["oi_change"] = normalize(oi.ChangePct, -2, 2)
		s += factors["oi_change"] * 0.3
	}
	factors["funding_rate"] = normalize(funding, -0.001, 0.001)
	s += factors["funding_rate"] * 0.3

	coin.Score = s
	coin.RankFactors = factors
	return coin
}
```

### 6.4 Prompt 层改动

文件 `kernel/engine_prompt.go`：

- 在 Candidate Coins 段落增加：每个候选币的 `score`、`rank_factors`。
- 明确要求 AI 优先从高分候选中选择；若选择池外币，需在 reasoning 中解释原因。

### 6.5 规则审查层改动

文件 `kernel/engine_position.go`：

- 在 `validateDecision` 中，如果 `RankingFilter.Enforce=true` 且 decision 是 `open_*`，检查 symbol 是否在候选池内。
- 不在池内：记录 `guard_type=candidate_pool`、`action=block`，并把决策转成 `wait`。

### 6.6 前端改动

- `web/src/components/strategy/CoinSourceEditor.tsx` 增加 ranking filter 折叠面板：
  - 开关
  - 启用价格动量 / OI 变化 / 资金费率
  - 最大候选数
  - 是否强制限制在池内
- i18n key：
  - `rankingFilter`
  - `enforceCandidatePool`
  - `maxCandidates`

### 6.7 测试

- `kernel/engine_candidate_test.go`（新建）：构造候选币 + mock ranking，验证排序和截断。
- `kernel/validate_test.go`：验证 `Enforce=true` 时池外币被 block，默认不影响现有行为。

### 6.8 验收标准

- [ ] 候选币按 quant score 排序。
- [ ] `Enforce=true` 时 AI 选池外币被 block。
- [ ] 默认 `Enabled=false` 不影响现有行为。

---

## 七、Phase 4：TP 锚点可信度 soft guard（Phase 1）

> 与 lifecycle plan 的 Task 4 重合，本文档补充与 prompt/遥测的联动。

### 7.1 目标

先用最近 N 根 K 线 high/low、BOLL、ATR 做粗锚点，对明显外推 TP 走 `warn_reduce`。

### 7.2 已有实现

- `kernel/engine_position.go:404-415` 已检测 `BlockExtendedTakeProfit`。
- `recentLowHigh` 已改为最近 20 根 K 线。
- `TakeProfitGuardMode` 已支持 `hard_block` / `warn_reduce`。

### 7.3 需要补充

1. 把命中原因写入 `guard_events`（`guard_type=tp_anchor`，`action=reduce/block`）。
2. 把 AI 在 `<guard_assessment>` 中声明的 `tp_rationale.anchor_type` 与代码判定结果对比：
   - AI 声明 `breakout_extension` 但代码判定为无锚点外推 → 记录 `ai_code_diff=true`。
3. 保持 `BlockExtendedTakeProfit` 开关，默认开启。

### 7.4 后续 Phase 2

- 引入结构位服务（supports/resistances/swing high/low）后，把 `anchor_type` 扩展到 `support_resistance`。
- 有足够证据后再考虑把部分场景从 `warn_reduce` 升级为 `hard_block`。

---

## 八、Phase 5：退出纪律代码化

> 对应 lifecycle plan 中尚未实现的 `trailing_stop` / `time_stop`。这里只列边界和治理要求。

### 8.1 trailing_stop

配置（新增）：

```go
type TrailingStopConfig struct {
	Enabled       bool    `json:"enabled"`
	TriggerPct    float64 `json:"trigger_pct"`    // 浮盈达到多少百分比后启动
	RetractPct    float64 `json:"retract_pct"`    // 从 peak 回撤多少百分比触发保护
	MinProfitLock float64 `json:"min_profit_lock"` // 至少锁定多少利润
}
```

状态：
- 在 `trader.Position` 或 `store.TraderPosition` 增加 `PeakUnrealizedPnLPct`。
- 每轮根据 mark price 更新 peak，触发回撤时执行减仓/平仓。

### 8.2 time_stop

配置（新增）：

```go
type TimeStopConfig struct {
	Enabled     bool `json:"enabled"`
	MaxBars     int  `json:"max_bars"`     // 最多持仓 K 线数
	BarInterval string `json:"bar_interval"` // "15m" | "1h" | "4h"
}
```

逻辑：
- 持仓时间超过 `MaxBars` 且未显著进展（例如未达 peak 浮盈阈值），触发 close review。
- 不直接市价平仓，而是把 decision 的 action 改为 `close_long`/`close_short`，交给 AI/代码共同审查。

### 8.3 治理

- 所有 `trailing_stop` / `time_stop` 触发都写入 `guard_events`（`guard_type=lifecycle_exit`）。
- 方便后续评估：触发后如果不平仓，后续走势如何。

---

## 九、Phase 6：回测 / Paper Trading / 看板

### 9.1 看板指标

基于 `guard_events` + `trader_positions` 计算：

| 指标 | 口径 |
|---|---|
| 命中次数 | 按 `guard_type` + `action` 分组计数 |
| block/reduce/allow 占比 | 同周期内 open decision 的命中分布 |
| 误杀率 | 被 block 后 4h/24h 价格向有利方向移动超过 1R 的比例 |
| 漏杀率 | 未被 block 但最终亏损的交易占比 |
| AI/代码差异率 | `ai_self_check.hard_block_expected` 与实际 `action` 不一致的比例 |
| 参数敏感性 | 同一 guard 在不同阈值下的命中次数对比 |

### 9.2 API

新增或复用接口：

- `GET /api/v1/traders/:id/guard-events?guard_type=&symbol=&action=&from=&to=&limit=`
- `GET /api/v1/traders/:id/guard-stats?window=4h|24h|7d`

### 9.3 前端

- 在“分析页”新增“风控审查” tab，展示：
  - 各 guard 命中次数柱状图。
  - 误杀/漏杀趋势。
  - 最近被 block 的交易列表及后续走势。

---

## 十、文件清单

| 文件 | 改动 |
|---|---|
| `store/guard_event.go` | 新增 |
| `store/decision.go` | 新增 `GuardAssessment` 字段 |
| `kernel/engine_position.go` | 返回 guard events；硬安全/entry/TP/R:R 命中写事件 |
| `kernel/engine_analysis.go` | 解析 `<guard_assessment>`，批量写 guard events |
| `kernel/engine.go` | `FullDecision` 加字段；`CandidateCoin` 加 score；`GetCandidateCoins` 加 ranking |
| `kernel/engine_prompt.go` | 增加 guard assessment 输出要求、候选池 score 展示 |
| `store/strategy.go` | `CoinSourceConfig` 新增 `RankingFilter` |
| `api/handler_guard.go`（或复用现有 handler） | 新增 guard event 查询接口 |
| `web/src/components/strategy/CoinSourceEditor.tsx` | 新增 ranking filter UI |
| `web/src/i18n/translations.ts` | 新增 i18n key |

---

## 十一、每个 PR 必须包含的检查清单

- [ ] 后端单元测试覆盖新逻辑。
- [ ] 前端类型定义已更新。
- [ ] 新增 i18n key（`zh`/`en`/`es` 三语，`es` 可先用英文占位）。
- [ ] 策略配置默认值在 `GetDefaultStrategyConfig()` 中已设置。
- [ ] 向后兼容：旧配置 `nil` 时默认关闭或保持原行为。
- [ ] `go test ./trader/... ./kernel/... ./store/... -count=1` 全绿。
- [ ] `npm run build` 通过。

---

## 十二、风险与注意事项

1. **guard_events 写入不要阻塞主流程**：使用异步或失败仅记日志。
2. **Prompt 改造要渐进**：先要求 AI 输出 `<guard_assessment>`，但不强制；等模型稳定后再强制。
3. **候选池 ranking 不要一开始就 enforce**：先观察几周，确认 score 与后续表现相关后再开启 `Enforce`。
4. **避免过度拟合**：每新增一个 guard，都要在 paper trading 跑至少 50~100 个样本后再全量开启。

---

## 十三、Phase 1~6 完成状态 & 后续迭代

> 更新时间：2026-06-15

| Phase | 状态 | 备注 |
|---|---|---|
| Phase 1 规则命中遥测 | ✅ 已完成 | `guard_events` 表 + 写入 + API + 测试 |
| Phase 2 Prompt 结构化输出 | ✅ 已完成 | `<guard_assessment>` 解析 + `decision_records.guard_assessment` + AI override 告警 |
| Phase 3 候选池 ranking 约束 | ✅ 已完成 | `ranking_filter` + score + enforce + normalize + 方向中性评分 |
| Phase 4 TP 锚点 soft guard | ⚠️ 部分完成 | (1) `TakeProfitGuardMode` + 粗细锚点 + 前端控件 ✅；(2) **AI `tp_rationale.anchor_type` 与代码判定 diff 标记未做** |
| Phase 5 退出纪律代码化 | ❌ 未实现 | `trailing_stop` / `time_stop` 的 struct、DB 字段、执行逻辑、前端控件均未落地 |
| Phase 6 风控看板 | ✅ 已完成（MVP） | `/risk-audit` 页面 + `/guard-stats` + AI agreement + top blocked symbols |

以下事项**不在 Phase 1~6 范围内**，建议作为后续独立任务交给其他同学继续迭代。

### 13.1 看板进阶指标（优先级高）

**目标**：在现有 `/risk-audit` 页面补上误杀率、漏杀率，让运营能判断规则是太严还是太松。

**需要的数据**：
- `guard_events`（block/reduce 事件，含 symbol、side、entry_price、stop_loss、take_profit、triggered_at）
- `trader_positions`（被 block 后实际有没有开仓？盈亏如何？）
- post-block 4h/24h 价格（复用 `market.Data` 或 `market/api_client.go` 的历史 K 线）

**指标口径**：
- 误杀率：被 block/reduce 后，4h/24h 内价格向有利方向移动超过 1R 的事件占比。
- 漏杀率：未被 block 但最终亏损（`realized_pnl < 0`）的交易占比。
- 参数敏感性：同一 guard 在不同阈值下的命中次数对比（可先用配置历史快照 + guard_events 做离线分析）。

**涉及文件**：
- `store/guard_event.go`：新增误杀/漏杀计算函数。
- `api/handler_guard.go`：新增 `/traders/:id/guard-insights` 接口。
- `web/src/pages/RiskAuditPage.tsx`：新增误杀/漏杀卡片和趋势图。

**验收标准**：
- [ ] `/risk-audit` 能看到过去 24h 的误杀率 / 漏杀率。
- [ ] 点击某个被 block 的 symbol 能下钻到该事件后续走势。

### 13.2 `allow` 事件记录开关（优先级中）

**目标**：当前 `guard_events` 只记录 block/reduce。算 allow 占比和漏杀率时，分母需要从 `decision_records` 反推，比较麻烦。

**方案**：在 `EntryRiskGuardConfig` 增加 `TelemetryRecordAllow bool`（默认 `false`，避免数据爆炸）。开启后，每个通过所有 guard 的 open decision 也写入一条 `action=allow` 的 `guard_event`。

**涉及文件**：
- `store/strategy.go`
- `kernel/engine_position.go`
- `store/guard_event.go`

### 13.3 按 guard 类型记录配置快照（优先级中）

**目标**：当前 `guard_events.config_snapshot` 只保存 `EntryRiskGuardConfig`。lifecycle_exit、cooldown、consecutive_loss_brake 等事件需要把对应配置也拍下来，方便复盘。

**方案**：把 `config_snapshot` 从单一 JSON 改成 `{guard_type: config_json}`，或新增 `guard_config JSONB` 字段按类型存。

**涉及文件**：
- `store/guard_event.go`
- `kernel/guard_logger.go`
- `trader/brake.go`、`trader/trend_end_watch.go` 等生命周期 hook

### 13.4 funding rate 排名因子真正落地（优先级中）

**目标**：Phase 3 的 `use_funding_rate` 目前是占位符，后端固定返回 0。

**依赖**：需要 nofxos 提供 funding-rate ranking 端点，或从现有 `market/indicator_engine` 取资金费率数据。

**涉及文件**：
- `provider/nofxos/funding.go`（如新增）
- `kernel/candidate_ranking.go`
- `web/src/components/strategy/CoinSourceEditor.tsx`（启用 checkbox）

### 13.5 策略配置变更审计日志（优先级中）

**目标**：当策略的 `entry_risk_guard`、`risk_control` 等关键配置被修改时，记录谁、何时、改了什么，便于追踪误配置。

**方案**：新增 `strategy_config_audit` 表，保存 `(strategy_id, user_id, changed_at, diff_json)`。

**涉及文件**：
- `store/strategy_audit.go`（新增）
- `api/handler_strategy.go`：在更新策略配置接口里写审计记录。

### 13.6 AI 质量监控告警（优先级低）

**目标**：当 AI 出现系统性偏差时自动告警。

**触发条件**：
- 过去 24h `AI agreement rate` 低于 60%。
- AI `override_suggested=true` 次数突增。
- 某个 symbol 被连续 block 超过 N 次。

**方案**：在 `RiskAuditPage` 加告警 banner，或对接 telegram bot。

### 13.7 Prompt A/B 测试框架（优先级低）

**目标**：对比不同 prompt 版本下的 `guard_events` 和 AI agreement 指标。

**方案**：给 `decision_records` 增加 `prompt_version` 字段，看板支持按版本筛选。

### 13.8 TP 锚点 Phase 2：结构位服务（优先级低）

**目标**：当本地有 support/resistance/swing high/low 服务后，把 `tp_rationale.anchor_type` 扩展到 `support_resistance`，并允许更精确的 hard_block。

**涉及文件**：
- `kernel/engine_position.go`
- `kernel/engine_prompt.go`

---

## 十四、文件清单（完整版）

| 文件 | 改动 |
|---|---|
| `store/guard_event.go` | 新增 / 后续迭代主要扩展点 |
| `store/decision.go` | 新增 `GuardAssessment` 字段 |
| `kernel/engine_position.go` | 返回 guard events；硬安全/entry/TP/R:R 命中写事件 |
| `kernel/engine_analysis.go` | 解析 `<guard_assessment>`，批量写 guard events |
| `kernel/engine.go` | `FullDecision` 加字段；`CandidateCoin` 加 score；`GetCandidateCoins` 加 ranking |
| `kernel/engine_prompt.go` | 增加 guard assessment 输出要求、候选池 score 展示 |
| `store/strategy.go` | `CoinSourceConfig` 新增 `RankingFilter`；`EntryRiskGuardConfig` 新增 TP/R:R 配置 |
| `api/handler_guard.go` | guard event 查询 + guard-stats 聚合接口 |
| `web/src/pages/RiskAuditPage.tsx` | Phase 6 风控看板 |
| `web/src/components/common/HeaderBar.tsx` | Risk Audit 导航入口 |
| `web/src/components/strategy/CoinSourceEditor.tsx` | 新增 ranking filter / TP guard mode / reduce pct UI |
| `web/src/i18n/strategy-translations.ts` | 新增 i18n key |
