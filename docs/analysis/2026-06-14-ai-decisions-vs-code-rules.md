# AI 决策与代码规则的协作边界分析

> 基于 `nofx-opt` 项目近几天的实盘表现、决策日志、策略配置与代码实现，分析哪些交易纪律应从 AI prompt 迁移到代码层，哪些判断仍应保留 AI 参与。

---

## 一、核心结论

**nofx 不应简单从“AI 决策”切换成“纯规则交易”，而应改成“AI 生成交易想法，代码执行准入审查与生命周期风控”。**

当前亏损暴露出来的主要问题，不是 AI 完全没有价值，而是：

- AI 会把风险提示当作“建议”，而不是“不可违反的约束”。
- AI 会为了满足趋势叙事，弱化 RSI、结构位、回撤、盈亏比等风险信号。
- prompt 很难保证每一次都稳定执行同一套纪律。
- 已经能明确量化的边界，应由代码强制执行，而不是让 AI 自觉遵守。
- 规则命中后如果没有留痕、追踪和复盘，系统会变成“规则越加越多，但不知道是否有效”。

因此更合理的边界是：

```text
AI 负责：解释市场、识别异常、给方向倾向、生成候选交易
代码负责：拒绝坏交易、验证 SL/TP/R:R、控制仓位、执行退出纪律
治理负责：记录规则命中、追踪后续表现、评估误杀/漏杀、推动参数迭代
```

这不是“AI vs 代码”，而是“AI 产出假设，代码做交易准入和风控审计”。

---

## 二、哪些内容应该从 AI 自觉迁移到代码约束？

从日志和决策记录中可以看到，AI 的 reasoning 经常包含一些本质上可量化的判断：

| AI 当前的决策 | 是否适合代码化 | 更合理的代码角色 |
|---|---|---|
| "ADX=47，趋势很强，追空" | ⚠️ 部分适合 | ADX 只表示趋势强度，不代表方向；代码可做趋势强度过滤，但不能单独作为开仓理由 |
| "RSI7=23 超卖，但趋势未结束，继续空" | ✅ 适合做禁区 | 极端 RSI 可作为 hard block 或仓位降级条件 |
| "R:R > 1.5，满足入场条件" | ✅ 必须代码化 | 代码直接计算 entry/SL/TP 的 R:R，并校验 TP 是否有结构锚点 |
| "已经 3 连亏，减少交易频率" | ✅ 必须代码化 | 由 `consecutive_loss_brake` 或方向/币种级刹车强制执行 |
| "浮盈后是否该保本？" | ✅ 必须代码化 | 由 `breakeven_protection` 与 `drawdown_close` 执行状态机 |
| "BTC/ETH/SOL 哪个更有机会？" | ⚠️ 适合辅助代码化 | 代码可做 ranking 和候选池过滤，AI 负责解释异常原因 |
| "这是趋势末端还是中继？" | ❌ 不宜完全代码化 | 需要综合结构、情绪、流动性，AI 仍有优势 |

关键区别在于：**代码不一定要替 AI 做完整交易决策，但必须阻止 AI 做明显违反纪律的交易。**

---

## 三、AI 决策中哪些层级适合代码化？

### 1. 已经由代码强制执行的部分

- 杠杆、仓位上限、保证金使用率
- 最大持仓数、最小 confidence
- `consecutive_loss_brake`
- `breakeven_protection`
- `drawdown_close`
- 止损触发后的 close reason 标记

这些已经是代码在兜底，方向是正确的。

### 2. 应优先代码化的部分：交易禁区与风控状态机

这是当前最值得继续推进的方向。

#### a) 入场禁区

入场禁区不是完整的开仓策略，而是用于拒绝明显差的候选交易。例如：

- 开空时 1h RSI7 极低或 RSI14 极低。
- 开多时 1h RSI7 极高或 RSI14 极高。
- 当前价过于接近支撑/阻力，真实 TP 空间不足。
- 过渡市中主周期与 1h 信号明显冲突。

这类规则适合作为 `entry_risk_guard`，因为它们解决的是“这笔交易不该做”，而不是“这笔交易一定该做”。

#### b) SL/TP/R:R 校验

R:R 不能只看数字，还要看目标价是否有市场结构依据：

- 做空 TP 应靠近可验证支撑、前低、BOLL 下轨或近期 swing low。
- 做多 TP 应靠近可验证阻力、前高、BOLL 上轨或近期 swing high。
- TP 如果明显越过最近结构位，需要有突破确认，否则应拒绝或降级。

这能避免 AI 为了凑出漂亮的 R:R，把 TP 放到没有依据的远端位置。

TP 锚点不应第一版就做成强结构位系统，建议分阶段：

| 阶段 | 做法 | 说明 |
|---|---|---|
| Phase 1 | 用最近 N 根 K 线 high/low、BOLL 边界、ATR 容差做粗锚点 | 复用现有行情数据，先检测明显外推 |
| Phase 2 | 引入 supports/resistances/swing high/swing low 结构位服务 | 数据稳定后再做精确校验 |

第一版更适合做 `warn_reduce` 或“要求 AI 提供 breakout evidence”，而不是直接把所有超出前高/前低的 TP 硬拒绝。

#### c) 退出状态机

退出纪律比入场更适合代码化：

- 到达保本触发阈值后自动抬 SL。
- 达到峰值浮盈后，如果回撤超过阈值则自动保护利润。
- 持仓超过 N 个周期没有进展，则减仓或退出。
- 趋势结构被破坏时触发退出审查。

AI 可以提供“趋势是否衰竭”的判断，但不应决定是否绕过这些硬规则。

### 3. 可以辅助代码化，但不宜直接替代 AI 的部分

#### a) 入场信号

趋势方向、ADX、EMA、MACD、RSI、多周期共振都可以写成规则，但这不代表应该立刻让代码独立开仓。

更稳妥的路径是：

```text
AI 给出候选方向和理由
↓
代码校验是否进入风险禁区
↓
代码校验 SL/TP/R:R 是否可信
↓
通过后才允许执行
```

这样保留 AI 对复杂结构的解释能力，同时避免它绕过纪律。

#### b) 币种选择

代码可以根据 quant ranking、momentum score、成交量、OI、资金费率等指标生成候选池。

但 AI 仍可用于：

- 解释某个币为什么异常强/弱。
- 识别公告、事件、流动性异常。
- 判断是否需要临时黑名单。

也就是说，币种选择可以从“AI 全权挑选”改成“代码给候选池，AI 做解释和异常过滤”。

### 4. 仍应保留 AI 的部分

- 市场结构综合判断：趋势末端、中继、假突破、流动性扫单。
- 异常环境识别：消息驱动、宏观事件、交易所公告、链上大额转账。
- 多因子权衡：指标冲突时，解释哪个信号更重要。
- 叙事一致性：判断多周期结构是否支持同一方向。

这些判断很难用少数 if/else 覆盖，适合由 AI 参与，但 AI 的输出必须经过代码层审查。

---

## 四、为什么把纪律性交给代码会更好？

### 1. 代码不会把约束当建议

prompt 可以写“RSI 极度超卖时谨慎追空”，但 AI 仍可能因为趋势很强而继续开空。

代码规则则可以直接表达：

```text
if side == short && rsi_1h_7 < threshold:
    block_or_reduce()
```

这类规则不需要 AI 解释，也不应该交给 AI 临场发挥。

### 2. 代码不会为了叙事牺牲风险边界

AI 很容易形成一个连贯叙事：

```text
趋势强 → 继续追随 → 回调只是中继 → R:R 看起来达标
```

但交易系统需要先问：

```text
是否极端超卖？
是否接近支撑？
TP 是否真实可达？
如果错了，亏损是否可控？
```

这些问题更适合由代码逐项检查。

### 3. 代码执行退出更一致

AI 对“是否平仓”的判断容易受当时 reasoning 影响。代码可以稳定执行：

- 浮盈达到阈值后进入保护状态。
- 峰值回撤超过阈值后保护利润。
- 达到最大持仓时间后触发退出审查。
- 连续亏损后降低交易频率。

这能减少“明明已经浮盈，最后又变大亏”的生命周期问题。

### 4. 代码规则可回测、可调参

prompt 的行为很难精确回测；代码规则可以被历史数据检验。

因此每新增一个规则，都应尽量支持：

- paper trading 对照组。
- 参数版本记录。
- 拒绝/降级原因日志。
- 按规则维度统计命中率与后续收益。

---

## 五、但完全不用 AI 也会有问题

如果全用代码，会失去两类能力。

### 1. 非结构化信息整合

例如：

- 链上大额转账
- 交易所公告
- 社交媒体情绪
- 宏观事件
- 新闻突发

这些可以通过数据源或插件喂给 AI，让 AI 做事件影响评估。

### 2. 复杂结构识别

例如：

- 这是 bear flag 还是 bottoming wedge？
- 当前是 liquidity grab 还是真突破？
- 为什么某个币在大盘下跌时明显抗跌？
- 多周期冲突时，哪一个周期更值得相信？

这些问题可以被量化辅助，但很难完全依靠固定规则覆盖。

---

## 六、推荐混合架构：AI 产出，代码审查，风控执行

```text
┌─────────────────────────────────────────┐
│  Layer 4: 数据与候选池                   │
│  - 行情、指标、排名、OI、资金费率          │
│  - 结构位、BOLL、swing high/low          │
│  输出：候选标的、结构上下文、风险上下文     │
└─────────────────────────────────────────┘
                    ↓
┌─────────────────────────────────────────┐
│  Layer 3: AI 假设层                      │
│  - 市场结构综合判断                       │
│  - 异常事件解读                           │
│  - 多周期叙事一致性                        │
│  输出：direction_bias, confidence, idea   │
└─────────────────────────────────────────┘
                    ↓
┌─────────────────────────────────────────┐
│  Layer 2: 规则审查层                      │
│  - entry_risk_guard                       │
│  - SL/TP/R:R/TP 锚点校验                  │
│  - 币种/方向/时段禁区                      │
│  输出：allow / reduce / block + guard log │
└─────────────────────────────────────────┘
                    ↓
┌─────────────────────────────────────────┐
│  Layer 1: 生命周期风控层                  │
│  - 仓位、杠杆、margin、最大持仓            │
│  - BE、drawdown、trailing、time stop       │
│  - 连亏刹车、对冲检测、退出状态机           │
│  输出：execute / modify / close           │
└─────────────────────────────────────────┘
```

### 风控治理原则

规则审查层不能只输出 `allow / reduce / block`，还必须定义命中后的治理边界。

#### 1. 不可 override 的硬安全规则

以下规则属于资金安全底线，不能被 AI、prompt 或普通策略配置绕过：

- 杠杆上限。
- 最大仓位和最大持仓数。
- margin 使用率。
- 缺失 SL / TP。
- TP 在入场价反方向。
- R:R 低于硬下限。
- 交易所最小下单额、精度、账户余额不足等执行安全约束。

这些规则一旦触发，只能 `block` 或转换为 `wait`，AI 不允许通过“重新解释 reasoning”绕过。

#### 2. 可参数化但必须留痕的规则

以下规则可以通过策略配置调整，但不能由 AI 临场 override：

- RSI 极端阈值。
- BOLL/ATR buffer。
- TP 外推容差。
- R:R soft floor。
- 连亏冷却阈值。
- time stop / trailing stop 参数。

参数调整后应记录配置版本，并通过 paper trading 或回测观察效果。

#### 3. AI 的有效权限

AI 可以：

- 放弃交易。
- 降低 confidence。
- 给出候选方向和 TP/SL rationale。
- 解释是否存在 breakout evidence。
- 对异常环境提出人工复核建议。

AI 不可以：

- 绕过 hard block。
- 在 live 决策里修改风控参数。
- 因为趋势叙事强而忽略已命中的禁区。
- 将被 block 的交易重新包装成另一个 open decision。

### Prompt 改造要求

现有 prompt 不应只提醒 AI “谨慎”，而应要求 AI 输出可被代码和复盘系统消费的结构化字段。

建议后续单独产出 `docs/plans/ai-guard-prompt-refactor.md`，至少固定这些字段：

```json
{
  "direction_bias": "long | short | neutral",
  "market_regime": "trend | range | transition | high_volatility",
  "risk_signals": [
    "extreme_rsi",
    "near_support_resistance",
    "transition_market",
    "tp_extension"
  ],
  "guard_awareness": {
    "known_entry_risk": true,
    "risk_can_be_overridden_by_ai": false
  },
  "tp_rationale": {
    "anchor_type": "recent_high_low | boll_band | support_resistance | breakout_extension",
    "anchor_price": 0,
    "breakout_evidence": []
  }
}
```

代码层不应信任这些字段做最终风控，但可以用它们做：

- AI 自检。
- prompt 质量评估。
- AI 判断与代码 guard 的差异分析。
- 后续复盘归因。

### 规则命中遥测与看板

每次规则审查都应记录事件，而不仅仅把交易改成 `wait`。

建议事件字段：

```json
{
  "decision_id": "...",
  "trader_id": "...",
  "strategy_id": "...",
  "guard_type": "entry_risk_guard | rr_check | tp_anchor | cooldown | lifecycle_exit",
  "action": "allow | reduce | block",
  "reason": "...",
  "symbol": "BTCUSDT",
  "side": "short",
  "position_size_before": 1000,
  "position_size_after": 500,
  "config_version": "risk-v3",
  "timestamp": 0
}
```

后续看板至少统计：

- 每个 guard 的命中次数。
- `block / reduce / allow` 占比。
- 被 block 信号的后续最大有利/不利波动。
- 被 reduce 信号的实际 PnL。
- 误杀率：被 block 后本来会盈利的比例。
- 漏杀率：未被 block 但后续亏损的比例。
- 按 symbol、side、regime、策略版本拆分的命中表现。

这一步很关键：没有遥测，规则只会越加越多；有了遥测，规则才能被治理和淘汰。

### 落地顺序建议

**第一步：把入场禁区代码化**

- 已落地方向：`entry_risk_guard`
- 重点不是让代码判断“该买什么”，而是让代码拒绝“明显不该买/卖”的交易。

**第二步：补规则命中遥测**

- 为 `entry_risk_guard`、R:R、TP 外推、冷却机制记录 guard event。
- 追踪被 `block/reduce` 的信号后续走势。
- 先有数据，再继续扩大规则范围。

**第三步：改造 prompt 输出字段**

- 要求 AI 显式输出 `risk_signals`、`market_regime`、`tp_rationale`。
- 让 AI 先自检是否触及禁区。
- 明确 AI 不能 override hard block。

**第四步：把候选池 ranking 约束提前**

- 用 ranking、momentum、volume、OI、funding rate 生成候选列表。
- AI 只在候选池内解释、排序、否决异常。
- 至少先把 quant score 作为 AI 选币的输入约束，避免 AI 全市场自由挑逆势币。

**第五步：把退出纪律代码化**

- 完善 `breakeven_protection`
- 完善 `drawdown_close`
- 增加 `trailing_stop`
- 增加 `time_stop`
- 评估 `structure_break_exit`

**第六步：把 TP 可信度代码化**

- Phase 1：先用最近 N 根 K 线 high/low、BOLL、ATR 做粗锚点。
- Phase 2：再引入 supports/resistances/swing high/swing low。
- 对无锚点外推 TP 先 `warn_reduce`，有足够证据后再考虑 hard block。

**第七步：用回测和 paper trading 验证参数**

- 每个规则都要记录命中原因和后续表现。
- 对比被 block/reduce 的交易后续表现。
- 避免凭直觉越加越多规则，最后形成过拟合系统。

---

## 七、直接回答

> AI 决策里有多少可被代码替代？

不建议直接用固定比例下结论。更稳妥的说法是：

- **纪律性边界应尽量代码化**：仓位、杠杆、R:R、SL/TP、入场禁区、退出状态机。
- **Alpha 判断不应完全代码化**：趋势末端/中继、异常事件、复杂结构、多因子冲突。
- **候选池和评分可以逐步代码化**：但需要 paper trading 和回测验证。

> 效果会相差多少？

未经系统回测前，不应给出确定收益提升数字。

更可靠的预期是：

- 最大回撤应下降。
- 明显劣质交易数量应下降。
- 浮盈回吐成亏损的比例应下降。
- 交易频率可能下降。
- 捕捉极端大行情的能力可能降低，需要由 AI 的结构判断补回来。

目标不是让代码替代所有 AI，而是让 AI 不能绕过风控。

---

## 八、下一步建议

1. 新增 `entry_risk_guard` / R:R / cooldown 的 guard event 记录。
2. 在 `docs/plans/` 中新增 `ai-guard-prompt-refactor.md`，固定 AI 输出字段。
3. 在 `docs/plans/` 中新增 `entry-risk-guard-telemetry-design.md`，定义事件表、看板和误杀/漏杀口径。
4. 把候选池 ranking 约束提前，让 AI 在代码筛选后的候选池内做解释和排序。
5. TP 锚点先做粗锚点 soft guard，再等结构位服务成熟后升级。
6. 继续完善 `trailing_stop` 与 `time_stop`，补齐退出生命周期。
