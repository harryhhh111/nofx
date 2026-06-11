# Breakeven Protection 设计文档

**日期**: 2026-06-10（修订 2026-06-11）
**状态**: 已修订,待实施

---

## 一、背景与目标

### 1.1 现状

当前系统的"浮盈管理"完全依赖两条路径:

1. **AI 在 CoT 中主动平仓**(L2 认知层) — 受 prompt 措辞、模型质量、注意力窗口限制,执行不一致
2. **DrawdownClose**(L1 代码层) — 单笔浮盈 ≥ `DrawdownCloseMinProfitPct`(默认 5%)**且**从 MFE 回撤 ≥ `DrawdownCloseTriggerPct`(默认 40%)→ 强制平仓

**缺失的中间档**:浮盈已达可观水平、但还未触发 DrawdownClose 时,**没有任何自动保护**。止损位钉在开仓时的设置点,价格一旦回落,保护效果与开仓时一致——浮盈全部吐回去。

### 1.2 问题

典型场景:浮盈 8% 时未达 DrawdownClose 阈值(需 ≥ 5% + MFE 回撤 40%),价格快速反转,回到 0% 浮盈,继续下行到 -3%,最终止损在 -5%。整段 8% 浮盈在反转中蒸发,本可"打平"出场。

### 1.3 目标

新增一条**可累进的 breakeven 保护**:浮盈每跨过一个 `trigger_pct` 阈值(默认 1%,口径与现有 `DrawdownClose` 的杠杆收益率一致),自动把止损位**沿价格有利方向**推进一格。第 1 步把 SL 推到 entryPrice(保本),后续每步锁定一档浮盈。SL **永不后退**,最终让该笔仓位最坏以"打平"或"锁定部分浮盈"出场。

示例(`trigger_pct=1%`,long,口径为 leveraged PnL = `priceMovePct × leverage`):
- 浮盈 ≥ 1% → SL 移到 `entryPrice`(保本)
- 浮盈 ≥ 2% → SL 移到 `entryPrice + 1 个 step`
- 浮盈 ≥ 3% → SL 移到 `entryPrice + 2 个 step`
- 浮盈回落到 1.5% → SL 仍停在 `entryPrice + 1 个 step`,**不退**

> 上述"1%/2%/3% 浮盈"对应不同杠杆下的价格变动不同:1x 杠杆时即价格涨 1%/2%/3%,10x 杠杆时即价格涨 0.1%/0.2%/0.3%,100x 时即 0.01%/0.02%/0.03%。详见 §2.1 `priceStep` 推导。

### 1.4 非目标

- **不做"距离 MFE 的 trailing"**(SL 不随浮盈实时上下浮动,只前进不后退)
- **不与 AI 交互**(纯代码强制,不在 prompt 中显式提及)
- **不干预 AI 后续的平仓决策**(AI 仍可自由选择 CLOSE;BE 保护只动交易所的 conditional stop loss order)
- **不区分开仓原因**(与 ExitStandards、ConsecutiveLossBrake 互不感知)

---

## 二、设计

### 2.1 触发条件

对**每笔** open position,每个检查周期计算:

```
unrealizedPnL      = (markPrice - entryPrice) * qty * sideSign
positionValue      = entryPrice * qty                 // 仓位名义价值(未乘杠杆)
marginValue        = positionValue / leverage         // 近似保证金
unrealizedPnLPct   = unrealizedPnL / marginValue * 100
                  = priceMovePct * leverage           // 与现有 DrawdownClose 口径一致

targetSteps   = floor(unrealizedPnLPct / triggerPct + epsilon)   // 应该已推进到第几步
currentSteps  = at.breakevenSteps[posKey]                       // 已推进到第几步(0 = 未触发)

其中 `epsilon = triggerPct * 1e-6`,用于防止浮点运算导致 `unrealizedPnLPct` 在数学上恰为 1.0 但被计算为 0.999999 时被 `floor` 错误地截断为 0。
```

**当 `targetSteps > currentSteps` 时,执行推进**:
- **Phase 1 限制每周期最多推进 1 步**,避免重启或跳空行情下瞬间产生数十次 API 调用触碰交易所 rate limit。若 `targetSteps - currentSteps >= 2`,本次只推进 1 步,剩余步数由后续 ticker 逐分钟追赶
- 每次推进:`nextStep = currentSteps + 1`
- 推进的目标 SL 价:
  - long:  `entryPrice + (nextStep - 1) * priceStep`
  - short: `entryPrice - (nextStep - 1) * priceStep`
  - 其中 `priceStep = entryPrice * (triggerPct / 100) / leverage`
  - `nextStep=1` 时 `(nextStep - 1)=0`,所以 SL 正好移到 entryPrice(保本)

#### `priceStep` 公式推导

`trigger_pct` 描述的是"leveraged PnL 跨过多少才推进一格",而 `priceStep` 是把"leveraged PnL 增量"换算到"价格步长":

```
leveraged_PnL% = priceMove% × leverage
1 step 的 leveraged PnL% = trigger_pct / 100
1 step 的 priceMove%    = (trigger_pct / 100) / leverage
priceStep                = entryPrice × (trigger_pct / 100) / leverage
```

示例:`trigger_pct=1%`,10x 杠杆,`entryPrice = 100_000`:
- `priceStep = 100_000 × 0.01 / 10 = 100`
- 价格涨到 100_100(0.1% 变动)→ leveraged PnL = 1% → 推一格 → SL 移到 100_000
- 价格涨到 100_200(0.2% 变动)→ leveraged PnL = 2% → 再推一格 → SL 移到 100_100

#### 分母选择

- **使用现有系统的杠杆收益率口径**,与 `trader/auto_trader_risk.go` 中 DrawdownClose 的 `currentPnLPct = priceMovePct * leverage` 保持一致
- 语义:浮盈达到"占保证金的 X%"时触发,而不是占账户权益或仓位名义价值的 X%
- 示例:10x 杠杆,仓位名义价值 100,000 USD,保证金约 10,000 USD。`trigger_pct=1%` 表示浮盈 100 USD,对应标的价格约上涨 0.1%
- 这个口径与 UI/日志里的 `PnL%`、`Peak PnL%` 一致,避免同一持仓出现两套收益率定义

#### "永不后退"

- SL 只通过 `nextStep = currentSteps + 1` 推进,**永远不减少**
- 即使浮盈从 5% 跌回 1%,SL 仍停在第 5 步的位置(long: `entryPrice + 4 * priceStep`)
- `targetSteps` 只用作"是否触发新推进"的判断,不写回内存(避免浮盈回调时把步数减回去)

### 2.2 执行动作

每次推进:

1. 读取当前交易所 active SL(通过 `trader.GetOpenOrders(symbol)` 取回,按 `Type` + side 过滤,见下文),得到 `oldSL`。若当前无 active SL(long: 用 `0`;short: 用 `math.MaxFloat64`)作为 fallback,使安全校验自然通过
2. 计算 `newSLPrice`
3. 安全校验:
   - long: `oldSL <= newSLPrice < markPrice - safetyBuffer`
   - short: `oldSL >= newSLPrice > markPrice + safetyBuffer`
   - 若 `newSLPrice` 已越过当前价或会让 SL 后退 → 跳过并记录日志

`safetyBuffer` 暂定义为"`priceStep × 0.5`"(半个步长),**不是交易所 tickSize**。原因:BE 的语义是"价格朝有利方向移动一个 step 时推进",buffer 与 step 同一量级能确保新 SL 既不越过当前价、也不比 oldSL 更差。Phase 1 简单且自洽;Phase 2 若需要更精确的边界,可从交易所 symbol info 拉取 tickSize 后用 `max(tickSize, priceStep × 0.5)`。
4. 修改交易所 SL:
   - Phase 1 使用当前接口可确定支持的路径:`CancelStopLossOrders(symbol)` → `SetStopLoss(...)`
   - 该路径在 cancel 和 set 之间有短暂无保护窗口(通常 < 200ms),若闪崩落在此窗口内则无 SL 保护;这是 Phase 1 接受的实现风险
   - 因 `CancelStopLossOrders` 是 symbol 级别,Phase 1 已要求 hedge 同币双向持仓跳过 BE(见 §2.2.1)
   - Phase 2 若要消除保护窗口,应新增 side/order-id aware replace/cancel 能力,或使用交易所原生 amend/replace API
5. 调用 `trader.SetStopLoss(symbol, strings.ToUpper(side), qty, newSLPrice)` 在交易所下新的 SL(接口要求 `positionSide` 为大写 `"LONG"`/`"SHORT"`)
6. `currentSteps = nextStep` 写回内存(仅在下单成功后写回;失败则下个 ticker 重试)
7. 写一条 `decision_actions` 记录,`action=update_stop_loss`,`reasoning` 中包含 `reason=breakeven_protection, from=oldSL, to=newSLPrice, step=N`
8. 日志:`🛡️ [trader] BE promotion step 3: BTCUSDT LONG 浮盈 3.20% (≥ 3.00%),SL 60100.00 → 60200.00`

> 注:`DecisionAction` 当前没有结构化 `from/to/step` 字段,先写入 `Reasoning`。若后续需要报表统计,再单独扩展 schema。

### 2.2.1 SL 撤单接口约束

当前 trader interface 是:

```go
CancelStopLossOrders(symbol string) error
```

它**不带 position side**。因此 BE 实现不能直接假设可以只撤某一侧 SL:

- 对大多数单向持仓场景,按 symbol 撤 SL 后立刻重挂是可接受的
- 若同一 symbol 同时存在 long/short 两侧仓位,按 symbol 撤单可能误撤另一侧 SL
- Phase 1 要求:
  - **BE 循环开始前**,先扫描所有 open position 构建 `map[symbol]int` 计数。若某 symbol 的 count ≥ 2(即同时存在 long 和 short 仓位)→ **跳过该 symbol 所有 position 的 BE promotion**,避免 symbol 级撤单误伤另一侧 SL
  - 日志提示:需要 side-aware cancel 才能支持 hedge 同币双向持仓
- Phase 2 可选升级:
  - 新增 `CancelStopLossOrdersForSide(symbol, positionSide string)` 或扩展现有接口
  - 各 adapter 按交易所能力筛选 side/order id 后撤单

### 2.2.2 Active SL 识别规则

`trader.GetOpenOrders(symbol)` 的 `OpenOrder.PositionSide` 并非所有交易所都会填充(例如 Bybit/Gate 可能为空),因此不能强依赖 `PositionSide`。Phase 1 使用以下规则识别当前 position 对应的 SL:

1. 先按 `Type` 过滤:
   - SL 白名单:`STOP_MARKET`、`STOP`、`STOP_LIMIT`、`STOPLOSS`
   - 显式排除:`TAKE_PROFIT`、`TAKE_PROFIT_MARKET`、`TAKE_PROFIT_LIMIT` 以及任何 `Type` 包含 `TAKE_PROFIT` 的订单
2. 再按 side 过滤:
   - 若 `PositionSide` 非空:必须等于当前持仓方向(`LONG`/`SHORT`)
   - 若 `PositionSide` 为空:使用 `Side` 推断
     - long position 的 SL 通常是 `SELL`
     - short position 的 SL 通常是 `BUY`
3. 若仍匹配到多条 SL:
   - long:取 `StopPrice` 最接近且不高于当前 `markPrice` 的订单
   - short:取 `StopPrice` 最接近且不低于当前 `markPrice` 的订单
   - 若无法判断,跳过该 position 的 BE promotion 并记录日志

### 2.3 状态机

每笔 position 在内存中跟踪"已推进步数":

```
Steps = 0  ──[浮盈 ≥ 1×step]──▶  Steps = 1   (SL at cost)
Steps = 1  ──[浮盈 ≥ 2×step]──▶  Steps = 2   (SL at cost + 1 step)
Steps = 2  ──[浮盈 ≥ 3×step]──▶  Steps = 3   (SL at cost + 2 step)
   ...
Steps = N  ──[浮盈 ≥ (N+1)×step]──▶  Steps = N+1

[浮盈回落] ──▶  步数不变,SL 不动
```

- 状态用**内存字段**记录:`at.breakevenSteps map[string]int`(`posKey = symbol_side`),`0` = 未触发
- 仓位平仓时(任何原因)清理该 key
- **不持久化** — trader 重启后从交易所 active SL 推断当前有效步数,再继续向前推进。详见 §五。

### 2.4 检查频率

复用 `startDrawdownMonitor` 现有的 1 分钟 ticker(`trader/auto_trader_risk.go:14-34`),不新增 ticker。

`checkPositionDrawdown` 内部每分钟迭代所有 open position,BE 检查作为同一个循环的"先做"步骤。

---

## 三、与现有系统的关系

### 3.1 与 DrawdownClose

```
浮盈:           0% ─── 1% ─── 2% ─── 3% ─── 4% ─── 5% ────── 100%
                 │      │      │      │      │       │           │
BE step:         │    [1]    [2]    [3]    [4]     [5]          │
                 │     ↓      ↓      ↓      ↓       ↓           │
                 │   cost  cost+1  cost+2  cost+3  cost+4  ...  │
                 │                                                  │
DrawdownClose:   │       (需 MFE 回撤 40%,独立于 BE step)         │
```

- **不冲突**:DrawdownClose 是"从 MFE 峰值回撤"为度量的,与 BE 的"从 cost 推进"度量正交
- **可能同时触发**:浮盈涨到 5%、MFE 5%,DrawdownClose 不触发(回撤 0%);价格回落到 3%,MFE 仍是 5%,回撤 40% = 3%,DrawdownClose 触发
- **执行顺序**:`checkBreakevenPromotion` 先于 `DrawdownClose` 跑(同 ticker 内),让 BE step 反映"本分钟最新浮盈"
- **互补**:BE 保住"逐级累进的最小利润",DrawdownClose 保住"峰值后的大幅回撤"

### 3.2 与 ConsecutiveLossBrake

BE 触发后,价格可能回落到成本价附近被甩出,产生**小额亏损**(-0.1% ~ 0% 区间,取决于交易所滑点)。这个亏损**正常计入** ConsecutiveLossBrake 计数:

- 这是**有意为之的简单性**:不在平仓记录里加特判标签
- 风险:震荡市反复触发 BE → 反复打平出场 → 累计 3 笔 -0.X% → 触发 3 cycle 冷却
- **观察窗口**:上线后跑 2-4 周,统计 BE 触发后的平仓 PnL 分布,再决定是否需要 B 路径(特判)

### 3.3 与 ExitStandards (prompt)

**互不感知**:
- ExitStandards 是 prompt 内容,告诉 AI "什么时候该考虑平仓"
- BE 保护是代码层,直接改交易所的 SL order
- AI 在 CoT 里看不到 BE 是否已触发,但**也不需要看到**——它的决策空间不受影响(可自由 CLOSE,可自由 HOLD)
- **不在 ExitStandards 中加 "trend healthy → may HOLD, system will BE-protect you" 的引导**——避免 AI 产生"反正有保护我就不管了"的依赖

### 3.4 与 AI 平仓决策

- AI 仍可在浮盈达 1% 后**主动 CLOSE**(直接市价平仓,不走 SL)
- AI 仍可 HOLD 等待更高利润
- BE 不影响 AI 的 CoT 决策,**只**在交易所侧改 SL order
- 若 AI 在 BE 触发**之前**就平仓 → BE 保护不触发(仓位已不存在)

---

## 四、配置

### 4.1 RiskControl 新增字段

`store/strategy.go` 在 `RiskControlConfig` 内追加:

```go
// BreakevenProtection: progressive SL promotion as float profit increases.
// Each trigger_pct of float profit pushes the SL forward by one step (in
// the price-favorable direction). SL never retreats.
// nil = off for backward compatibility; default strategy templates set Enabled=true.
BreakevenProtection *BreakevenProtectionConfig `json:"breakeven_protection,omitempty"`
```

`BreakevenProtectionConfig`:

```go
type BreakevenProtectionConfig struct {
    Enabled    bool    `json:"enabled"`     // Enable (default: true)
    TriggerPct float64 `json:"trigger_pct"` // Step size: each N% float profit
                                            // advances the SL by one step.
                                            // Default: 1.0 (i.e. 1% leveraged PnL)
    // Notes:
    // - trigger_pct uses leveraged PnL%, same as DrawdownClose/currentPnLPct:
    //   price_move_pct * leverage, equivalent to unrealized PnL / margin.
    // - Progressive: SL is pushed forward every trigger_pct, but never
    //   retreats. First step moves SL to entry price (breakeven);
    //   subsequent steps lock in incremental profit.
}
```

### 4.2 默认值

`GetDefaultStrategyConfig` 中默认开启:

```go
BreakevenProtection: &BreakevenProtectionConfig{
    Enabled:    true,
    TriggerPct: 1.0,
},
```

为了避免历史策略在升级后静默改变行为,已存在策略若 `breakeven_protection` 为 `nil` 则保持关闭。用户保存/新建默认策略时才会带上上述默认值。

### 4.3 ClampLimits

```go
if c.RiskControl.BreakevenProtection != nil {
    bp := c.RiskControl.BreakevenProtection
    if bp.TriggerPct < 0.1 {
        bp.TriggerPct = 0.1   // 防止阈值过低导致频繁触发
    }
    if bp.TriggerPct > 20.0 {
        bp.TriggerPct = 20.0  // 防止阈值过高导致形同虚设
    }
}
```

### 4.4 EstimateTokens

无影响(配置项不进入 prompt)。

---

## 五、持久化与重启

**设计选择:不持久化 BE 状态,但每轮从交易所 active SL 推断有效步数**

- 内存 map `at.breakevenSteps` 仅在 trader 进程内存中
- trader 重启后,该 map 清空
- 重启后第一次检查时,必须读取 active SL,反推 `inferredSteps`:
  - 通过 `trader.GetOpenOrders(symbol)` 获取所有挂单
  - **必须按 §2.2.2 的 active SL 识别规则过滤**:优先使用 `PositionSide`,为空时用 `Side` 推断;同时严格排除 `TAKE_PROFIT*`,避免将 TP 价格误读为 SL 后把 SL 推得过高
  - 若有多条 SL 订单(罕见),取最接近 entryPrice 的那条作为 `oldSL`
  - long:  `inferredSteps = max(0, floor((oldSL - entryPrice) / priceStep) + 1)`
    - 旧 SL 在 entryPrice → inferredSteps=1
    - 旧 SL 高于 entryPrice 2 个 step → inferredSteps=3
    - 旧 SL 低于 entryPrice(用户调近过 SL)→ inferredSteps=0,等价于"未触发过"
  - short: `inferredSteps = max(0, floor((entryPrice - oldSL) / priceStep) + 1)`
  - 若读不到 active SL(如 `trader` 接口不支持,或 symbol 还没挂 SL)→ `inferredSteps = 0`
- `effectiveSteps = max(memorySteps, inferredSteps)`——取**保守值**(宁可少推一格,不让 BE 跨过用户意图)
- 后续推进从 `effectiveSteps` 起步,继续用 §2.2 的 `oldSL <= newSLPrice`/`oldSL >= newSLPrice` 二次保护
- 为什么不持久化:
  - BE 是"可重入的保险",不是"决策状态"
  - 持久化会带来:重启时如何恢复(从 DB 读最近一条 update_stop_loss action?)、并发竞态(DB 写入与交易所下单)、状态污染(如果 SL 已被外部调整过)
  - 收益小,风险大

---

## 六、代码改动清单

### 6.1 `store/strategy.go`

- [ ] `RiskControlConfig` 新增 `BreakevenProtection *BreakevenProtectionConfig`
- [ ] 新增 `BreakevenProtectionConfig` struct
- [ ] `GetDefaultStrategyConfig()` 中填入默认配置(`Enabled: true, TriggerPct: 1.0`)
- [ ] `ClampLimits()` 中加入 TriggerPct 范围约束
- [ ] `EstimateTokens()` **无需改动**(配置项不入 prompt)

### 6.2 `trader/auto_trader.go`

- [ ] `AutoTrader` struct 新增 `breakevenSteps map[string]int` 字段(`posKey → 已推进步数,0=未触发`)
- [ ] `NewAutoTrader` 中初始化该 map
- [ ] 在 position close / clean up 路径上清理该 map 的对应 key
- [ ] 在 `breakevenSteps` 的所有读写路径上加 `sync.Mutex` 或复用现有锁(参考 `peakPnLCacheMutex` 模式)

### 6.3 `trader/auto_trader_risk.go`

- [ ] 新增 `checkBreakevenPromotion(positions []map[string]interface{})` 函数
- [ ] 在 `checkPositionDrawdown` 同一 ticker 循环开头调用 BE 检查
- [ ] 函数内:
  1. **hedge 检测**:扫描所有 position,构建 `map[symbol]int`——若任一 symbol 的 count ≥ 2 → 该 symbol **所有** position 跳过 BE(避免 symbol 级撤单误伤)
  2. **exchange guard**:`if at.exchange == "hyperliquid" || at.exchange == "lighter" { return }`(因 `CancelStopLossOrders` 会误撤 TP)
  3. 对每笔 position 读 `memorySteps := at.breakevenSteps[posKey]`(`0` if missing)
  4. 通过 `trader.GetOpenOrders(symbol)` 读取 active SL,按 §2.2.2 过滤并选择目标 SL,推断 `inferredSteps`;取 `currentSteps := max(memorySteps, inferredSteps)`
  5. 计算 `targetSteps := floor(unrealizedPnLPct / triggerPct + epsilon)`(其中 `epsilon = triggerPct * 1e-6`)
  6. 若 `targetSteps > currentSteps`:**Phase 1 限制每 ticker 最多 1 步**,执行一次安全校验 + `CancelStopLossOrders(symbol)` + `SetStopLoss`,成功后 `currentSteps++` 写回内存;剩余步数后续 ticker 追赶
  7. 同步清理:`emergencyClosePosition` / `ClearPeakPnLCache` / AI 主动 CLOSE 路径,都需 `delete(at.breakevenSteps, posKey)`
- [ ] `qty` 取值:使用 `pos["positionAmt"].(float64)` 并用 `math.Abs()` 取正(与现有 DrawdownClose 代码一致)
- [ ] `priceStep` 计算:`priceStep = entryPrice * (triggerPct / 100) / leverage`
- [ ] `newSLPrice` 计算:
  - long: `entryPrice + (nextStep - 1) * priceStep`
  - short:`entryPrice - (nextStep - 1) * priceStep`
- [ ] 下单前校验 `newSLPrice` 不后退(`oldSL <= newSLPrice`/`oldSL >= newSLPrice`),且不会因越过当前价格而立即触发(`newSL < markPrice - safetyBuffer`/`newSL > markPrice + safetyBuffer`)
- [ ] 无 active SL 时的 `oldSL` fallback:long 用 `0`,short 用 `math.MaxFloat64`

### 6.4 `trader/types/interface.go` / 各交易所 adapter

`SetStopLoss` 在所有支持的交易所(OKX、Bybit、Binance、Hyperliquid、Gate、Bitget、KuCoin、Lighter、Aster、Paper)均已实现,Phase 1 可复用。

`CancelStopLossOrders` 当前是 symbol 级别,不是 side 级别。Phase 1 不改接口,但需要在 BE 中检测 hedge 同币双向仓位并跳过。Phase 2 若要完整支持双向持仓,需要新增 side-aware cancel 接口。

特别注意:
- Hyperliquid/Lighter 当前无法区分 SL/TP,`CancelStopLossOrders` 会撤掉该 symbol 的所有 stop orders,包括 TP。BE 对这两个交易所默认应跳过。实现方式:在 `checkBreakevenPromotion` 入口处检查 `at.exchange == "hyperliquid" || at.exchange == "lighter"`,若匹配则直接 return,打日志提示。后续 adapter 支持精确撤单后再移除该 guard。
- Indodax(spot-only)无 SetStopLoss;spot 模式不参与 AI 合约自动交易,跳过即可(通过检查 trader 是否实现 `SetStopLoss` 接口或 provider 白名单)。

### 6.5 `web/src/types/strategy.ts`

- [ ] `RiskControlConfig` 新增 `breakeven_protection?: { enabled: boolean; trigger_pct: number }`

### 6.6 `web/src/i18n/strategy-translations.ts`

- [ ] `riskControl` 命名空间新增:
  - `breakevenProtection` { zh: '保本止损', en: 'Breakeven Protection' }
  - `breakevenProtectionDesc` { zh: '每达阈值浮盈推进一格 SL,永不后退', en: 'Push SL forward by one step per trigger threshold. Never retreats.' }
  - `breakevenProtectionTriggerPct` { zh: '每步触发浮盈(杠杆收益率 %)', en: 'Step trigger (leveraged PnL %)' }

### 6.7 `web/src/components/strategy/RiskControlEditor.tsx`

- [ ] 在 ConsecutiveLossBrake 区块之前(逻辑上更基础)新增 BE 控件
- [ ] 开关 + slider(0.1% - 20%)

---

## 七、测试

### 7.1 单元测试

- [ ] `BreakevenProtectionConfig.ClampLimits()` 边界值测试
- [ ] `checkBreakevenPromotion` 状态转换测试(用 mock trader):
  - 浮盈 0.5% → steps 不变
  - 浮盈 1.0% → steps=1(SL 移到 entryPrice)
  - 浮盈 0.999%(浮点边界) → steps 不变(epsilon 防止误判)
  - 浮盈 2.5%(已有 steps=1)→ **每 ticker 只推 1 步**,本 ticker:steps 1→2,剩余步数需后续 ticker
  - 浮盈从 3% 跌回 1.5% → steps 仍为 2(SL 不退)
  - 浮盈从 2.5% 涨到 5% → 逐 ticker 追赶:第一分钟 steps 2→3,第二分钟 steps 3→4,第三分钟 steps 4→5
  - 浮盈从 0.9% 跳到 4.1% → 第一分钟 steps 0→1,后续逐分钟追到 step 4(共需 4 分钟)
  - active SL 已在 step3、内存 steps=0(重启场景) → inferredSteps=3,不得把 SL 降回 entry
  - `GetOpenOrders` 同时返回 TP 和 SL → 只取 SL 白名单类型,不误读 TP 价格
  - `PositionSide` 为空但 `Side=SELL/BUY` 可推断当前持仓 SL → 正确识别 active SL
  - long 新 SL ≥ `markPrice - safetyBuffer` / short 新 SL ≤ `markPrice + safetyBuffer` → 跳过,避免立即触发
  - 同一 symbol 同时 long+short → Phase 1 跳过,避免 symbol 级撤单误伤
  - exchange 为 Hyperliquid/Lighter → Phase 1 跳过,避免撤掉 TP
  - 无 active SL 时 oldSL fallback:long=0,short=MaxFloat64 → 安全校验自然通过

### 7.2 集成测试

- [ ] Paper trader:开仓 → 浮盈涨到 1% → 验证 SL order 出现在 entryPrice
- [ ] Paper trader:开仓 → 浮盈涨到 3% → 逐分钟验证:第一分钟 SL 推进到 step1,第二分钟推进到 step2(每 ticker 限 1 步)
- [ ] Paper trader:开仓 → 价格下跌 → BE 不触发,SL 保留在原位
- [ ] Paper trader:BE 推进 2 步后 → 验证 active SL 价格正确;当前 Paper trader 只记录 stop order,不自动模拟触发关闭
- [ ] 若要测试"价格回落触发 SL → 仓位关闭",需先扩展 Paper trader 的 stop order matching/simulation
- [ ] Paper trader:BE 触发后 → AI 主动 CLOSE → `breakevenSteps` map key 正确清理
- [ ] Paper trader:trader 重启后 → active SL 已在 step2 且浮盈仍 3% → 从 step2 推进到 step3,不得后退

### 7.3 端到端验证

- [ ] devnet / testnet 上,人工开一笔 1x 仓,推到 +1% 浮盈,检查交易所实际 SL order 已改为 entryPrice
- [ ] devnet 上推到 +3% 浮盈,SL 应推进 2 步
- [ ] 极端 case:10x 杠杆 + 0.1% 阈值 → 标的价格约 +0.01%(杠杆收益率 +0.1%)时应触发 1 步推进,SL 在 entryPrice

---

## 八、风险与回退

| 风险 | 缓解 / 观察指标 |
|---|---|
| BE 推进后被滑点甩出,小幅亏损被 ConsecutiveLossBrake 计数,导致震荡市频繁冷却 | 观察上线后 2-4 周的"BE 触发的平仓 PnL 分布";若 -1% 以上亏损占比 > 30% 触发冷却,改走 B 路径(特判 BE 平仓不算) |
| 多次推进导致 SL 下单 API 调用频率增加 | Phase 1 已内置"每 ticker 最多推进 1 步"硬限制:1 分钟 ticker × N 笔仓位 × 最多 1 次/仓位 = 最多 N 次 `SetStopLoss`/分钟,在交易所 rate limit 范围内 |
| 多交易所 SL 下单失败时,代码静默吞错 | 日志记录 + 监控(下一阶段可加 metric);BE 状态在内存,下次 ticker 仍会重试 |
| `CancelStopLossOrders` 与 `SetStopLoss` 之间有 race(交易所已部分成交) | Phase 1 使用 cancel→set,存在短暂无保护窗口;下单前重新读取仓位,失败不更新 steps。Phase 2 用 side/order-id aware amend/replace 消除窗口 |
| 当前撤 SL 接口是 symbol 级别,可能误撤同币另一侧仓位 SL | Phase 1 检测同 symbol long+short 时跳过 BE;Phase 2 增加 side-aware cancel |
| Hyperliquid/Lighter `CancelStopLossOrders` 会撤 TP | Hyperliquid/Lighter 默认跳过 BE,直到 adapter 支持精确撤 SL |
| 新 SL 已越过当前价导致立即触发 | 下单前校验 long: `newSL < markPrice - safetyBuffer`,short: `newSL > markPrice + safetyBuffer` |
| `GetOpenOrders` 返回的止盈单(TP)被误读为 SL,导致 `inferredSteps` 虚高 → SL 推得过远 | 按 `Type` 严格过滤:只取 `STOP_MARKET`/`STOP`/`STOP_LIMIT`/`STOPLOSS`,排除 `TAKE_PROFIT*`;`PositionSide` 为空时用 `Side` 推断 |
| 重启后内存 steps=0 导致 SL 后退 | 每轮从 active SL 推断 `inferredSteps`,并校验新 SL 不得比 oldSL 更差 |
| `breakevenSteps` map 内存增长(有 position 进出但 key 漏清) | 在 `emergencyClosePosition`、`clearPosition`、`ai-decide close` 三个路径都加清理;若仍泄漏,1 分钟 ticker 内 map 不会超过 100 个 key(单 trader 持仓上限) |
| 浮盈达 1% 推进后,价格又回落 0.X% 触发 SL,变成小幅亏损(-0.1% 量级),反复发生 3 次 → ConsecutiveLossBrake 触发冷却 | 这是 A 路径(最简)已知风险;观察期后再决定是否走 B 路径 |
| 跟 ExitStandards / ConsecutiveLossBrake 联动的复杂性 | 严守 §三 边界:不与 L1/L2 互相感知,BE 是独立的 L1 子机制 |

**回退方案**:`BreakevenProtection.Enabled = false`。也支持把整个 struct 设为 nil。等同关闭。

---

## 九、上线与监控

### 9.1 上线

- 通过 `RiskControlEditor` 灰度开关(每个 trader 独立)
- 建议先在 paper / devnet trader 上跑 1 周,再开 live

### 9.2 监控指标(后续 phase,本次不实现)

- `trader_breakeven_promoted_total{trader_id}` (counter)
- `trader_breakeven_close_pnl{trader_id}` (histogram)
- `trader_breakeven_promote_to_close_seconds` (histogram) — 从 BE promotion 到仓位关闭的时长

### 9.3 日志

- 触发时:`🛡️ [trader] BE promotion step 2: BTCUSDT LONG 浮盈 2.20% (≥ 2.00%), SL 60100.00 → 60200.00`
- 失败时:`⚠️ [trader] BE promotion step 2 failed: BTCUSDT LONG set SL: <err>`
- 跳步场景:`🛡️ [trader] BE promotion catch-up pending: BTCUSDT LONG 浮盈 4.50% (target steps=4, current=0), 本轮推进 1 步,剩余后续 ticker 追赶`

---

## 十、实施顺序

1. **后端**(一次性提交)
   - `store/strategy.go`:类型 + 默认值 + ClampLimits
   - `trader/auto_trader.go`:`breakevenSteps` 字段 + 锁
   - `trader/auto_trader_risk.go`:`checkBreakevenPromotion` + 挂到 ticker + 清理路径同步
2. **前端**(一次性提交)
   - 类型 + i18n + 编辑器
3. **测试**
   - 单元 + paper integration
4. **观察 2-4 周,再决定 B 路径**

预计工作量:后端 ~180 行 Go,前端 ~50 行 TSX(比一次性版多 30 行 Go,主要是 step 累进逻辑 + 跳步 catch-up 日志)。
