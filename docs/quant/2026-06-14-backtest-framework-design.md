# 量化回测框架设计

> 作为长期量化交易系统的第一块地基，定义可信回测框架的目标、非目标、数据边界、核心模型、执行流程、费用/滑点假设和 MVP 实施顺序。

---

## 一、为什么先做回测框架

量化系统的第一阶段不应直接写策略或接入实盘，而应先建立可信回测。

原因：

- 没有可信回测，策略收益无法判断是 alpha、过拟合，还是数据/执行假象。
- 低周期交易的毛利润很薄，手续费、滑点、资金费率会显著改变结果。
- 多周期指标容易出现未来函数，必须先定义时间对齐规则。
- paper trading 和 live trading 都需要一个可对照的历史基准。
- 后续所有策略都应共享同一套数据、风控、费用和指标口径。

一句话：

```text
先证明框架不会骗自己，再验证策略是否有价值。
```

---

## 二、MVP 目标与非目标

### MVP 目标

第一版回测框架只追求可验证、可复现、可扩展。

必须支持：

- BTC/ETH 两个标的。
- 5m/15m/1h 三个周期。
- bar-by-bar 回放。
- 单策略单仓位回测。
- 手续费、滑点、资金费率成本。
- 固定风险仓位 sizing。
- 止盈、止损、time stop、breakeven、trailing stop。
- 每笔交易归因到 signal、feature snapshot、regime、risk decision。
- 输出基础指标报告。

### MVP 非目标

第一版先不做：

- tick 级回测。
- orderbook 盘口撮合。
- 多交易所套利。
- 复杂组合优化。
- 自动参数寻优。
- AI 参与交易决策。
- 多策略资金分配。

这些可以后续扩展，但不应进入第一版。

---

## 三、数据输入边界

### 1. 必需数据

| 数据 | 用途 | MVP 要求 |
|---|---|---|
| kline | 回放、指标、信号 | 必需，至少 5m/15m/1h |
| funding rate | 持仓成本 | 可按周期近似 |
| fee schedule | 手续费 | maker/taker 先固定配置 |
| symbol metadata | 精度、最小下单量 | 用于 sizing 和结果校验 |

### 2. 可选数据

| 数据 | 用途 | 进入阶段 |
|---|---|---|
| open interest | 趋势/拥挤度过滤 | Phase 2+ |
| volume profile | 结构位判断 | Phase 2+ |
| orderbook | 滑点模型优化 | Phase 3+ |
| trades | 更精细成交模拟 | Phase 3+ |

### 3. 数据质量检查

每次回测前必须检查：

- K 线是否缺失。
- 时间戳是否严格递增。
- 多周期数据是否能按时间对齐。
- OHLC 是否满足 `low <= open/close <= high`。
- 是否存在异常跳价。
- funding rate 是否覆盖回测区间。

数据质量不过关时，回测应直接失败，而不是静默补齐。

---

## 四、核心模型

### 1. `BacktestRun`

表示一次完整回测。

```json
{
  "run_id": "2026-06-14-boll-mr-v1",
  "strategy": "boll_mean_reversion",
  "symbols": ["BTCUSDT", "ETHUSDT"],
  "timeframes": ["5m", "15m", "1h"],
  "start_time": "2026-03-01T00:00:00Z",
  "end_time": "2026-06-01T00:00:00Z",
  "initial_equity": 10000,
  "fee_model": "fixed_taker_fee",
  "slippage_model": "fixed_bps",
  "config_version": "v1"
}
```

### 2. `FeatureSnapshot`

每根主周期 K 线生成一次特征快照。

```json
{
  "symbol": "BTCUSDT",
  "time": "2026-05-01T12:00:00Z",
  "timeframe": "5m",
  "close": 65000,
  "rsi_7": 28.5,
  "rsi_14": 35.2,
  "boll_lower": 64800,
  "boll_middle": 65200,
  "boll_upper": 65600,
  "atr_14": 180,
  "adx_14": 18.4,
  "regime": "range"
}
```

要求：

- 只能使用当前时间点及以前的数据。
- 1h 特征在 5m 回测中只能使用已经收盘的 1h K 线。
- 每笔交易必须保存入场时的 feature snapshot。

### 3. `StrategySignal`

策略只输出候选信号，不直接下单。

```json
{
  "strategy": "boll_mean_reversion",
  "symbol": "BTCUSDT",
  "side": "long",
  "signal_time": "2026-05-01T12:00:00Z",
  "entry_type": "market",
  "stop_loss": 64750,
  "take_profit": 65400,
  "confidence": 0.72,
  "reason": "lower band touch + RSI oversold + low ADX"
}
```

### 4. `RiskDecision`

风控层决定是否执行、降仓或拒绝。

```json
{
  "action": "allow",
  "position_size": 0.08,
  "risk_usdt": 25,
  "reasons": ["risk_per_trade_0.25_pct", "daily_loss_ok"]
}
```

可选动作：

- `allow`
- `reduce`
- `block`

### 5. `BacktestTrade`

每笔交易必须包含完整生命周期。

```json
{
  "trade_id": "bt_001",
  "symbol": "BTCUSDT",
  "side": "long",
  "entry_time": "2026-05-01T12:05:00Z",
  "entry_price": 65010,
  "exit_time": "2026-05-01T12:45:00Z",
  "exit_price": 65380,
  "exit_reason": "take_profit",
  "gross_pnl": 29.6,
  "fees": 8.4,
  "funding": 0.0,
  "net_pnl": 21.2,
  "max_favorable_excursion_pct": 0.65,
  "max_adverse_excursion_pct": -0.18
}
```

---

## 五、回测执行流程

```text
load data
↓
validate data quality
↓
build aligned timeline
↓
for each bar:
    update feature snapshots
    update open positions
    apply exits: SL / TP / BE / trailing / time stop
    generate strategy signals
    apply portfolio & risk decisions
    simulate execution
    record events
↓
calculate metrics
↓
export trades / events / report
```

### 关键原则

- 先处理已有持仓退出，再处理新入场信号。
- 同一时刻只允许策略看到当前 bar 收盘后已知的信息。
- 入场默认在下一根 bar 开盘价附近成交，避免使用当前 bar 收盘价产生未来函数。
- 每个事件都应可追溯，不能只输出最终收益曲线。

---

## 六、成交、费用与滑点模型

### 1. 成交模型

MVP 使用保守简化模型：

- signal 在当前 bar 收盘生成。
- market entry 在下一根 bar open 成交。
- limit TP / stop SL 在后续 bar 内触发。
- 若同一根 bar 同时触发 TP 和 SL，默认按更保守路径处理。

保守路径建议：

| 持仓方向 | 同 bar 同时触发 TP/SL | MVP 假设 |
|---|---|---|
| long | high >= TP 且 low <= SL | 先触发 SL |
| short | low <= TP 且 high >= SL | 先触发 SL |

### 2. 手续费模型

第一版使用固定 taker fee：

```text
fee = notional * taker_fee_rate
```

默认所有入场和出场都按 taker 计算，避免高估收益。

### 3. 滑点模型

第一版使用固定 bps：

```text
long entry  = raw_price * (1 + slippage_bps)
long exit   = raw_price * (1 - slippage_bps)
short entry = raw_price * (1 - slippage_bps)
short exit  = raw_price * (1 + slippage_bps)
```

后续可升级为：

- 根据成交量动态滑点。
- 根据波动率动态滑点。
- 根据盘口深度估算冲击成本。

### 4. 资金费率

MVP 可按持仓跨越 funding timestamp 时扣除：

```text
funding = position_notional * funding_rate * side_factor
```

如果暂时没有完整 funding 数据，应在报告中明确标注，而不是忽略。

---

## 七、风控模块

回测必须从第一版就包含风控，否则策略结果没有实际参考意义。

### 1. 每笔固定风险

```text
risk_usdt = equity * risk_per_trade_pct
position_size = risk_usdt / abs(entry_price - stop_loss)
```

初期建议：

- `risk_per_trade_pct = 0.25%`
- 单日最大亏损 `1%`
- 每天最多 3 笔

### 2. 生命周期退出

必须支持：

- hard stop loss
- take profit
- time stop
- breakeven protection
- trailing stop

### 3. 冷却机制

必须记录并支持：

- symbol + side 连亏冷却。
- side 级别连亏冷却。
- daily loss 冷却。

### 4. 组合风险

MVP 可以先只允许单仓位，后续再支持：

- 多仓位。
- 同方向相关性暴露。
- 交易所级风险上限。
- 策略级风险预算。

---

## 八、输出报告

### 1. 基础指标

| 指标 | 含义 |
|---|---|
| total return | 净收益率 |
| gross pnl / net pnl | 成本前后收益 |
| max drawdown | 最大回撤 |
| win rate | 胜率 |
| avg win / avg loss | 平均盈利与亏损 |
| profit factor | 总盈利 / 总亏损 |
| Sharpe / Sortino | 风险调整收益 |
| max consecutive loss | 最大连亏 |
| fee to gross profit | 手续费侵蚀比例 |

### 2. 分组指标

必须按以下维度拆分：

- symbol
- side
- strategy
- regime
- weekday / hour
- exit reason
- risk decision

### 3. 交易明细

每笔交易应导出：

- signal 内容。
- 入场时 feature snapshot。
- risk decision。
- entry/exit price。
- gross/net PnL。
- MFE/MAE。
- exit reason。

### 4. 被拒绝信号追踪

被 `block` 或 `reduce` 的信号也要记录，并追踪后续表现。

这是判断风控是否有效的关键：

```text
如果被 block 的信号后续普遍亏损，说明风控有效。
如果被 block 的信号后续普遍盈利，说明风控过度约束。
```

---

## 九、目录与代码组织建议

建议量化系统先作为独立模块，不要混入现有 AI trader 主循环。

候选目录：

```text
quant/
  data/
  features/
  strategies/
  backtest/
  risk/
  reports/
```

如果暂时不想引入新的 Go package，也可以先用脚本验证：

```text
scripts/quant/
  export_data.go
  backtest_boll_mean_reversion.go
  report_backtest.go
```

但无论采用哪种方式，都应保持：

- 策略接口独立。
- 回测与 live 执行共享同一套风控口径。
- 报告输出格式稳定。
- 参数版本可追踪。

---

## 十、MVP 实施顺序

### Phase 0：数据导出与质量检查

- 导出 BTC/ETH 的 5m/15m/1h K 线。
- 导出 fee/funding 配置。
- 实现数据缺口检查。
- 输出数据质量报告。

### Phase 1：最小回测引擎

- 实现 bar-by-bar loop。
- 支持单 symbol 单策略。
- 支持 market entry、SL、TP。
- 支持手续费和固定滑点。
- 输出交易列表和基础指标。

### Phase 2：加入风控生命周期

- 固定风险 sizing。
- daily loss limit。
- time stop。
- breakeven protection。
- trailing stop。
- symbol/side cooldown。

### Phase 3：实现第一个策略

- 实现布林带均值回归。
- 加入 ADX/regime filter。
- 只测 BTC/ETH。
- 做参数敏感性测试。

### Phase 4：报告与对照

- 输出 HTML/Markdown 报告。
- 拆分 gross/net pnl。
- 按 regime/symbol/side 归因。
- 记录被拒绝信号后续表现。

### Phase 5：Paper Trading 对照

- 将同一策略和风控跑在 paper trading。
- 每天对照 paper 结果和回测预期。
- 观察 2-4 周后再评估是否进入小资金实盘。

---

## 十一、验收标准

第一版回测框架完成时，应满足：

- 能跑完 BTC/ETH 最近 3-6 个月 5m 数据。
- 每笔交易都有完整 feature、signal、risk、execution 记录。
- 成本前后收益分开展示。
- 同一参数重复运行结果完全一致。
- 数据缺口会导致明确失败或报告警告。
- 至少包含一个无策略基线和一个随机信号基线。
- 布林带均值回归可以作为第一个策略接入。

未达到这些标准前，不应进入 paper trading。

---

## 十二、下一步

建议下一步先做两件事：

1. 确认 nofx 现有 kline、funding、fee 数据来源与导出方式。
2. 在代码层建立最小 `quant/backtest` 或 `scripts/quant` 原型。

如果只选一个起点，优先做：

```text
BTC/ETH 5m K 线导出 + 数据质量检查 + 空策略回放
```

这一步看起来朴素，但它决定后面所有策略验证是否可信。
