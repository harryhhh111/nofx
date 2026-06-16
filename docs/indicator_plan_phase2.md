# Phase 2 指标实现方案 v2

> 基于 `docs/i.md` 的 14 个指标需求，Phase 1 已完成 #1-#8（SMA ~ VWAP），本文档规划剩余 #9-#14 的实现方案。
>
> **v3 更新（二轮审核）：** 
- P1-1：Rolling Percentile 返回 `(float64, bool)` 替代 NaN，无效时不输出 IndicatorPoint
- P1-2：Volume Spike 改为单一 multiplier + 静态 operand 名（`volume_spike`），不再动态生成 `volume_spike_{M}x`
- P1-3：`return_3d` 默认不启用（1m/3m 超 MaxComputeLookback=1000）；bar 不足时 skip 不报错
- P1-4：MTSI zscore/percentile lookback 需同步更新 `requiredCalculationLookback()`，不只是 `ensureComputeLookbackForCalculations()`
- P2-1：`last_volume_spike_high` 明确 per-period 输出 + 排除当前 bar
- P2-2：`always_calculated_indicators` 仅为 API 元数据，运行时保障来自 `clampIndicatorConfig()`
- P2-3：检查清单增加 `requiredCalculationLookback()` 行
- 新增设计原则 #4（bar 不足 skip 不报错）、#5（无效值处理）、#6（operand 静态白名单）

---

## 当前状态

### Phase 1 已实现（#1-#8）

| # | 指标 | 文件 | 状态 |
|---|------|------|------|
| 1 | SMA | `market/indicator_sma.go` | ✅ |
| 2 | ADX / DMI | `market/indicator_adx.go` | ✅ |
| 3 | Parabolic SAR | `market/indicator_sar.go` | ✅ |
| 4 | Donchian Channel | `market/indicator_donchian.go` | ✅ |
| 5 | Previous Session OHLC | `market/indicator_session.go` | ✅ |
| 6 | Opening Range | `market/indicator_opening_range.go` | ✅ |
| 7 | R-Breaker | `market/indicator_rbreaker.go` | ✅ |
| 8 | VWAP | `market/indicator_vwap.go` | ✅ |

### Phase 2 待实现（#9-#14）

| # | 指标 | 涉及模板 | 当前状态 |
|---|------|----------|----------|
| 9 | MTSI | micaletti_mtsi_reversion | 未实现 |
| 10 | Rolling Percentile | micaletti_mtsi_reversion | 未实现 |
| 11 | Price / Momentum Ranking | myquant_momentum_scan_20/25 | 部分（price_change 有单周期 bar 收益，无 named return / first_cross） |
| 12 | Small Market Value Ranking | myquant_alpha_small_market_value | **暂缓**（需外部数据；模板文件也未落地） |
| 13 | Volume Spike Ratio | durgia_volume_breakout | 部分（volume_ratio 已有，缺 spike / last spike high） |
| 14 | Parameterized Bollinger | myquant_bollinger_bandit | 引擎支持，配置层硬编码 k=2 |

---

## 架构回顾

新增一个技术指标的标准改动流程（8 个文件 + 2 个前端文件 + 1 个测试文件）：

### 后端（Go）

```
market/data_indicators.go        ← 纯计算函数（无状态、无 IO）
market/indicator_xxx.go          ← Module 实现（Name + Calculate）
market/indicator_engine.go       ← 注册到 DefaultIndicatorEngine
market/factor_snapshot.go        ← IndicatorRequest 增加配置字段（如需）
store/strategy.go                ← IndicatorConfig 增加 enable 开关 + 参数
                                  → ensureComputeLookbackForCalculations() 纳入新 period（勿漏）
kernel/engine_analysis.go        ← IndicatorRequestFromStrategyConfig() 映射 config → request
api/strategy_metadata.go         ← 前端元数据（technical_indicators 条目 + indicatorGroup + indicatorAvailableInConfig）
kernel/strategy_metadata.go      ← SupportedIndicatorOperands() 注册操作数
```

**关键函数名确认：**
- `store/strategy.go:399` → `ensureComputeLookbackForCalculations()`
- `kernel/engine_analysis.go:544` → `IndicatorRequestFromStrategyConfig()`

**跨模块隔离约束（重要）：**
- `indicator_engine.go:97-101` 逐个调用 `mod.Calculate(calcCtx, req)`，各 module 之间**不共享输出**。`CalcContext` 只包含原始 K 线 + timeframe + as_of + symbol，**不包含其他 module 的 IndicatorPoint 结果**。
- 因此 #9 MTSI 不能在 Calculate 内"复用 VWAPModule 的输出"，必须内部重新调用 `calculateVWAP(klines, period)`。

### 前端（TypeScript/React）

```
web/src/types/strategy.ts                           ← IndicatorConfig 接口补充字段
web/src/components/strategy/IndicatorEditor.tsx      ← fallbackTechnicalIndicators 同步新增条目
```

### 测试

```
market/indicator_keying_test.go   ← 新增 module 的注册键测试（多输出指标必须测 name-addressability）
```

---

## 逐项设计

### 前置 0 — 补全前端现有缺口

Phase 1 的 #6 Opening Range 和 #7 R-Breaker 后端已实现，但前端 `IndicatorConfig` 类型和 `fallbackTechnicalIndicators` 缺少对应字段。**本轮一并补上**，不改步骤编号。

**改动：**

| 文件 | 改动 |
|------|------|
| `web/src/types/strategy.ts` | `IndicatorConfig` + `enable_opening_range: boolean`、`enable_rbreaker: boolean`、`opening_range_minutes?: number` |
| `web/src/components/strategy/IndicatorEditor.tsx` | `fallbackTechnicalIndicators` + opening_range、rbreaker 两个条目 |

---

### #14 — Parameterized Bollinger

#### 当前状态

引擎层 `BOLLSpec{Period, Multiplier}` 已支持自定义标准差倍数，但配置→请求映射硬编码 `Multiplier: 2`：

```go
// kernel/engine_analysis.go:569-571（现状）
for _, period := range indicators.BOLLPeriods {
    req.BOLLPeriods = append(req.BOLLPeriods, market.BOLLSpec{
        Period: period, Multiplier: 2,
    })
}
```

#### 设计决策

`IndicatorConfig` 增加 `BOLLMultiplier float64`，默认 2.0，所有 period **共享同一个 multiplier**。

> 不给每个 period 配独立 multiplier 的理由：当前模板只需要 n=50/k=1（Bandit）。如果后续需要混用 `20/2 + 50/1`，可升级为 `[]BOLLSpec`。现在共享 multiplier 改动最小，JSON 兼容性最好。

#### 兼容性

老 strategy JSON 反序列化时 `BOLLMultiplier == 0`，由 `clampIndicatorConfig()` 兜底为 2.0。

#### 改动清单

| 文件 | 改动 |
|------|------|
| `store/strategy.go` | `IndicatorConfig` + `BOLLMultiplier float64` (`json:"boll_multiplier,omitempty"`)；`clampIndicatorConfig()` 中 0 → 2.0 |
| `kernel/engine_analysis.go` | `IndicatorRequestFromStrategyConfig()` 用 config 的 `BOLLMultiplier` 替代硬编码 2；当值为 0 时 fallback 2 |
| `api/strategy_metadata.go` | `enable_boll` 条目 + `multiplier_key: "boll_multiplier"` |
| `store/strategy.go` | `ensureComputeLookbackForCalculations()` 无需改（period 已覆盖，multiplier 不影响 bar 数） |

**无新文件，0 新增 Module。**

---

### #13 — Volume Spike Ratio

#### 当前状态

`VolumeModule` 已输出 `volume`、`volume_avg`、`volume_ratio`（见 `market/indicator_volume.go:19-28`）。

还缺：

| 字段 | 说明 |
|------|------|
| `volume_spike` | 当前 bar volume_ratio ≥ multiplier |
| `last_volume_spike_high` | 最近一次 spike bar 的 high（回溯扫描，排除当前 bar） |
| `break_last_volume_spike_high` | close 突破上次 spike bar 的 high |

#### ⚠️ 关键约束：operand 必须是静态名称

`kernel/strategy_metadata.go` 的 `SupportedIndicatorOperands()` 是**静态白名单**，不允许动态生成 `volume_spike_2x`、`volume_spike_4x` 这类带参数后缀的 operand。

**两个方案：**

| 方案 | operand 名 | 做法 |
|------|-----------|------|
| A（采纳） | `volume_spike` | 单一 operand，multiplier 在 `Params`（`{"multiplier": 4}`）中携带。规则引擎判断时取 `IndicatorValue("volume_spike", "5m", 20)` 返回 0/1 |
| B（不采纳） | `volume_spike_4x` | 预注册 2x/3x/4x/5x 四个固定 operand，但不灵活 |

**结论：方案 A。** 单一 multiplier + 统一 operand 名。

另外，`volume_ratio` 本身已可在规则中做 `volume_ratio >= 4` 判断，`volume_spike` 有一定冗余。其存在意义是给 `last_volume_spike_high` 提供一致的"spike"定义标准。

#### 设计决策

- `IndicatorConfig` 增加 `VolumeSpikeMultiplier float64`（**单数**），默认 `4.0`
- `last_volume_spike_high`：对每个 `VolumePeriod` 分别输出。扫描逻辑**排除当前 bar**（当前 bar 放量时用前一根 spike bar 的 high，否则 `break_last_volume_spike_high` 在当前 bar 自己放量时立即触发，逻辑错误）
- 如果历史中找不到 spike bar：不输出 `last_volume_spike_high` 和 `break_last_volume_spike_high`（返回 nil 而非 NaN）

#### 输出字段

| 字段 | 类型 | Period | 说明 |
|------|------|--------|------|
| `volume_spike` | 0/1 | n | volume_ratio >= multiplier，Params: `{"multiplier": M}` |
| `last_volume_spike_high` | float64 | n | 最近 spike bar（不含当前）的 high；无历史 spike 则不输出 |
| `break_last_volume_spike_high` | 0/1 | n | close > last_volume_spike_high；无 `last_volume_spike_high` 时不输出 |

#### 改动清单

| 文件 | 改动 |
|------|------|
| `store/strategy.go` | `IndicatorConfig` + `VolumeSpikeMultiplier float64` (`json:"volume_spike_multiplier,omitempty"`)；`clampIndicatorConfig()` 默认 4.0 |
| `store/strategy.go` | `ensureComputeLookbackForCalculations()` 无需改（period 来自 VolumePeriods，multiplier 不影响 bar 数需求） |
| `market/indicator_volume.go` | 扩展 Calculate()，per-period 增加 spike 检测 + last spike high 扫描（排除当前 bar） |
| `market/data_indicators.go` | +1 函数 `findLastVolumeSpikeHigh(klines []Kline, period int, multiplier float64) *Kline`（返回 nil 表示无历史 spike） |
| `api/strategy_metadata.go` | Volume 条目的 operands + `volume_spike`、`last_volume_spike_high`、`break_last_volume_spike_high` |
| `kernel/strategy_metadata.go` | +3 静态 operands：`volume_spike`、`last_volume_spike_high`、`break_last_volume_spike_high` |
| `web/src/types/strategy.ts` | `IndicatorConfig` + `volume_spike_multiplier?: number` |
| `web/src/components/strategy/IndicatorEditor.tsx` | Volume 卡片可能需要 multiplier 输入框 |
| `market/indicator_keying_test.go` | + Volume spike 多输出地址测试（含无历史 spike 的 nil 兜底测试） |

---

### #10 — Rolling Percentile

#### 公式

```
percentile_rank = count(x_i ≤ x_t in window) / window_size × 100
z_score         = (x_t - mean(window)) / std(window)
```

#### 设计决策

| 方案 | 做法 | 优点 | 缺点 |
|------|------|------|------|
| **A（采纳）** | 在 `data_indicators.go` 中做成纯函数，不注册 Module | 简单，MTSI 等模块直接调用；0 新文件 | 使用方需硬编码 window 参数 |
| B（后续升级） | 独立 Module，通过 target 参数指定对哪个指标做分位 | 前端可自由组合 | 需要跨模块引用机制，工程量大 |

**结论：方案 A。**

#### 核心函数

```go
// data_indicators.go

// calculateRollingPercentile 计算 series 中最后一个值在窗口内的百分位排名 (0-100)。
// 返回 (value, true)；如果 len(series) < window 则返回 (0, false)。
// 注意：不使用 NaN，因为 encoding/json 不能序列化 NaN，且 NaN 无法做规则比较。
func calculateRollingPercentile(series []float64, window int) (float64, bool)

// calculateZScore 计算 series 中最后一个值在窗口内的 z-score。
// 返回 (value, true)；如果 len(series) < window 或窗口内 std_dev == 0 则返回 (0, false)。
func calculateZScore(series []float64, window int) (float64, bool)
```

**使用模式：** 调用方在 `ok == false` 时不输出对应 IndicatorPoint（不追加到 points 列表），模块不报错。这与其他模块"条件不够时不输出"的模式一致。`IndicatorValue()` 查询时返回 `(0, false)`，不会影响规则评估。

#### 改动清单

| 文件 | 改动 |
|------|------|
| `market/data_indicators.go` | +2 函数 `calculateRollingPercentile()` `calculateZScore()` |

**无新文件，无 API 暴露，无前端改动**（纯内部工具函数）。

---

### #9 — MTSI

#### 公式

```
MTSI = ln(close / VWAP)
```

#### 依赖

- VWAP 计算公式：`calculateVWAP()`（`data_indicators.go:213`，已有）
- Rolling Percentile：`calculateZScore()` + `calculateRollingPercentile()`（#10，先于本步完成）

#### ⚠️ 关键约束：跨模块隔离

`CalcContext` **不包含其他 module 的输出**（见 `indicator_engine.go:89-95`）。MTSIModule 的 `Calculate()` 不能"复用 VWAPModule 的结果"，必须**内部重新调用 `calculateVWAP(klines, period)`** 重复计算 VWAP。

这不会引入额外问题：`calculateVWAP` 是纯函数，相同输入产生相同输出，两次调用结果一致。

#### 周期来源

MTSI **复用 `VWAPPeriods`**（与 VWAP 相同的周期列表），不增加独立的 `MTSIPeriods`。

- `IndicatorRequest` 已有 `VWAPPeriods []int`，MTSIModule 直接读取。
- 如果 `VWAPPeriods` 为空且 `EnableMTSI == true`，MTSIModule 返回空（不报错）。
- 运行时保障来自 `clampIndicatorConfig()`（`store/strategy.go`），该函数为 `VWAPPeriods` 设置默认值 `[20]`。`always_calculated_indicators` 仅是 API 元数据，不参与运行时保证。

#### 启用开关风格

`IndicatorConfig` 增加 `EnableMTSI bool`（与 `EnableRBreaker` / `EnableOpeningRange` 一致）。

> 为什么不沿用 VWAP 的"无 Enable 开关、仅靠 Periods 驱动"模式？因为 MTSI 复用 VWAPPeriods，无法靠独立的 Periods 判断是否启用，必须显式开关。

#### 输出字段（Phase 2a，依赖 #10 之前）

| 字段 | 类型 | 说明 |
|------|------|------|
| `mtsi` | float64 | `ln(close / vwap)` |
| `mtsi_abs` | float64 | `abs(mtsi)` |
| `close_vwap_distance_pct` | float64 | `(close / vwap - 1) × 100` |
| `close_above_vwap` | 0/1 | `close > vwap` |
| `close_below_vwap` | 0/1 | `close < vwap` |

#### 输出字段（Phase 2b，依赖 #10 完成后追加）

| 字段 | 类型 | window 默认值 | 说明 |
|------|------|---------------|------|
| `mtsi_zscore` | float64 | 100 bars | MTSI 序列的 rolling z-score |
| `mtsi_percentile` | float64 | 100 bars | MTSI 序列的 rolling percentile rank |

> window 默认 100 bars 的理由：足够覆盖不同 timeframe（5m 的 100 bars ≈ 8h，1h 的 100 bars ≈ 4d），避免小样本误判。默认值写死在代码中，后续可通过 config 开放。

#### 改动清单

| 文件 | 改动 |
|------|------|
| `market/indicator_mtsi.go` | **新增** — MTSIModule（实现 `Name()` + `Calculate()`） |
| `market/data_indicators.go` | +1 函数 `calculateMTSI(klines []Kline, vwapPeriod int) (mtsi, abs, distPct, above, below float64)` |
| `market/indicator_engine.go` | 注册 `&MTSIModule{}` |
| `market/factor_snapshot.go` | `IndicatorRequest` + `EnableMTSI bool` |
| `store/strategy.go` | `IndicatorConfig` + `EnableMTSI bool` (`json:"enable_mtsi"`)；`ensureComputeLookbackForCalculations()` 追加 MTSI window 所需 bar 数 |
| `kernel/engine_analysis.go` | `IndicatorRequestFromStrategyConfig()` 映射 `req.EnableMTSI = indicators.EnableMTSI`；**`requiredCalculationLookback()` 同步纳入 MTSI** |
| `api/strategy_metadata.go` | `technical_indicators` +1 条目、`indicatorGroup()` +1 case、`indicatorAvailableInConfig()` +1 case |
| `kernel/strategy_metadata.go` | +5 operands（mtsi, mtsi_abs, close_vwap_distance_pct, close_above_vwap, close_below_vwap） |
| `web/src/types/strategy.ts` | `IndicatorConfig` + `enable_mtsi: boolean` |
| `web/src/components/strategy/IndicatorEditor.tsx` | `fallbackTechnicalIndicators` +1 条目 |
| `market/indicator_keying_test.go` | + MTSI 多输出地址测试 |

**关于 lookback：** 当前仅基础 MTSI 输出时，lookback 只需 `maxVWAPPeriod`（已在 `ensureComputeLookbackForCalculations` 中通过 `VWAPPeriods` 覆盖）。

> Phase 2b 追加 `mtsi_zscore`（window=100）时，lookback 需变为 `maxVWAPPeriod + 100 - 1`。届时必须同时改 `store/strategy.go:ensureComputeLookbackForCalculations()` **和** `kernel/engine_analysis.go:requiredCalculationLookback()`，两个函数各自维护了 lookback 计算逻辑。

---

### #11 — Price / Momentum Ranking

#### 公式

```
return_h = close_now / close_N_bars_ago - 1
first_cross_20pct_3d = return_3d >= 0.20 AND prev_return_3d < 0.20
first_cross_25pct_3d = return_3d >= 0.25 AND prev_return_3d < 0.25
```

#### ⚠️ 与 PriceChangeModule 的关系（关键决策）

现有 `PriceChangeModule`（`market/indicator_price_change.go`）已按 `PriceChangeWindows []int`（bar 数）输出 `price_change`。

**问题：** `return_1h`、`return_4h`、`return_24h`、`return_3d` 本质上是不同窗口的 `price_change`。如果新增一个独立 `MomentumModule`，会导致：
- 两组语义高度重叠的 operands（`price_change_12` vs `return_1h` 在 5m 线上等价）
- 两份几乎相同的计算逻辑
- 维护成本翻倍

**决策：方案 1 — 扩展现有 PriceChangeModule，不新增 MomentumModule。**

具体做法：

1. `PriceChangeModule` 继续用 `PriceChangeWindows []int` 驱动，输出 `price_change`（已有）
2. 同时新增部分**有意义的命名窗口**（`return_1h`、`return_4h`、`return_24h`、`return_3d`），其本质是从当前 bar 往前推 N 根，参数存为具体 bar 数
3. `first_cross_20pct_3d` / `first_cross_25pct_3d` 新增为附加输出，基于 `return_3d` 的值变化检测（需要向前多看一根 bar 的 return_3d 做比较）
4. 新增 `PriceChangeNamedWindows []string`（如 `["1h","4h","24h","3d"]`），模块内部根据当前 timeframe 换算为 bar 数

**为什么不用方案 2（废弃 PriceChange 并入 Momentum）：**
- `price_change` operands 已在线上策略中使用，废弃是破坏性改动。
- 扩展现有模块风险更小。

#### ⚠️ 关键约束：MaxComputeLookback 上限为 1000

`store/strategy.go:20` 定义 `MaxComputeLookback = 1000`。named window 换算为 bar 数后不能超过此值。

各 timeframe 下 window 对应 bar 数：

| window | 1m | 3m | 5m | 15m | 1h | 4h | 1d |
|--------|----|----|----|-----|----|----|-----|
| `return_1h` | 60 | 20 | 12 | 4 | 1 | — | — |
| `return_4h` | 240 | 80 | 48 | 16 | 4 | 1 | — |
| `return_24h` | 1440 ⚠️ | 480 | 288 | 96 | 24 | 6 | 1 |
| `return_3d` | 4320 ⚠️ | 1440 ⚠️ | 864 | 288 | 72 | 18 | 3 |

`return_3d` 在 1m/3m 上超限，`return_24h` 在 1m 上超限。

**处理规则：** Module 在 `Calculate()` 中对每个 named window 做手动 bar 数判断（`if len(klines) <= bars { continue }`）。bar 数不足时**跳过该 window（不输出对应 IndicatorPoint），不报错**。这样不会因为单个 timeframe 数据不足而中断整个 snapshot。

> 不要用 `requireBars()` / `requireBarsExclusive()`（它们返回 error，会中断整个 snapshot）。命名 windows 的场景用 `hasEnoughBars(klines, bars int) bool` 做静默跳过。

**默认值：** `PriceChangeNamedWindows` 默认 `["1h","4h","24h"]`，**不含 `3d`**。用户手动加上 `3d` 时，只会在 ≥5m 的 timeframe 上输出，低 timeframe 静默跳过。

`first_cross_*` 同理：如果 `return_3d` 因 bar 数不足未被输出，`first_cross_20pct_3d` / `first_cross_25pct_3d` 也不输出。

#### 输出字段

| 字段 | 类型 | 说明 |
|------|------|------|
| `price_change` | float64 | 已有，按 `PriceChangeWindows`（bar 数） |
| `return_1h` | float64 | 1 小时窗口（bar 数 = 60 / timeframe_minutes），不足则跳过 |
| `return_4h` | float64 | 4 小时窗口 |
| `return_24h` | float64 | 24 小时窗口 |
| `first_cross_20pct_3d` | 0/1 | `return_3d >= 0.20 AND prev_return_3d < 0.20`；3d 窗口不可用时跳过 |
| `first_cross_25pct_3d` | 0/1 | `return_3d >= 0.25 AND prev_return_3d < 0.25`；3d 窗口不可用时跳过 |

> 跨币种排名（`price_rank_*`）属于外部聚合层能力，不在本步实现。

#### 改动清单

| 文件 | 改动 |
|------|------|
| `store/strategy.go` | `IndicatorConfig` + `PriceChangeNamedWindows []string` (`json:"price_change_named_windows,omitempty"`)；`ensureComputeLookbackForCalculations()` 纳入 named windows 的最大 bar 数（上限 MaxComputeLookback） |
| `store/strategy.go` | `clampIndicatorConfig()` 默认 windows: `["1h","4h","24h"]`（不含 3d，避免低 timeframe 超限）；`ensureComputeLookbackForCalculations()` lookback 下限 ≥ max(return_24h 所需 bar, 已有 PriceChangeWindows) + 1（first_cross 需要多一根） |
| `kernel/engine_analysis.go` | `IndicatorRequestFromStrategyConfig()` 映射 named windows；**`requiredCalculationLookback()` 同步纳入** |
| `market/indicator_price_change.go` | 扩展 Calculate()，per-window 用 `hasEnoughBars()` 判断 bar 数，不足则**跳过**（不报错，用 `continue`） |
| `market/data_indicators.go` | +1 函数 `detectFirstCross(klines []Kline, bars int, threshold float64) (float64, bool)` 返回 (0/1, ok)；bar 不足返回 false |
| `market/factor_snapshot.go` | `IndicatorRequest` + `PriceChangeNamedWindows []string` |
| `api/strategy_metadata.go` | Price Change 条目扩展 operands |
| `kernel/strategy_metadata.go` | +6 operands（return_1h, return_4h, return_24h, return_3d, first_cross_20pct_3d, first_cross_25pct_3d） |
| `web/src/types/strategy.ts` | `IndicatorConfig` + `price_change_named_windows?: string[]` |
| `web/src/components/strategy/IndicatorEditor.tsx` | 如 Price Change 卡片需要 UI 更新（window 选择器） |
| `market/indicator_keying_test.go` | + named windows 输出地址测试 + bar 不足跳过测试 |

---

### #12 — Small Market Value / Market Cap Ranking

#### 当前状态

- `docs/i.md` 列举了 `myquant_alpha_small_market_value` 模板需求。
- 但该模板**在代码库中尚无对应文件**（未落地）。
- 该指标依赖 `circulating_supply` / `market_cap`，无法从 K 线推导。

#### 决策

**Phase 2 暂不实现。** 后续作为 external data source 接入，放入 `FactorSnapshot.External`。

如果决定此模板永久不做，建议从 `docs/i.md` 的活跃列表中移除，避免模板处于"语义不可满足"状态。

---

## 推荐实现顺序（修正版）

```
前置 0: 补全前端 #6/#7 缺失字段              (10min, 0新文件)  ← 先修现有缺口
Step 1: #14 Parameterized Bollinger           (20min, 0新文件)  ← 仅改配置层，引擎已支持
Step 2: #13 Volume Spike Ratio                (1h,   0新文件)  ← 扩展现有 VolumeModule
Step 3: #10 Rolling Percentile                (30min, 0新文件)  ← MTSI 前置依赖
Step 4: #9  MTSI                              (1.5h, 1新文件)  ← 依赖 VWAP + Rolling Percentile
Step 5: #11 Price/Momentum (扩展 PriceChange) (3h,   0新文件)  ← 扩展现有 PriceChangeModule
Step 6: #12 Small Market Value                (暂缓)
```

**每一步独立可合入，不互相阻塞。**（#4 依赖 #3，其余无依赖。）

---

## 总改动量（修正版）

| Step | 指标 | 新文件 | Go 文件 | Web 文件 | 测试 | 估时 |
|------|------|--------|---------|----------|------|------|
| 前置 0 | #6/#7 前端补全 | 0 | 0 | 2 | 0 | 0.2h |
| 1 | #14 Bollinger 参数化 | 0 | 3 | 0 | 0 | 0.3h |
| 2 | #13 Volume Spike | 0 | 6 | 2 | 1 | 1h |
| 3 | #10 Rolling Percentile | 0 | 1 | 0 | 0 | 0.5h |
| 4 | #9 MTSI | 1 | 8 | 2 | 1 | 1.5h |
| 5 | #11 PriceChange 扩展 | 0 | 8 | 2 | 1 | 3h |
| 6 | #12 Small Market Value | — | — | — | — | 暂缓 |
| **合计** | | **1** | **26** | **8** | **3** | **~6.5h** |

---

## 遗漏项检查清单

> 每个 step 完成时，逐项检查以下文件是否都已覆盖：

| 检查项 | 文件位置 | Step 1 | Step 2 | Step 3 | Step 4 | Step 5 |
|--------|----------|--------|--------|--------|--------|--------|
| 计算函数 | `market/data_indicators.go` | — | ✅ | ✅ | ✅ | ✅ |
| Module 注册 | `market/indicator_engine.go` | — | — | — | ✅ | — |
| 请求字段 | `market/factor_snapshot.go` | — | ✅ | — | ✅ | ✅ |
| 配置字段 | `store/strategy.go` | ✅ | ✅ | — | ✅ | ✅ |
| lookback 下限（clamp 阶段） | `store/strategy.go:191` → `ensureComputeLookbackForCalculations()` | ✅ | ✅ | — | ✅ | ✅ |
| lookback 下限（fetch 阶段） | `kernel/engine_analysis.go:307` → `requiredCalculationLookback()` | ✅ | — | — | ✅ | ✅ |
| JSON 兼容默认值 | `store/strategy.go:clampIndicatorConfig()` | ✅ | ✅ | — | ✅ | ✅ |
| config→request 映射 | `kernel/engine_analysis.go:544` → `IndicatorRequestFromStrategyConfig()` | ✅ | ✅ | — | ✅ | ✅ |
| API 元数据 | `api/strategy_metadata.go` | ✅ | ✅ | — | ✅ | ✅ |
| 策略 operands | `kernel/strategy_metadata.go` | — | ✅ | — | ✅ | ✅ |
| 前端类型 | `web/src/types/strategy.ts` | — | ✅ | — | ✅ | ✅ |
| 前端 fallback | `web/src/components/strategy/IndicatorEditor.tsx` | — | ✅ | — | ✅ | ✅ |
| 单元测试 | `market/indicator_keying_test.go` | — | ✅ | — | ✅ | ✅ |

> **注意：** `ensureComputeLookbackForCalculations()`（在 `clamp*` 阶段调用）和 `requiredCalculationLookback()`（在 fetch 阶段调用）是**两处独立的 lookback 校验**，新增任何影响 bar 数需求的参数时必须同步更新。历史已有遗漏案例，本轮避免重复。

---

## 设计原则与风格约定

1. **Enable 开关风格：** 若指标有独立周期参数可用其判断是否启用（如 VWAP），沿用"无 Enable 开关"风格。若复用其他指标的周期（如 MTSI 复用 VWAPPeriods），或无周期参数（如 R-Breaker），则增加显式 `EnableXxx bool`。

2. **JSON 兼容性：** 所有新增 `IndicatorConfig` 字段必须带 `omitempty` tag。零值语义（0/""/nil/false）必须在 `clampIndicatorConfig()` 中兜底默认值，确保老 strategy JSON 反序列化后不崩溃。

3. **多输出 addressability：** 如果一个 Module 输出多个 indicator name（如 MTSI 输出 `mtsi` + `mtsi_abs` + `close_vwap_distance_pct`），必须在 `indicator_keying_test.go` 中验证每个 name 可独立寻址。历史 bug 教训见 `TestMultiOutputIndicatorsAddressable`。

4. **bar 数安全：** `requireBars()` / `requireBarsExclusive()` 返回 error，适用于"数据不足时必须中断"的场景（如 SMA 的 period 校验）。对于"数据不足时应静默跳过"的场景（如 named windows 在低 timeframe 上），用 `hasEnoughBars(klines []Kline, bars int) bool` 手动判断 + `continue`。`ensureComputeLookbackForCalculations()` 和 `requiredCalculationLookback()` 共同确保 fetch 阶段已拿到足够 K 线。但 Module 必须再兜底一次：**bar 不足时 skip（不输出），不报错**，避免单个 timeframe 窗口不足中断整个 snapshot。

5. **无效值处理：** 禁止返回 `NaN`（`encoding/json` 不能序列化，规则引擎也无法比较）。任何可能无法计算的值使用 `(float64, bool)` 返回值模式，`ok=false` 时**不追加 IndicatorPoint**（而非追加一个 sentinel 值）。这与其他模块"条件不满足时不输出"的模式一致。

6. **operand 静态白名单：** `SupportedIndicatorOperands()` 是静态数组，不随用户配置动态变化。新 operand 名称必须在设计阶段确定并注册，不允许运行时拼接生成。
