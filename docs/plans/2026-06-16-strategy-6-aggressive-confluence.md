# 策略 6：激进动量聚合（Aggressive Momentum Confluence）

> 基于用户对高频、高盈亏比、不收紧入场的需求，设计一个更激进的策略。核心思想：**不减少交易次数，而是提高每笔交易的 conviction 评分，并通过动态仓位把高 conviction 机会放大**。

---

## 一、设计原则

### 1. 不收紧入场，而是提高 conviction

传统做法是加过滤条件减少交易，但用户明确需要更多数据。所以策略 6 的做法是：

- **保留多个入场 setup**（趋势突破、回调、均值回归 snap）
- **每个 setup 用 confluence 评分**（0-100）
- **评分越高，仓位越大**；评分低时仍然可以交易，但仓位小
- 这样既能产生大量交易数据，又能避免低质量机会造成大亏

### 2. 低胜率 + 高盈亏比

目标不是提高胜率，而是：

- 错的时候快速小亏（严格止损 + 时间止损）
- 对的时候让利润奔跑（移动止损 + 浮盈加仓）
- 盈亏比目标 **≥ 2.0**，胜率可以容忍 **35%-45%**

### 3. 更激进的仓位和杠杆

| 参数 | 趋势 5-2 | 策略 6 |
|---|---|---|
| max_positions | 3 | 6 |
| BTC/ETH leverage | 3x | 5x |
| Altcoin leverage | 3x | 4x |
| max_margin_usage | 0.5 | 0.8 |
| 单笔风险 | 固定 | 动态 0.3% - 1.0% |
| min R:R | 1.5 | 1.2 |

---

## 二、三套入场 Setup

### Setup A：趋势突破（Trend Breakout）

**适用市场状态**：趋势市（ADX > 25）

**做多条件**：
1. 价格突破 15m 前高或布林带中轨向上
2. EMA20 > EMA50（多头排列）
3. ADX > 25 且 +DI > -DI
4. 成交量/OI 同步放大（确认资金流入）
5. 不在 1h RSI > 80 极端超买区

**仓位权重**：confluence 满足 5/5 → 100% 标准仓位；4/5 → 60%；3/5 → 30%

### Setup B：趋势回调（Trend Pullback）

**适用市场状态**：趋势市（ADX > 25）

**做多条件**：
1. 1h/15m 趋势向上
2. 价格回调至 EMA20 或前低支撑位
3. RSI7 从超卖区（< 40）反弹
4. 未跌破关键结构位
5. 15m 出现看涨 K 线形态

**仓位权重**：同上

### Setup C：均值回归 Snap（Mean Reversion Snap）

**适用市场状态**：震荡市（ADX < 20）或趋势末端

**做多条件**：
1. 价格触及布林下轨
2. RSI7 < 25 或 RSI14 < 35
3. 5m/15m 出现底背离或锤子线
4. 未进入强趋势空头（1h EMA20 < EMA50 时仓位减半）
5. 止损紧贴下轨外侧

**仓位权重**：比趋势 setup 低 50%，因为逆势

---

## 三、Confluence 评分模型

每笔潜在交易从以下几个维度打分：

| 维度 | 权重 | 说明 |
|---|---|---|
| 趋势方向一致性 | 25 | 5m/15m/1h 方向一致得分高 |
| ADX 强度 | 20 | ADX > 30 满分，20-30 中等，<20 低 |
| 支撑/阻力质量 | 20 | 是否触及真实结构位 |
| RSI 位置 | 15 | 超买/超卖区 + 拐头 |
| 成交量/OI 确认 | 10 | 量价配合 |
| K线形态 | 10 | 吞没、锤子、流星等 |

**总分区间**：
- 80-100：高 conviction，标准仓位 × 1.5
- 60-79：中等 conviction，标准仓位 × 1.0
- 40-59：低 conviction，标准仓位 × 0.5
- < 40：不交易

---

## 四、动态仓位 sizing

标准仓位定义为：单笔风险 = 账户净值 × 0.5%

```
position_size_usd = (equity * risk_pct * conviction_multiplier) / (leverage * |entry - stop_loss| / entry_price)
```

| 评分 | conviction_multiplier | 实际单笔风险 |
|---|---|---|
| ≥ 80 | 2.0 | ~1.0% |
| 60-79 | 1.0 | ~0.5% |
| 40-59 | 0.5 | ~0.25% |

这样高 confidence 时激进，低 confidence 时小仓位试探。

---

## 五、出场规则（比入场更严格）

### 1. 硬止损

- 基于 ATR14 × 1.0 或关键结构位外侧
- 入场时必须确定，不可移动扩大

### 2. 时间止损

- 持仓 15 分钟未盈利 → 减仓 50%
- 持仓 30 分钟未盈利 → 全部平仓

### 3. 移动止损（保本 + 跟踪）

- 浮盈 ≥ 0.5% → 推止损到保本
- 浮盈 ≥ 1.5% → 锁定 50% 浮盈
- 浮盈 ≥ 3.0% → 锁定 70% 浮盈

### 4. 趋势结构破坏

- 做多时，价格跌破 EMA20 且 RSI 拐头向下 → 离场
- 做空时，价格突破 EMA20 且 RSI 拐头向上 → 离场

### 5. 盈利目标

- 最小 1.2:1 R:R
- 高 conviction 交易可放宽到 1.5:1 或 2:1

---

## 六、风险控制参数

```json
{
  "max_positions": 6,
  "btc_eth_max_leverage": 5,
  "altcoin_max_leverage": 4,
  "btc_eth_max_position_value_ratio": 8,
  "altcoin_max_position_value_ratio": 2,
  "max_margin_usage": 0.8,
  "min_position_size": 100,
  "min_risk_reward_ratio": 1.2,
  "min_confidence": 70,
  "min_close_confidence": 75,
  "stop_loss_atr_buffer": 0,
  "drawdown_close_enabled": true,
  "drawdown_close_min_profit_pct": 1,
  "drawdown_close_min_protected_profit_pct": 0.2,
  "drawdown_close_trigger_pct": 25,
  "drawdown_close_use_ai": false,
  "consecutive_loss_brake": {
    "enabled": true,
    "max_losses": 5,
    "cool_down_cycles": 2
  },
  "breakeven_protection": {
    "enabled": true,
    "trigger_pct": 0.8
  },
  "entry_risk_guard": {
    "enabled": true,
    "mode": "warn_reduce",
    "block_extreme_rsi": true,
    "long_rsi7_max": 85,
    "long_rsi14_max": 75,
    "short_rsi7_min": 15,
    "short_rsi14_min": 25,
    "block_transition_market": false,
    "transition_adx_max": 25,
    "transition_adx_min": 20,
    "block_near_boll_band": false,
    "take_profit_atr_tolerance": 1.0,
    "block_extended_take_profit": false,
    "reduce_position_pct": 0.3,
    "boll_atr_buffer": 0.25
  }
}
```

---

## 七、与趋势 5-2 的核心差异

| 维度 | 趋势 5-2 | 策略 6 |
|---|---|---|
| 交易频率 | 2-4 次/天 | 5-10 次/天 |
| 入场哲学 | 严格过滤，少而精 | 多 setup，confluence 评分 |
| 仓位 | 固定 | 动态基于 conviction |
| 杠杆 | 3x | 5x/4x |
| 止损 | ATR 缓冲 | ATR + 时间止损 |
| 时间止损 | 无 | 15/30 分钟强制减仓/平仓 |
| 保本触发 | 1.5% | 0.5% |
| 连亏刹车 | 3 笔/3 周期 | 5 笔/2 周期 |
| 目标 | 趋势波段 | 小盈积累 +  occasional 大趋势 |

---

## 八、预期表现

### 理想状态

- 胜率：35%-45%
- 盈亏比：1.8 - 2.5
- 日均交易：5-10 笔
- 日均手续费占比：需 < 0.05%（否则侵蚀利润）
- 最大回撤：8%-12%

### 风险点

1. **高杠杆 + 高频率**：单笔小亏累积会快
2. **震荡市 false breakout**：Setup A 在震荡市容易反复止损
3. **AI 评分不稳定**：confluence 评分依赖 AI 判断，需要 prompt 非常清晰
4. **滑点和手续费**：如果每天 10 笔，手续费压力很大

---

## 九、实施建议

1. **先在 Paper Trading 跑 2 周**
   - 目标：验证 confluence 评分是否稳定
   - 目标：看日均交易次数是否过高

2. **只交易 BTC/ETH 前 1 周**
   - 减少滑点和手续费变量

3. **第 2 周加入 SOL**
   - 看 altcoin setup 是否同样有效

4. **关键观察指标**
   - 胜率
   - 平均盈亏比
   - 平均持仓时间
   - 手续费 / 净利润占比
   - 最大单笔亏损

5. **如果 2 周后不理想**
   - 降低杠杆到 4x/3x
   - 提高 confluence 评分门槛到 60
   - 或加入更严格的 time stop

---

## 十、配置导入说明

策略 6 的完整配置见 `config/strategy_6_template.json`。

可以通过前端策略编辑器的"导入"功能加载，或直接用 SQL 插入 `strategies` 表。

---

## 十一、下一步

1. 导入策略 6 到数据库
2. 创建一个 Paper Trader 绑定策略 6
3. 跑 1-2 周收集数据
4. 根据结果调整 confluence 权重和出场参数
