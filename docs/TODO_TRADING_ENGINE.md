# Trading Engine TODO

## Core Direction

目标是把实时交易与 AI 服务彻底解耦，同时保留 AI 在显式离线设计环节的辅助价值。

最终方向：
- AI 可做策略编译和离线参数建议。
- 程序做指标计算、结构识别、信号触发、风控校验。
- 实盘每轮不调用 AI，也不能让 AI 临时重写交易规则和参数。
- 如果 AI 需要调整参数，必须生成新的策略版本，不能静默覆盖当前实盘策略。

交易主链路：

`strategy config -> factor snapshot -> setup engine -> deterministic evidence review -> market context -> risk gate -> execution`

## Status Audit

这次复查发现：部分模块已经跑通主链路，但标题写成 `done` 容易误导。后续按下面口径执行：
- `done`：当前目标闭环完成，可以进入使用。
- `core done`：主链路完成，但还有明确的完整性回补。
- `partial`：只是中间态，不能算完成。

当前需要继续回补的模块：
- Strategy Evolver：core done，缺 proposal 存储、显式 apply、自动触发器。
- Execution Analytics：core done，缺 OrderSync 成交回填和撤单/替单统计。
- Strategy Studio UI：core done，缺结构参数编辑、proposal、memory、execution analytics 入口。
- Grid Engine：已从中间态修正为确定性 rule engine + market context + risk gate。

## 1. Strategy Prompt Compiler - done

目标：把用户自然语言策略编译成可执行配置。

已完成：
- `strategy_prompt` 已进入策略配置。
- `compiled_rules` 已包含 `execution` 参数：杠杆、仓位、止损百分比、止盈百分比、置信度。
- 新增后端编译 API：`POST /api/strategies/compile`。
- 规则编译结果会做硬校验；开仓规则缺少必要执行参数会直接报错。
- 信号生成已从规则执行参数生成完整 `CandidateSignal`。

已完成回补：
- 编译器已增加指标白名单、结构字段白名单、外部因子白名单。
- 编译器支持两种输出：`compiled_rules` 和 `scoring_config`。
- 编译提示词已明确：Fibonacci / support / resistance 由结构引擎支持。
- 编译结果会返回 `resolved_parameters`；持久化时会保存评分模式的最终生效参数。

## 2. Indicator Functions - done

目标：K 线可计算指标由程序统一计算，不让 LLM 从原始 K 线临场计算。

已完成：
- `IndicatorRequest` 支持 EMA、RSI、ATR、BOLL、MACD、VWAP、Volume、Donchian、Price Change、Realized Vol。
- `DefaultIndicatorEngine` 统一计算上述指标并写入 `FactorSnapshot.Technical`。
- `TimeframeSeriesData` 保留完整计算窗口 `ComputeBars`，但不会暴露到 AI JSON。
- 指标缺少必要 K 线数量时直接报错，不静默降级。

已完成回补：
- MACD、Volume、VWAP、Donchian、Realized Vol、Price Change 的参数已进入策略配置。
- `IndicatorRequestFromStrategyConfig` 已改为从策略配置生成，不再写死这些指标参数。
- 前端类型已补齐上述参数字段。

后续：
- 前端策略页面可以继续增加这些新参数的可视化编辑控件。

## 3. Structure Factors - done

目标：结构类指标由程序识别，不交给 LLM 肉眼判断。

已完成：
- `DefaultStructureEngine` 已实现 Fibonacci 结构识别。
- Fibonacci anchor 由程序基于 swing high / swing low、ATR 阈值、最小波段长度、最小涨跌幅确定。
- 已实现 Fibonacci 回撤位：0.236、0.382、0.5、0.618、0.786。
- 已实现支撑阻力识别：swing 点聚类、ATR 区间宽度、最小触达次数、最小间隔。
- 结构因子写入 `FactorSnapshot.Structures`。
- 规则条件可以读取 `valid`、`invalid_price`、`support`、`resistance`、`fib_0_618`。

已完成回补：
- 结构参数已进入策略配置：lookback、swing_window、ATR 阈值、最小触达次数。
- Fibonacci / support resistance 的最终生效参数已写入 `resolved_parameters`。
- `StructureRequestFromStrategyConfig` 已改为从配置生成，不再写死在交易分析代码里。
- `compute_lookback` 会自动覆盖结构识别需要的 lookback，避免结构参数大于实际抓取窗口。

后续：
- 用历史回放验证结构参数质量。

## 4. Scoring Strategy Mode - done

目标：支持用户只指定指标类型，但不指定明确条件的策略。

边界：
- AI 可以在策略生成阶段推荐指标参数、权重、评分阈值。
- 程序必须校验参数边界并保存为 `scoring_config`。
- 实盘每轮不能让 AI 临时改变参数。

已完成：
- `strategy_mode`: `rule` / `scoring` / `hybrid`。
- `scoring_config`: `selected_factors`、`factor_weights`、阈值、执行参数。
- `resolved_parameters.scoring`：记录最终生效评分参数。
- `SetupSignalEngine`：先消费结构化 setup，再把评分作为证据强弱和冲突过滤。
- `CompositeSignalEngine`：合并 Rule Mode 和 Scoring Mode 的信号。
- 编译器支持输出 `compiled_rules` 或 `scoring_config`。
- scoring factor 已做白名单校验；未知 factor 或缺少正权重会直接报错。
- scoring 选择 `structure` 时会自动启用结构因子计算，避免评分项和计算层脱节。

后续：
- 评分公式只是第一版确定性规则，目前用于跑通模式和边界。
- 权重、阈值、因子方向必须进入历史回放校准，不允许直接当成最终交易质量结论。

## 5. Market Context Engine - done

目标：由程序统一计算大环境状态，并按 setup 类型用于环境过滤和风险审查。

已完成：
- BTC / ETH 大盘趋势摘要。
- 全市场风险状态：risk_on、risk_off、chop、high_volatility、overheated。
- Funding 是否过热。
- 基于候选币快照的市场广度摘要。
- 输出 `market_regime`、`risk_flags`、`context_summary`。
- Market Context 已进入确定性证据复核和 RiskGate。
- RiskGate 继续复用老风控做硬校验：仓位、杠杆、最小仓位、RR、止损距离等。
- Market Context 采用 setup-aware 分层处理：方向冲突可硬拦截趋势类 setup；反转类 setup 只增加 warning，并继续由结构失效和风控校验决定。

使用边界：
- 可以降级、警告、否决信号。
- 不可以凭外部信息直接创造交易。
- 不可以临时重写策略参数。

后续：
- OI Ranking / NetFlow / Quant Data / News / Announcement / Macro Event 进入结构化 context。

## 6. External Factors - done

目标：把 K 线算不出来的外部数据统一进入 `FactorSnapshot.External`。

已完成：
- OI
- Funding
- OI Ranking
- NetFlow Ranking
- Price Ranking
- Quant Data：Quant OI、Quant NetFlow、Quant Price Change

已接入：
- 主交易链路会把 `Context.QuantDataMap / OIRankingData / NetFlowRankingData / PriceRankingData` 写入 `FactorSnapshot.External`。
- 策略测试预览会复用现有 StrategyEngine 拉取这些数据，并写入 factor snapshot。
- 每个外部因子记录 `source`、`source_time`、`available_at`、`cost_class`。
- 基础 OI / Funding 会保留交易所返回时间；Funding 拉取失败时标记 `available=false`，不把 0 伪装成中性资金费率。
- Ranking / NetFlow / Price 外部因子已进入 `MarketContext` 汇总。

要求：
- 每个外部因子必须记录 source、source_time、available_at、cost_class。
- 外部数据必须先结构化，不允许由运行时 AI 从长文本临场生成信号。

后续：
- News / Announcement / Macro Event。

## 7. Strategy Evolver - core done

目标：保留 AI 的长期自适应能力，但通过策略版本迭代实现，而不是每轮临场改规则。

触发方式：
- 用户手动优化。
- 每日 / 每周复盘。
- 连续亏损。
- 市场 regime 持续变化。

已完成：
- 新增 `StrategyEvolutionProposal`。
- 新增 `LLMStrategyEvolver`。
- 新增 `POST /api/strategies/:id/evolve`。
- 该接口只生成建议，不保存、不激活、不应用到实盘。
- trigger 已限制为 `manual`、`daily_review`、`weekly_review`、`loss_streak`、`regime_shift`。
- proposal 必须包含摘要；如果包含配置 patch，必须同时给出参数变更说明。
- proposal 禁止 patch 运行态字段，例如 active/status/id/user_id/时间字段。
- 前端 API 类型已补齐。

输出：
- 新策略版本建议。
- 参数调整建议。
- 原因说明。
- 预期影响。

限制：
- 不能静默替换当前实盘策略。
- 必须记录旧版本、新版本、变更原因。

后续：
- 增加 proposal 存储表。
- 增加显式 apply proposal 流程。
- 接入每日 / 每周复盘、连续亏损、regime 持续变化的自动触发器。

## 8. Setup Episode Calibration - done

目标：将连续多轮扫描归并成独立的结构机会，形成可复现、可回放、可统计的学习样本。

已完成：
- 新增 `setup_episodes`，按策略版本、币种、setup 和方向记录机会生命周期。
- 同一根已收盘 K 线的重复扫描不会增加独立样本数。
- 记录首次结构证据、触发、风控通过、执行、MFE/MAE、结构失效和目标命中。
- 未执行或被拒的机会也会继续跟踪前向结果，用于反事实校准。
- 按 regime、setup、方向和因子聚合前向表现，供离线权重校准使用。
- AI 只生成离线校准草稿，不进入实时交易门，也不会自动覆盖运行配置。

边界：
- ATR 执行缓冲不改变结构标签的止损锚点和目标位。
- 无结构风险基准的 episode 不参与平均 R，避免混合不同统计口径。
- 参数回放是结构分类稳定性扫描，不等同于收益回测。

## 9. Execution Analytics - core done

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

已完成：
- 新增 `execution_analytics` 存储表。
- 实盘下单前记录真实信号生成时间、订单提交时间、盘口 best bid/ask、spread bps、预期滑点。
- 下单失败会记录 `failed` 和错误原因。
- 对同步确认成交的交易，记录成交时间、成交均价、成交数量、部分成交比例、真实滑点。
- 新增 `GET /api/execution-analytics`，用于查看 trader 的执行质量记录。

边界：
- 当前第一版覆盖主交易链路的市价开/平仓。
- 使用异步 OrderSync 的交易所，第一版先记录提交与盘口，成交质量后续需要从同步模块回填。
- veto / cooldown reason 和撤单 / 替单次数暂未接入，需要后续和订单管理模块、RiskGate 打通。

后续：
- OrderSync 回填 `first_fill_at`、`final_fill_at`、`avg_fill_price`、`partial_fill_ratio`。
- 统计每个交易所、币种、策略版本的平均滑点和失败率。
- 把执行质量摘要按策略版本、setup 和交易所汇总到校准报告。

## 10. Strategy Studio UI - core done

目标：前端从旧 Prompt 编辑器切换为结构化策略工作台。

需要实现：
- 策略 Prompt 输入。
- 编译按钮。
- AI 模型选择。
- Rule Mode / Scoring Mode / Hybrid 展示。
- `compiled_rules` 预览。
- `scoring_config` 预览。
- `resolved_parameters` 展示。
- 编译错误展示。
- flow/test-run 结构化结果展示。

已完成：
- 策略编辑区新增 `策略 Prompt 编译`。
- 支持选择 AI 模型后调用 `/api/strategies/compile`。
- 编译成功后写回当前编辑配置：`strategy_prompt`、`strategy_mode`、`compiled_rules`、`scoring_config`、`resolved_parameters`。
- 编译失败时会在结构化页签展示后端返回的结构化错误。
- 右侧新增 `结构化` 页签，展示规则数、策略模式、证据过滤、规则摘要和原始结构。
- 保留 `preview-flow` 和 `test-run`，用于查看与生产一致的确定性结构化流和风控结果。

边界：
- 编译结果不会自动变成实盘，仍然需要点击保存策略。
- 运行时不允许 AI 临时改参数，前端只负责生成和查看结构化配置。

后续：
- 增加结构参数编辑器：Fibonacci / support resistance。
- 增加 strategy evolver proposal 的前端查看和手动应用流程。
- 增加 execution analytics 的前端查看入口。

## 11. API Compatibility - done

目标：对外接口尽量复用旧路径，只替换内部逻辑，减少外部调用方断裂。

已完成：
- 保留 `POST /api/strategies/preview-prompt`，内部复用结构化预览。
- 保留 `POST /api/strategies/preview-flow` 作为新语义接口。
- 预览接口兼容 `{"config": ...}` 和直接传 `StrategyConfig` 两种请求格式。
- 响应中返回 `legacy_compatible` 和 `replacement_endpoint`，明确旧路径只是兼容入口。
- 明确废弃旧的长 Prompt 预览，预览结果只展示结构化链路和已编译配置。

后续：
- 明确废弃 trader 级 prompt 覆盖接口，如果仍有路由或前端入口，需要迁移到 strategy compile / preview flow。

## 12. Grid Engine Decision - done

目标：决定 grid 是否保留独立 LLM Prompt 流，或迁入新的结构化交易流。

结论：
- Grid 不并入普通交易信号引擎；它是挂单网格执行，不是单次开仓信号。
- 实盘 grid 每轮不再调用 LLM 直接决定挂单。
- 新增确定性 `GetGridRuleDecisions`，只根据 grid config、grid state、价格位置生成挂单/hold 动作。
- 新增 `BuildGridMarketContext`，把 grid 的趋势、波动、资金费率、区间状态输出成结构化市场上下文。
- 新增 `ApplyGridRiskGate`，在执行前拦截趋势风险、高波动、价格越界等不适合 grid 加挂单的状态。
- 旧 `GetGridDecisions`、grid prompt 和响应解析器已删除，实盘 grid cycle 只保留确定性规则路径。

后续：
- Grid 的趋势暂停、方向调整、区间重算应继续走确定性规则。
- 如果要让 AI 优化 grid，只能生成配置调整 proposal，不能在 live cycle 临时改订单。
