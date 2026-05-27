# Trading Engine TODO

## 1. Strategy Prompt Compiler - done

目标：把用户自然语言策略编译成可执行的 `compiled_rules`，并保证规则包含完整交易参数，不允许缺字段后静默兜底。

已完成：
- `strategy_prompt` 已进入策略配置。
- `compiled_rules` 已包含 `execution` 参数：杠杆、仓位、止损百分比、止盈百分比、置信度。
- 新增后端编译 API：`POST /api/strategies/compile`。
- 规则编译结果会做硬校验；开仓规则缺少必要执行参数会直接报错。
- 信号生成已从规则执行参数生成完整 `CandidateSignal`。

## 2. Indicator Functions - done

目标：把 K 线可直接计算的常用指标沉淀成统一指标函数，由程序计算后进入 `FactorSnapshot`，不再让 LLM 从原始 K 线临场计算。

已完成：
- `IndicatorRequest` 支持 EMA、RSI、ATR、BOLL、MACD、VWAP、Volume、Donchian、Price Change、Realized Vol。
- `DefaultIndicatorEngine` 统一计算上述指标并写入 `FactorSnapshot.Technical`。
- `TimeframeSeriesData` 保留完整计算窗口 `ComputeBars`，但不会暴露到 AI JSON。
- 主交易链路按策略配置生成 `IndicatorRequest`，指标缺少必要 K 线数量时直接报错，不做静默降级。

## 3. Structure Factors - done

目标：实现需要结构定义的因子，尤其是支撑阻力、有效波段、Fibonacci anchor。

已完成：
- `DefaultStructureEngine` 已实现 Fibonacci 结构识别。
- Fibonacci anchor 由程序基于 swing high / swing low、ATR 阈值、最小波段长度、最小涨跌幅确定。
- 已实现 Fibonacci 回撤位输出：0.236、0.382、0.5、0.618、0.786。
- 已实现支撑阻力识别：基于 swing 点聚类、ATR 区间宽度、最小触达次数、最小间隔。
- 结构因子写入 `FactorSnapshot.Structures`。
- 规则条件可以读取结构字段，例如 `valid`、`invalid_price`、`support`、`resistance`、`fib_0_618`。
- 参数不足、K 线不足、ATR 不可得时直接报错；找不到有效结构时返回 `valid=false` 和明确原因。

## 4. External Factors

目标：把 K 线算不出来的外部数据统一进入 `FactorSnapshot.External`。

范围：
- OI
- Funding
- OI Ranking
- NetFlow Ranking
- Price Ranking
- Quant Data

## 5. Trade Memory

目标：每笔交易结束后形成结构化经验，并在后续决策中引用。

范围：
- 交易总结存储。
- 经验检索。
- 成败归因。
- 记忆质量评分和过期机制。

## 6. Execution Analytics

目标：补齐真实执行质量指标，区分策略问题和执行问题。

范围：
- signal generated at
- order submitted at
- first/final fill at
- spread
- slippage
- partial fill ratio
- cancel/replace count
- veto/cooldown reason

## 7. Strategy Studio UI

目标：前端从旧 Prompt 编辑器切换为结构化策略编辑器。

范围：
- 策略文本输入。
- 编译按钮。
- `compiled_rules` 预览。
- 编译错误展示。
- flow/test-run 结构化结果展示。

## 8. API Compatibility

目标：对外接口尽量复用旧路径，只替换内部逻辑，减少外部调用方断裂。

范围：
- 保留 `POST /api/strategies/preview-prompt`，内部复用结构化预览。
- 保留 `POST /api/strategies/preview-flow` 作为新语义接口。
- 明确废弃 trader 级 prompt 覆盖接口。

## 9. Grid Engine Decision

目标：决定 grid 是否保留独立 LLM Prompt 流，或迁入新的结构化交易流。
