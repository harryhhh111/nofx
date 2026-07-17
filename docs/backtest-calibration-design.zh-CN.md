# 回测校准设计

## 目标

回测校准是策略版本上线前的质量闸门。它不直接修改运行中的交易员，也不让 AI 在交易循环里临场调参。

正确链路：

```text
策略草稿 -> 结构化编译 -> 参数回放 -> 模拟盘验证 -> 用户手动启用 -> 实盘运行
```

策略进化链路：

```text
实盘/模拟盘交易记录 -> 复盘统计 -> 生成优化提案 -> 回测校准 -> 用户应用为新版本
```

## 数据要求

只记录成交结果不足以做严谨校准。系统必须记录三类数据。

### 1. 市场计算证据

用于证明信号从哪里来：

- 策略 ID 和策略版本。
- 币种、周期角色：主周期、入场周期、确认周期。
- 代码计算后的 `FactorSnapshot`。
- setup 判定结果。
- scoring 判定结果。
- 信号生成时间和价格。

这些数据用于回答：

- 当时为什么产生信号？
- 哪个 setup 触发？
- 哪些因子缺失？
- 多周期是否方向冲突？
- 调整阈值后，这个样本会不会被过滤？

### 2. 确定性复核与风控证据

用于证明信号为什么通过或被拒绝：

- Evidence Review 状态：pass、warn、reject。
- Evidence Review 原因。
- Risk Gate 状态：approved、risk_rejected、no_signal。
- Risk Gate 拒绝原因。
- 当时市场上下文摘要。

这些数据用于区分：

- 代码没有产生机会。
- 代码产生了机会但证据复核拒绝。
- 证据复核通过但风控拒绝。
- 最终进入执行。

### 3. 执行与结果证据

用于证明交易质量：

- 订单、成交、滑点、手续费。
- 实际开仓价、平仓价。
- 持仓时间。
- 真实盈亏。
- 最大浮盈、最大浮亏。
- 平仓原因。

这些数据用于评估：

- setup 胜率。
- 盈亏比。
- 最大回撤。
- 执行质量是否拖累策略。
- 某个参数变化是否真的改善表现。

## 当前第一版实现

当前使用 `signal_calibration_samples`、`setup_episodes` 和 `calibration_klines` 记录可复现的校准数据。

它记录：

- 独立 setup episode，包括未触发和未执行的机会。
- signal 样本，包括被确定性 Evidence Review 或 Risk Gate 拒绝的信号。
- factor snapshot。
- setup/scoring trace。
- 确定性 evidence review 结果。
- risk gate 结果。
- market context。
- decision_id 和 cycle_number。

同一根主周期 K 线的重复扫描只累计 repeat count，不增加独立样本数。历史 K 线按 source、symbol、timeframe、open time 去重存储，回放时按原始 cutoff 重建窗口。

## 后续升级

### 阶段一：参数回放

输入策略版本和历史样本，输出：

- 样本数是否足够。
- setup 分布。
- approved / rejected / no_signal 数量。
- setup 结构分类与基线匹配率。
- 是否允许进入模拟盘。

### 阶段二：episode 统计与参数扫描

仅扫描少数核心参数：

- long_threshold。
- short_threshold。
- min_confidence。
- min_available_weight_ratio。
- 结构 lookback、最小 leg、breakout buffer。
- setup-aware 证据权重和阈值。

输出推荐范围，而不是自动应用。

### 阶段三：完整 Walk-forward 回测

按时间滚动：

```text
训练窗口 -> 验证窗口 -> 下一轮
```

只有跨窗口稳定的参数才允许进入上线候选。

## 强约束

- 回测校准不能直接修改正在运行的交易员。
- AI 只能解释报告或生成 proposal。
- 参数变更必须由用户手动应用为新策略版本。
- 样本不足时不能给出强结论。
- 运行时交易循环不能因为回测结果自动调参。
- 参数回放不包含完整成交、手续费和滑点模拟，不能冒充收益回测。
