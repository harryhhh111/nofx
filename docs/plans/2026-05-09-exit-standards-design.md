# Exit Standards 设计文档

**日期**: 2026-05-09（修订 2026-06-10）
**背景**: 基于近三天 CoT 失败案例分析，AI 在出场决策上反复出现两类问题：
1. "趋势已明显反转，但失效条件未触发，建议 HOLD"
2. "连续亏损 5 笔，需要降低频率" → 3 分钟后继续开仓

**目标**: 建立清晰的出场标准模块，区分「代码强制层」与「AI 认知层」，解决 CoT 中出场理由模糊、标准临时编造的问题。

**设计变更说明（2026-06-10）**:
- 本文档初稿包含「权益回撤制动（Drawdown Brake）」四层状态机（normal/caution/restricted/halt），经评估后**有意移除**
- 移除原因：权益快照频率不足以支撑及时制动；与现有 `DrawdownClose`（单笔浮盈回撤 40% 自动平仓）机制重叠；增加系统复杂度
- 当前采用**简化方案**：仅保留 `ConsecutiveLossBrake`（连续亏损冷却）作为代码层制动，配合 `ExitStandards` Prompt 层出场标准，形成「轻量代码 + 清晰认知」的组合

---

## 一、设计原则：三层分工

| 层级 | 负责内容 | 实现方式 | 示例 |
|---|---|---|---|
| **L1 代码强制** | 不可协商的硬性规则 | Go 代码拦截，AI 无法绕过 | ConsecutiveLossBrake 制动、MaxMarginUsage 限制、DrawdownClose 40% 触发 |
| **L2 AI 认知** | 需要技术分析判断的 checklist | Prompt 注入，AI 必须在 CoT 中显式回答 | 趋势结构破坏、多周期矛盾、利润保护决策 |
| **L3 状态通知** | 让 AI 知道系统已做了什么 | `BrakeNotice` 动态注入 | "系统已制动，仅允许平仓" |

**核心原则**: 凡是能用代码量化的规则（数字阈值、次数统计），一律代码强制；凡是依赖图表解读的判断（趋势是否破坏、矛盾是否成立），交给 AI checklist。

---

## 二、问题归因与对应方案

### 2.1 "趋势已明显反转，但失效条件未触发，建议 HOLD"

**根因**: 当前 `强制平仓检查清单` 只要求检查 `THESIS invalidation`，而 AI 设置的 invalidation 条件往往过窄（如"收盘跌破 2080"）。当趋势通过 EMA 交叉、ADX 崩溃等方式反转时，AI 认为 invalidation 未触发，选择 HOLD。

**方案**:
- 在 `ExitStandards` 中新增「趋势结构破坏」checklist 项
- 要求 AI 在每次考虑持有时，显式检查：
  - 开仓周期是否出现 EMA(9) 与 EMA(21) 反向交叉？
  - ADX 是否从 >25 快速跌破 20，且最近 3 根 K 线持续下降？
  - 价格是否跌破/突破开仓周期的关键摆动低点/高点？
  - 若以上任一成立 → 必须评估平仓，close_confidence ≥ 70 即可执行

### 2.2 "连续亏损 5 笔，需要降低频率" → 3 分钟后继续开仓

**根因**: 纯 prompt 自律不可行。LLM 注意力窗口短，无法跨周期保持状态。

**方案**:
- **不**在 `ExitStandards` prompt 中要求 AI 自己数连续亏损次数
- 代码层已实现 `ConsecutiveLossBrake`（`trader/brake.go`，commit `6269b77a`）
- 判定逻辑：从最近一笔向前数，所有 realized PnL < 0 的交易连续累计 ≥ MaxLosses（默认 3）→ `CoolingState` 进入冷却期
- 冷却期内禁止开仓 K 个轮次（`CoolDownCycles`，默认 3）。**冷却期间若再次发生连亏 ≥ MaxLosses，冷却周期重置**（`CoolDownRemain` 回到满值），确保恢复的前提是市场条件真正改善
- AI 通过 `BrakeNotice`（L3 状态通知）获知当前冷却状态及剩余轮次，但无法自行绕过

---

## 三、Prompt 层设计

### 3.1 数据结构改动

`store/strategy.go`:

```go
type PromptSectionsConfig struct {
    RoleDefinition   string `json:"role_definition,omitempty"`
    TradingFrequency string `json:"trading_frequency,omitempty"`
    EntryStandards   string `json:"entry_standards,omitempty"`
    ExitStandards    string `json:"exit_standards,omitempty"`   // 新增
    DecisionProcess  string `json:"decision_process,omitempty"`
}
```

与 `EntryStandards` 完全对称：支持用户自定义，提供默认文案，可留空回落到系统默认。

### 3.2 Prompt 插入位置

`kernel/engine_prompt.go` — `BuildSystemPrompt()` 中：

当前顺序（`BuildSystemPrompt()` 主流程，编号对应代码注释；省略 `// 0. Data Dictionary`、`// 1. Role definition`、`// 8. Custom Prompt` 等公共头部）：

```
// 3. Hard constraints        → # 硬性约束（风控，含"止损与入场质量"子段）
// 4. Trading frequency       → # ⏱️ 交易频率意识
// 5. Entry standards         → # 🎯 入场标准（内含"可用指标列表 + confidence 阈值"）
// 6. Decision process        → # 📋 决策流程
// 6.5. Thesis-driven mgmt    → # ⚠️ 基于论点的持仓管理 + ⛔ 强制平仓检查清单（6 项）
// 7. Output format           → # 输出格式
```

新顺序：

```
// 3. Hard constraints        → # 硬性约束（风控，含"止损与入场质量"子段）
// 4. Trading frequency       → # ⏱️ 交易频率意识
// 5. Entry standards         → # 🎯 入场标准（内含"可用指标列表 + confidence 阈值"）
// 6. Decision process        → # 📋 决策流程
// 6.6. Exit standards        → # 🛑 出场标准        ← 新增（位于 §6 之后、§6.5 之前）
// 6.5. Thesis-driven mgmt    → # ⚠️ 基于论点的持仓管理 + ⛔ 强制平仓检查清单（6 项）
// 7. Output format           → # 输出格式
```

理由：
- `ExitStandards` 回答"什么情况下你应该考虑平仓"（判断触发条件）
- `强制平仓检查清单` 回答"当你考虑平仓时，必须回答这 6 个问题"（校验执行条件）
- 两者是前置条件 → 执行校验的关系，同一层级排列符合 AI 的 CoT 思维顺序

### 3.3 默认文案

> **默认留空**。`ExitStandards` 字段在 `GetDefaultStrategyConfig` 中**不**填入任何值。
> `BuildSystemPrompt()` 渲染时若 `promptSections.ExitStandards == ""` → 整个 `// 6.6. Exit standards` 段直接跳过,
> prompt 完全回归到 Phase 1 之前的状态(`// 6. Decision process` 后紧接 `// 6.5. Thesis-driven mgmt`)。
>
> 原因:出场景的 checklist 措辞直接影响 AI 的平仓决策,必须基于实际 CoT 表现调优后才固化。
> 在调出"好用的 prompt"之前,先让用户**主动**写一段、不带默认。
> 附录中提供一份**候选默认文案**供参考,用户可手动复制进 PromptSectionsEditor。

### 3.4 候选默认文案（中文，参考用）

```markdown
# 🛑 出场标准（决策 checklist）

你在考虑平仓前，必须在 <reasoning> 中显式回答以下问题。
若无法给出清晰答案 → 输出 HOLD。

## 1. 硬止损逼近
- 价格是否距离开仓时设定的硬止损位 ≤ 当前 ATR 的 0.3 倍？
- **逼近止损 + 趋势结构已破坏（见 §2）→ 可提前 CLOSE，避免滑点损失**
- **逼近止损 + 趋势结构健康 → 让止损正常触发，切勿提前离场**

## 2. 趋势结构破坏
- 开仓周期上 EMA(9) 与 EMA(21) 是否出现反向交叉？
- 或 ADX 是否从 >25 快速跌破 20，且最近 3 根 K 线持续下降（非长期低位震荡）？
- 或价格是否跌破（多头）/ 突破（空头）开仓周期的关键摆动低点/高点？
- **以上任一成立 → 必须重新评估；若 close_confidence ≥ 70 → CLOSE**

## 3. 多周期矛盾加剧
- 更高周期（如 1h、4h）信号是否与当前持仓方向矛盾？
- 矛盾是否已持续 ≥6 根开仓周期 K 线？
- **是 → 必须减仓或平仓，不得死扛等待 thesis 失效**

## 4. 利润保护（MFE 回撤）
- 当前浮盈是否已从该笔持仓的 MFE（最大浮盈峰值）回撤 ≥30%？
- 趋势结构是否仍然健康？
- **趋势结构已破坏 → CLOSE 保护利润**
- **趋势结构健康 → 可 HOLD，让系统 40% DrawdownClose 机制自动处理**
- 注意：若 AI 此处选择 HOLD 但趋势随后加速恶化，系统在回撤 ≥40% 时强制平仓。
  AI 在 30%-40% 区间内有且仅有一次主动干预机会，请谨慎判断。

## 5. 早止盈禁令
- 以开仓时设定的原始止损/止盈计算 R:R；若当前 R:R < 1.0，禁止以"落袋为安"为由提前平仓
- 禁止通过调近止损位来让 R:R 在纸面上 ≥1.0——计算以开仓时的原始值为准
- **唯一例外：趋势结构已破坏（见 §2）**
```

**文案设计意图**:
- 不写"连续亏损 3 次后怎么办" → 交给 ConsecutiveLossBrake 代码层
- 不写"单日回撤 3% 禁止开仓" → 当前仅 Grid Trading 有每日亏损限制（`auto_trader_grid.go`），AI 自主交易无此硬性规则，由 DrawdownClose 单笔浮盈回撤 40% 兜底
- 不写"保证金 85% 禁止开仓" → 交给 `MaxMarginUsage`
- §1「硬止损触发」改为「硬止损逼近」——真正的止损触发由交易所执行，AI 的职责是在逼近时判断是否提前行动以减少滑点
- §2 明确 EMA 周期（9/21）、ADX 区分高位崩溃 vs 长期低位、加入 swing level 检测
- §3 用 K 线根数替代分钟，适配不同主周期
- §4 明确 MFE 定义，注明 30%-40% 区间的干预窗口
- §5 堵上 R:R 绕过漏洞

---

## 四、代码层改动清单

> **Status**: 全部待实施。Phase 1 的 §4.1/§4.2/§4.5 属于 Prompt 层，预计当天可完成；Phase 2 的 §4.4 需 1-2 天。§4.3 已实现。

### 4.1 `store/strategy.go`

- [ ] `PromptSectionsConfig` 新增 `ExitStandards string` 字段
- [ ] `ClampLimits()` 中无需改动（prompt section 是可选字符串）
- [ ] `GetDefaultStrategyConfig()` 中**不**填入中英文 `ExitStandards` 默认值（保持空字符串，留给用户主动配置）
- [ ] `EstimateTokens()` 中增加 `len(c.PromptSections.ExitStandards)` 统计

### 4.2 `kernel/engine_prompt.go`

- [ ] `BuildSystemPrompt()` 中，在 `DecisionProcess` 处理块（`// 6.`）之后、`Thesis-driven position management + close checklist`（`// 6.5.`）之前，新增 `// 6.6. Exit standards` 处理块
- [ ] 渲染逻辑与 `EntryStandards` **不对称**：
  - 若 `promptSections.ExitStandards != ""` → 输出用户自定义内容
  - 否则**整段跳过**，不输出任何内容（不留 placeholder、不注入系统默认）
  - 系统默认候选文案参见 §3.4，**不**写入 `engine_prompt.go`

### 4.3 `trader/brake.go` — 连续亏损追踪（已实现 ✅）

`ConsecutiveLossBrake` 已于 `6269b77a` 实现，无需额外改动。当前架构摘要：

- **状态机**: `CoolingState`（NORMAL → COOLING → NORMAL），由 `UpdateCoolingState()` 每轮驱动
- **触发条件**: `ConsecutiveLossBrakeConfig.MaxLosses`（默认 3）笔连续 realized loss。`countConsecutiveLosses()` 从最近一笔反向遍历，遇盈即停
- **冷却周期**: `ConsecutiveLossBrakeConfig.CoolDownCycles`（默认 3）个轮次禁止开仓，每轮 `CoolDownRemain--`
- **Reset 行为**: 冷却期间若再次发生连亏 ≥ MaxLosses → `CoolDownRemain` 重置到 `CoolDownTotal`（不是叠加延长，是周期重置）。若连亏已中断（`losses < MaxLosses`）→ 继续倒计时
- **退出**: `CoolDownRemain` 归零 → `*state = nil`，恢复正常开仓
- **AI 通知**: 冷却期间 `UpdateCoolingState` 返回 `BrakeNotice` 文本，经 `kernel.Context.BrakeNotice` 注入系统 prompt 尾部
- **开仓拦截**: `ApplyConsecutiveLossGate()` 过滤冷却期间的 `open_long`/`open_short` 决策

> 注：文档初稿曾设计四层权益回撤制动（`ComputeBrakeState` + `BrakeRestricted`/`BrakeHalt`），经评估后有意移除（见文档开头「设计变更说明」）。当前代码仅保留基于连续亏损的 `CoolingState`，更简单可靠。

### 4.4 `kernel/engine.go` — 开仓 stop_loss 强制校验

在 `ValidateDecision` 或交易执行前增加：

```go
if d.Action == "open_long" || d.Action == "open_short" {
    if d.StopLoss <= 0 {
        return fmt.Errorf("stop_loss is required and must be > 0 for %s %s", d.Action, d.Symbol)
    }
}
```

真正实现"硬性止损不可协商"。

### 4.5 前端改动

- [ ] `Store/strategy.ts` 类型定义：`PromptSectionsConfig` 中新增 `exit_standards?: string`
- [ ] `PromptSectionsEditor` 组件中新增 ExitStandards 编辑区域（与 EntryStandards 编辑区对称）
- [ ] i18n 翻译文件中新增 exitStandards 相关 key

---

## 五、与现有系统的整合

### 5.1 与 ConsecutiveLossBrake 的关系

```
ExitStandards (prompt)          ConsecutiveLossBrake (code)
    │                                    │
    ├── AI 认知：趋势破坏                │
    ├── AI 认知：多周期矛盾              ├── 连续 N 笔 realized loss → 状态机进入 cooling
    ├── AI 认知：利润保护判断            │     → 过滤 open_long/open_short 决策
    └── AI 认知：早止盈禁令              │     → 经 BrakeNotice 通知 AI 当前冷却状态
```

两者互补：
- `ExitStandards` 让 AI 在 CoT 中有结构性理由去平仓（解决"临时编造标准"问题）
- `ConsecutiveLossBrake` 确保即使 AI 忽视标准，系统也能拦截（解决"AI 不自律"问题）
- 冷却期间的 `BrakeNotice` 告知 AI 当前状态，避免 AI 困惑"为什么我的开仓决策被拒绝了"

### 5.2 与 DrawdownClose 的关系

- `DrawdownClose`（代码层，40% 浮盈回撤自动平仓）是**最后一道保险**
- `ExitStandards` 中的「利润保护」条款允许 AI 在 30-40% 回撤区间**主动**平仓
- Prompt 文案明确告知 AI："30% 回撤时你有一次主动干预机会；如果你判断趋势健康选择 HOLD，但趋势随后加速恶化突破 40%，系统会强制平仓——这中间可能有滑点延迟"
- AI 的职责是：在 30% 阈值触发时做出最准确的趋势判断，避免不必要的强制平仓或过早离场

### 5.3 与强制平仓检查清单的关系

当前已有 6 项强制平仓检查清单（`[TIMEFRAME]`, `[THESIS]`, `[FEES]`, `[MIN PROFIT]`, `[CLOSE CONFIDENCE]`, `[VERDICT]`）。

`ExitStandards` 不是替代它，而是**前置条件**：
- `ExitStandards` 告诉 AI "什么情况下你应该考虑平仓"（触发判断）
- 强制平仓检查清单告诉 AI "当你考虑平仓时，必须回答这 6 个问题"（执行校验）

两者组合使用：先读 `ExitStandards` 判断是否需要平仓，再读检查清单完成平仓决策的校验。

---

## 六、Token 影响预估

**默认留空时**:`ExitStandards` 不消耗任何 token。

**若用户填入**（参考候选文案体量）:中文约 480 个中文字符 + markdown 标记，合计 ~800 字符；英文约 220 词 / 1300 字符。

| 语言 | 字符数（含 markdown） | 预估 token |
|---|---|---|
| 中文 | ~800 | ~550 |
| 英文 | ~1300 | ~350 |

当前 `EntryStandards` 默认约 600 词（中文），若 `ExitStandards` 也填入则两者体量相当，整体对总 prompt token 影响在 **5-8%** 范围内，可接受；若 `ExitStandards` 留空则无影响。

已在 `EstimateTokens()` 中预留统计位置（`len(c.PromptSections.ExitStandards)`），留空时为 0。

---

## 七、实施顺序建议

1. **Phase 1 — Prompt 层**（当天可完成）
   - `store/strategy.go`: 新增 `ExitStandards` 字段（**不**填默认值，留空）
   - `kernel/engine_prompt.go`: 在强制平仓检查清单之前插入 `ExitStandards` 渲染逻辑，空值时**整段跳过**
   - 前端 `strategy.ts` + `PromptSectionsEditor` + i18n 同步更新
   - 编译通过；用户需在编辑器中**主动**填入 ExitStandards 内容
   - 通过对比 CoT 表现调优出"好用的 prompt"后，再考虑是否回填为系统默认

2. **Phase 2 — 代码层加固**（1-2 天）
   - `kernel/engine.go`: 增加开仓 `stop_loss > 0` 强制校验（§4.4）
   - 端到端测试：验证止损缺失时开仓被正确拒绝

3. **Phase 3 — 迭代优化**（持续）
   - 观察 AI 在 `ExitStandards` 下的 CoT 质量
   - 根据实际表现调整 close_confidence 阈值（当前默认 70）
   - **L2 → L1 升级路径**：若连续出现以下模式 → 将「趋势结构破坏」从 prompt 检查升级为代码层检测，交叉发生时自动注入 `BrakeNotice` 级别 alert：
     - 评估窗口：最近 20 笔平仓
     - 触发条件：≥3 笔平仓在 EMA(9)/(21) 已反向交叉时，AI 仍选择 HOLD（即违反 §2 但未 close）
     - Owner：开发团队 + 策略分析师，Phase 3 启动后每 2 周 review 一次

---

## 八、风险与回退方案

| 风险 | 缓解措施 |
|---|---|
| AI 过度平仓，导致频繁止损 | 保留 `MIN PROFIT` 检查（Net PnL ≥ fees×2）+ `close_confidence` 阈值 |
| Token 增加导致上下文窗口紧张 | `ExitStandards` 与 `EntryStandards` 可选，用户可精简或留空 |
| 与现有制动系统冲突 | 明确分层：代码规则在 `BrakeNotice` 中说明，prompt 只提供判断框架 |
| 不同策略类型（scalping vs trend）对出场标准需求不同 | `ExitStandards` 支持用户自定义，系统默认值取保守共性 |
| 「硬止损逼近」可能被 AI 滥用，恐慌性提前平仓 | 加了"趋势结构健康 → 让止损正常触发"的正向引导，且 AI 需在 reasoning 中论证 |

回退：若发现 AI 反而因为标准过多而混淆，可随时将 `ExitStandards` 设为空字符串，系统即回落到当前行为（仅保留强制平仓检查清单）。

---

## 附录：完整默认文案

### 中文默认

```markdown
# 🛑 出场标准（决策 checklist）

你在考虑平仓前，必须在 <reasoning> 中显式回答以下问题。
若无法给出清晰答案 → 输出 HOLD。

## 1. 硬止损逼近
- 价格是否距离开仓时设定的硬止损位 ≤ 当前 ATR 的 0.3 倍？
- **逼近止损 + 趋势结构已破坏（见 §2）→ 可提前 CLOSE，避免滑点损失**
- **逼近止损 + 趋势结构健康 → 让止损正常触发，切勿提前离场**

## 2. 趋势结构破坏
- 开仓周期上 EMA(9) 与 EMA(21) 是否出现反向交叉？
- 或 ADX 是否从 >25 快速跌破 20，且最近 3 根 K 线持续下降（非长期低位震荡）？
- 或价格是否跌破（多头）/ 突破（空头）开仓周期的关键摆动低点/高点？
- **以上任一成立 → 必须重新评估；若 close_confidence ≥ 70 → CLOSE**

## 3. 多周期矛盾加剧
- 更高周期（如 1h、4h）信号是否与当前持仓方向矛盾？
- 矛盾是否已持续 ≥6 根开仓周期 K 线？
- **是 → 必须减仓或平仓，不得死扛等待 thesis 失效**

## 4. 利润保护（MFE 回撤）
- 当前浮盈是否已从该笔持仓的 MFE（最大浮盈峰值）回撤 ≥30%？
- 趋势结构是否仍然健康？
- **趋势结构已破坏 → CLOSE 保护利润**
- **趋势结构健康 → 可 HOLD，让系统 40% DrawdownClose 机制自动处理**
- 注意：若 AI 此处选择 HOLD 但趋势随后加速恶化，系统在回撤 ≥40% 时强制平仓。
  AI 在 30%-40% 区间内有且仅有一次主动干预机会，请谨慎判断。

## 5. 早止盈禁令
- 以开仓时设定的原始止损/止盈计算 R:R；若当前 R:R < 1.0，禁止以"落袋为安"为由提前平仓
- 禁止通过调近止损位来让 R:R 在纸面上 ≥1.0——计算以开仓时的原始值为准
- **唯一例外：趋势结构已破坏（见 §2）**
```

### English Default

```markdown
# 🛑 Exit Standards (Decision Checklist)

Before considering a close, you MUST explicitly answer the following in <reasoning>.
If you cannot give a clear answer → output HOLD.

## 1. Hard Stop-Loss Approaching
- Is price within ≤ 0.3× current ATR of the hard stop-loss set at entry?
- **Approaching SL + trend structure broken (see §2) → may CLOSE early to avoid slippage**
- **Approaching SL + trend structure healthy → let the stop trigger normally; do NOT exit early**

## 2. Trend Structure Broken
- Has EMA(9) crossed EMA(21) in the opposite direction on the opening timeframe?
- Or has ADX crashed from >25 to below 20, with 3 consecutive bars of declining ADX (not a sustained low-range chop)?
- Or has price broken below (long) / above (short) the key swing low/high on the opening timeframe?
- **Any of the above → MUST re-evaluate; if close_confidence ≥ 70 → CLOSE**

## 3. Multi-Timeframe Conflict Escalation
- Does a higher timeframe (e.g. 1h, 4h) signal contradict the current position direction?
- Has the conflict persisted for ≥6 bars on the opening timeframe?
- **YES → MUST reduce or close. Do NOT hold and wait for thesis invalidation.**

## 4. Profit Protection (MFE Drawdown)
- Has floating profit retraced ≥30% from this position's MFE (Maximum Favorable Excursion peak)?
- Is the trend structure still healthy?
- **Trend broken → CLOSE to protect profits**
- **Trend healthy → may HOLD and let the system's 40% DrawdownClose mechanism handle it**
- Note: if AI chooses HOLD here but the trend accelerates downward, the system will force-close at ≥40% drawdown.
  AI has exactly one discretionary intervention window in the 30%-40% zone — judge carefully.

## 5. Early Profit-Taking Ban
- Calculate R:R using the ORIGINAL stop-loss and take-profit set at entry. If R:R < 1.0, do NOT close for "locking in small gains."
- Do NOT tighten the stop-loss post-entry to make R:R ≥1.0 on paper — the calculation uses original entry values.
- **Only exception: trend structure is broken (see §2)**
```
