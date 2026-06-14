# 策略生命周期优化落地实施方案

> 本文档是对 [`docs/analysis/2026-06-14-strategy-lifecycle-optimization-recommendations.md`](../analysis/2026-06-14-strategy-lifecycle-optimization-recommendations.md) 的 P1/P2 落地细化。
> 已跳过 P0（P0 由核心团队负责并已/将单独实现）。
>
> 落地顺序经过调整，优先做低成本、低风险、见效快的项。

---

## 一、落地顺序

```text
1. 统一 UI/分析页三种收益率口径
2. 方向 + 币种级 consecutive loss brake
3. 趋势末期观望模式最小版
4. TP 外推 soft guard
5. R:R 分层合并进 entry_risk_guard
```

---

## 二、通用约定

- **每个任务一个独立 PR**，按上面的顺序提交，便于回滚和 review。
- **不删除旧字段、不改旧接口行为**，只新增字段/配置。
- 配置字段 JSON 用 `snake_case`，Go struct 用 `CamelCase`。
- 所有 Go 改动必须通过：
  ```bash
  go test ./trader/... ./kernel/... ./store/... -count=1
  ```
- 前端改动需通过：
  ```bash
  npm run lint
  npm run build
  ```
- 新增 i18n key 必须补 `zh` / `en` / `es` 三语（`es` 可先用英文占位）。

---

## 三、Task 1：统一 UI/分析页三种收益率口径

### 目标

让“USDT 浮盈 / 账户净值收益率 / 杠杆价格收益率”同时在持仓列表和账户卡片可见，并明确标注风控口径。

### 后端

文件：`trader/auto_trader_decision.go` 的 `GetPositions()`

- 保留现有 `unrealized_pnl_pct`（它已经是杠杆价格收益率）。
- 新增返回字段：
  - `unrealized_pnl_pct_leveraged`：同 `unrealized_pnl_pct` 的值，显式命名。
  - `unrealized_pnl_pct_account`：**建议由前端计算**（`GetPositions()` 里拿不到 account total equity），后端可暂不实现。

> 说明：账户收益率 = `pos.unrealized_pnl / account.total_equity * 100`，前端已有 `account.total_equity`，直接计算最干净。

### 前端

文件：

- `web/src/types/trading.ts`：给 `Position` 增加 `unrealized_pnl_pct_leveraged?: number`。
- `web/src/pages/TraderDashboardPage.tsx`：持仓表格的 `uPnL` 列改成组合展示：
  - 主显示：`+123.45 USDT`
  - 副显示（小字）：
    - 账户收益率：`+0.12%`
    - 杠杆价格收益率：`+1.64%（风控）`
- 账户卡片：给 `total_pnl_pct` 加 tooltip，说明“账户净值收益率 = 总权益相对初始本金的变动”。
- `web/src/i18n/translations.ts`：新增 key：
  - `uPnLLeveraged`：杠杆价格收益率 / Leveraged Price Yield / Rendimiento Apalancado
  - `uPnLAccount`：账户收益率 / Account Yield / Rendimiento de Cuenta
  - `riskControlYield`：风控口径 / Risk Control Metric / Métrica de Control de Riesgo

### 验收标准

- [ ] 持仓列表每一行同时看到 USDT、账户收益率、杠杆价格收益率。
- [ ] `total_pnl_pct` 有 tooltip 说明。
- [ ] 三种口径与分析文档 `/docs/analysis/...:256` 对应。

---

## 四、Task 2：方向 + 币种级 consecutive loss brake

### 目标

把连续亏损刹车从“全局”扩展到“方向级”和“币种+方向级”，降低误杀。

### 配置层

文件：`store/strategy.go`

在 `ConsecutiveLossBrakeConfig` 中新增：

```go
Scope string `json:"scope"` // "global" | "direction" | "symbol_side"，默认 "global"
```

- `global`：当前行为。
- `direction`：按 `LONG` / `SHORT` 分别计数。
- `symbol_side`：按 `BTCUSDT_LONG` 这种 key 分别计数。

### 状态层

文件：`trader/auto_trader.go`

把：

```go
cooling *CoolingState
```

改成：

```go
coolingStates map[string]*CoolingState // key = scopeKey
```

### 逻辑层

文件：`trader/brake.go`

- 把 `countConsecutiveLosses(trades)` 改成 `countConsecutiveLosses(trades, scope, symbol, side)`。
- 新增 `makeScopeKey(scope, symbol, side) string`。
- `UpdateCoolingState` 增加 `scopeKey string` 参数，操作 `states[scopeKey]`。
- `ApplyConsecutiveLossGate(decisions, states, cfg, traderName)`：对每个 `open_*` 决策，按 `symbol_side` → `direction` → `global` 的顺序检查是否有 active cooling；任一命中即 block。

文件：`trader/auto_trader_loop.go`

在现有刹车调用处：

```go
if cfg := ...RiskControl.ConsecutiveLossBrake; cfg != nil && cfg.Enabled {
    recentTrades, _ := at.store.Position().GetRecentTrades(at.id, 20)
    // 根据 cfg.Scope 计算需要维护的 scopeKey
    // 例如 symbol_side：从 recentTrades 中出现过的 symbol+side 生成 keys
    // 更新每个 key 的 cooling state
    // 最后 ApplyConsecutiveLossGate
}
```

### 前端

文件：`web/src/components/strategy/RiskControlEditor.tsx`

在 consecutive loss brake 区域增加下拉选择：

- 冷却范围：全局 / 按方向 / 按币种+方向

### 测试

文件：`trader/brake_test.go`（新建）

构造 `[]store.RecentTrade`，验证：

- global scope 3 连亏触发冷却。
- direction scope 下 `BTCUSDT_SHORT` 2 连亏只 block SHORT，不 block LONG。
- symbol_side scope 下 `BTCUSDT_SHORT` 冷却不影响 `ETHUSDT_SHORT`。

### 验收标准

- [ ] 3 个 scope 下刹车行为正确。
- [ ] 切换 scope 时已有 cooling state 被清空或按新 scope 重新计算（建议重启 trader 后生效）。

---

## 五、Task 3：趋势末期观望模式最小版

### 目标

当“最近 N 笔同方向交易都没触及 TP 就平仓”时，暂停该方向/币种的新开仓。

### 配置层

文件：`store/strategy.go`

新增配置（放在 `RiskControlConfig` 下）：

```go
TrendEndWatch *TrendEndWatchConfig `json:"trend_end_watch,omitempty"`

type TrendEndWatchConfig struct {
    Enabled        bool   `json:"enabled"`
    Misses         int    `json:"misses"`           // 连续未达 TP 次数，默认 3
    CoolDownCycles int    `json:"cool_down_cycles"` // 冷却轮次，默认 3
    Scope          string `json:"scope"`            // "direction" | "symbol_side"，默认 "direction"
}
```

### 数据层

- 复用 `store.Position().GetClosedPositions(at.id, limit)`，因为 `HistoricalPosition` 有 `close_reason`。
- “未达 TP”判定：`close_reason != "take_profit"` 且 `close_reason != ""`。

### 状态层

文件：`trader/auto_trader.go`

新增：

```go
missCooldown map[string]int // key = scopeKey，value = 剩余冷却轮次
```

### 逻辑层

文件：新建 `trader/trend_end_watch.go`

函数：

```go
func UpdateMissCooldown(
    states map[string]int,
    closedPositions []store.HistoricalPosition,
    cfg *store.TrendEndWatchConfig,
) (updated map[string]int, blockedKeys map[string]bool)
```

- 对 `direction` 或 `symbol_side` 分别统计最近 N 笔是否都 miss。
- 命中则设置 `CoolDownCycles`。
- 每轮调用时所有现存 key 的剩余轮次 `-1`，到 0 移除。

文件：`trader/auto_trader_loop.go`

在 consecutive loss brake 之后加入：

```go
if cfg := ...RiskControl.TrendEndWatch; cfg != nil && cfg.Enabled {
    closed, _ := at.store.Position().GetClosedPositions(at.id, 20)
    at.missCooldown = UpdateMissCooldown(at.missCooldown, closed, cfg)
    sortedDecisions = ApplyTrendEndWatchGate(sortedDecisions, at.missCooldown, cfg, at.name)
}
```

`ApplyTrendEndWatchGate` 与 `ApplyConsecutiveLossGate` 类似，只 block `open_*`。

### 前端

文件：`web/src/components/strategy/RiskControlEditor.tsx`

新增“趋势末期观望”折叠面板：

- 开关
- 连续未达 TP 次数
- 冷却轮次
- 范围：按方向 / 按币种+方向

### 测试

文件：`trader/trend_end_watch_test.go`（新建）

构造 `[]store.HistoricalPosition`，覆盖 `close_reason` 为 `stop_loss`、`manual`、`take_profit`：

- 3 连 miss 后触发冷却。
- 只 block 对应方向。

### 验收标准

- [ ] 同方向最近 N 笔均未达 TP 时，该方向新开仓被 block。
- [ ] 已有持仓的平仓/管理不受影响。

---

## 六、Task 4：TP 外推 soft guard

### 目标

把 TP 设在近期结构位之外时，默认走 `warn_reduce`，而不是硬拦截；同时给 reasoning 打上标记。

### 配置层

文件：`store/strategy.go`

在 `EntryRiskGuardConfig` 中新增：

```go
TakeProfitGuardMode string `json:"take_profit_guard_mode"` // "hard_block" | "warn_reduce"，默认 "warn_reduce"
```

### 逻辑层

文件：`kernel/engine_position.go`

当前 `evaluateEntryRiskGuard` 已经会检测 `BlockExtendedTakeProfit`。修改 `applyEntryRiskGuard`：

- 遍历 reasons 时，识别出 TP 外推相关的 reason（可判断字符串包含 `TP`）。
- 如果 `TakeProfitGuardMode == "warn_reduce"`，则只执行降仓。
- 如果 `"hard_block"`，则返回 error。
- 在 `d.Reasoning` 前追加 `[TP_EXTENSION_GUARD]` 标记，方便后续复盘。

> `recentLowHigh` 已改为最近 20 根 K 线（之前的 commit 已完成），这里直接复用。

### 前端

文件：`web/src/components/strategy/RiskControlEditor.tsx`

在 `entry_risk_guard` 面板里，给“TP 结构位外推保护”加一行小字说明：

> 默认“提示并降仓”，不会直接拒绝开仓。

### 测试

文件：`kernel/validate_test.go`

- TP 外推 + `TakeProfitGuardMode=warn_reduce` 时，`PositionSizeUSD` 被降低但决策保留。
- TP 外推 + `TakeProfitGuardMode=hard_block` 时，决策被转成 wait。

### 验收标准

- [ ] TP 超出最近 20 根 K 线高低点 + ATR 容差时，默认降仓而不是拒绝。
- [ ] 配置可切换为硬拦截。

---

## 七、Task 5：R:R 分层合并进 entry_risk_guard

### 目标

把当前“0.8 倍 hard floor”的魔法数字，改成 entry guard 里的可配置 soft/hard 分层。

### 配置层

文件：`store/strategy.go`

在 `EntryRiskGuardConfig` 中新增：

```go
BlockLowRiskReward  bool    `json:"block_low_risk_reward"`   // 默认 true
RiskRewardSoftFloor float64 `json:"risk_reward_soft_floor"`  // 默认 0.8
```

### 逻辑层

文件：`kernel/engine_position.go`

当前 `validateDecision` 中 R:R 检查：

```go
hardFloor := minRiskRewardRatio * 0.8
if riskRewardRatio < hardFloor { ... }
```

改成：

```go
if entryRiskGuard != nil && entryRiskGuard.Enabled && entryRiskGuard.BlockLowRiskReward {
    softFloor := entryRiskGuard.RiskRewardSoftFloor
    if softFloor <= 0 { softFloor = 0.8 }
    hardFloor := minRiskRewardRatio * softFloor

    if riskRewardRatio < hardFloor {
        return fmt.Errorf("risk/reward ratio too low ...") // 硬拦截
    }
    if riskRewardRatio < minRiskRewardRatio {
        // soft tier：降仓并保留
        return applyWarnReduce(d, entryRiskGuard, fmt.Sprintf(
            "R/R %.2f below target %.1f:1 (soft floor %.1f:1)",
            riskRewardRatio, minRiskRewardRatio, hardFloor,
        ))
    }
} else {
    // 兼容旧逻辑
    hardFloor := minRiskRewardRatio * 0.8
    if riskRewardRatio < hardFloor { ... }
}
```

把 `applyEntryRiskGuard` 里的降仓逻辑抽成公共函数 `applyWarnReduce(d, cfg, msg)`，供这里复用。

### 前端

文件：`web/src/components/strategy/RiskControlEditor.tsx`

在 `entry_risk_guard` 面板增加：

- 复选框：低 R:R 保护
- 数字输入：R:R soft floor（默认 0.8，范围 0.5-1.0）

### 测试

文件：`kernel/validate_test.go`

- R:R = 1.2，min = 1.5，soft floor = 0.8 → 触发 warn_reduce，size 降低。
- R/R = 0.9，min = 1.5，soft floor = 0.8 → 硬拦截。
- `BlockLowRiskReward=false` 时回到旧 hard floor 行为。

### 验收标准

- [ ] R:R 不达标但高于 soft floor 时，走 warn_reduce。
- [ ] R:R 低于 soft floor 时，仍硬拦截。
- [ ] 不开启 `BlockLowRiskReward` 时，保持原有行为。

---

## 八、每个 PR 必须包含的检查清单

- [ ] 后端单元测试覆盖新逻辑。
- [ ] 前端类型定义已更新。
- [ ] 新增 i18n key（zh/en/es 三语，es 可先用英文占位）。
- [ ] 策略配置默认值在 `GetDefaultStrategyConfig()` 中已设置。
- [ ] 向后兼容：旧配置 nil 时默认关闭或保持原行为。
- [ ] `go test ./trader/... ./kernel/... ./store/...` 全绿。
- [ ] `npm run build` 通过。

---

## 九、备注

- Task 4、Task 5 都会改动 `kernel/engine_position.go`，建议 Task 5 在 Task 4 合并后再开分支，避免冲突。
- 所有新配置对已有策略默认关闭（entry_risk_guard 已通过 backward-compat 逻辑处理），不会触发惊喜行为变更。
- 如遇不明确字段命名或默认值，优先与 `store/strategy.go` 中现有 `BreakevenProtectionConfig` / `EntryRiskGuardConfig` 保持一致。
