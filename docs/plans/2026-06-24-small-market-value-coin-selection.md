# Small Market Value Coin Selection Plan

## 背景/目标

`/Users/vinci/Documents/GitHub/NFPrompt/trading/docs/zhibiao.md` 中的 `Small Market Value / Market Cap Ranking` 描述的是跨币种排序与过滤能力，而不是单个交易对自身 K 线可计算出的技术指标。它依赖市场基础数据，例如流通供应量、总供应量、价格、成交额、OI、盘口深度等，用于先筛出可交易且流动性合格的标的，再按 `market_cap` 或 `fdv` 升序选择小市值组合。

> 本仓库是从原始 nofx 仓库 fork 而来的独立项目，新功能需要在本仓库内自行实现，不依赖原始 nofx/NofxOS 托管服务；市值、供应量、OI、盘口深度等市场数据可以通过本仓库内 provider 接入交易所或第三方市场数据源。

目标是在系统中新增一个选币方案：`small_market_value`。该方案作为 `coin_source.source_type` 的一种，和当前 `static`、`ai500`、`oi_top`、`oi_low`、`hyper_all`、`hyper_main` 同层，不放入 `technical_indicators`。

## 范围

- 新增候选币来源 `small_market_value`。
- 支持按小市值排序，同时强制绑定流动性过滤。
- 支持输出用于审计和 AI 上下文的市场基础指标：`market_cap`、`fdv`、`market_cap_rank`、`liquidity_rank`、`small_cap_score`。
- 支持策略编辑页配置该来源、数量上限、排序字段和流动性阈值。
- 支持接口预览该来源返回的候选币与过滤原因。
- 保持现有 `excluded_coins` 对所有来源统一生效。

## 非目标

- 不把 Small Market Value 注册为 SMA/ADX 这类技术指标。
- 不在一期实现链上供应量计算或自行维护 token supply 数据库。
- 不在一期实现自动等权/风险平价下单，只负责候选币列表与依据输出。
- 不改变现有 AI500、OI Top、OI Low 的排序逻辑。
- 不绕过现有 `MaxCandidateCoins` 上限。
- 不在一期开放 `small_cap_score` 作为规则编译器 operand；一期只用于候选币审计、预览和 AI 上下文说明。

## 现状分析

当前候选币来源配置在 [store/strategy.go](/Users/vinci/Documents/GitHub/harryhhh111/nofx-kline-calc/store/strategy.go) 的 `CoinSourceConfig` 中，已有 `source_type`、`static_coins`、`excluded_coins`、`use_ai500`、`use_oi_top`、`use_oi_low` 等字段。

候选币生成集中在 [kernel/engine.go](/Users/vinci/Documents/GitHub/harryhhh111/nofx-kline-calc/kernel/engine.go) 的 `GetCandidateCoins()`，按 `source_type` 分支调用 `getAI500Coins()`、`getOITopCoins()`、`getOILowCoins()`、`getHyperAllCoins()`、`getHyperMainCoins()`。

前端候选币来源配置在 [web/src/components/strategy/CoinSourceEditor.tsx](/Users/vinci/Documents/GitHub/harryhhh111/nofx-kline-calc/web/src/components/strategy/CoinSourceEditor.tsx)，类型定义在 [web/src/types/strategy.ts](/Users/vinci/Documents/GitHub/harryhhh111/nofx-kline-calc/web/src/types/strategy.ts)。

`CoinSourceEditor.tsx` 的 `buildSourceTypeConfig()` 当前只处理 `static/ai500/oi_top/oi_low/mixed` 的互斥 flag；新增 `small_market_value` 时需要同步设置 `use_small_market_value`，并顺手补齐 `hyper_all/hyper_main` 的互斥处理。后端 `normalizeCoinSourceFlags()` 当前也缺少 `hyper_all/hyper_main` case，虽然这不是 Small Market Value 的核心能力，但本次新增 source type 时应一起修正，避免前后端 source 枚举继续漂移。

系统当前已有 AI500、OI Ranking、NetFlow Ranking、Price Ranking 等候选币来源。本仓库也已有 CoinAnk 相关 primitives，例如 `provider/coinank` 中的单币 `GetCoinMarketCap()`、`GetCoinMarketResponse.MarketCap/CirculatingSupply/TotalSupply`，以及 `InstrumentAggSortBy.MarketCap` 排序字段；但这些能力尚未封装成 Small Market Value 候选池 provider，也缺少统一的流动性过滤、候选排序、审计 metrics 和预览 API。因此该方案需要在本仓库内新增可组合的数据 provider。一期先实现 mockable provider 接口和清晰的错误提示，在真实数据源接入前不应伪造市值。

## 设计方案

新增 `source_type = "small_market_value"`，语义为：在交易所可交易标的集合中，先按流动性和可交易性过滤，再按小市值优先排序，返回前 N 个候选币。

默认策略建议：

- 排序字段：优先 `market_cap`，缺失时允许回退 `fdv`。
- 默认数量：3，受 `MaxCandidateCoins` 限制。
- 默认流动性过滤：
  - `min_24h_quote_volume_usd`：建议 5,000,000。
  - `min_open_interest_usd`：建议 1,000,000。
  - `min_depth_usd`：若数据源支持，建议 100,000。
- 风险过滤：
  - 排除没有永续合约或当前交易所不可交易的标的。
  - 排除价格/供应量缺失且无法计算 `market_cap/fdv` 的标的。
  - 排除流动性字段全缺失或低于阈值的标的。

排序流程：

1. 获取交易所可交易 universe。
2. 获取每个标的的 `price`、`circulating_supply`、`total_supply`、`volume_24h_usd`、`open_interest_usd`、`depth_usd`。
3. 计算 `market_cap = circulating_supply * price`。
4. 计算 `fdv = total_supply * price`。
5. 应用可交易性、成交额、OI、盘口深度过滤。
6. 以 `market_cap` 或 `fdv` 升序排序。
7. 计算 `market_cap_rank`、`liquidity_rank`、`small_cap_score`。
8. 返回前 N 个，并保留被过滤项的原因供预览/审计。

`small_cap_score` 建议一期采用简单可解释模型，并明确计算口径：

```text
rank_score = 1 - ((market_cap_rank - 1) / max(total_passed - 1, 1))
liquidity_score = min(
  volume_24h_usd / min_24h_quote_volume_usd,
  open_interest_usd / min_open_interest_usd,
  depth_usd / min_depth_usd
)
liquidity_multiplier = 1.0 if liquidity_score >= 2.0
liquidity_multiplier = 0.75 if 1.0 <= liquidity_score < 2.0
small_cap_score = round(100 * rank_score * liquidity_multiplier)
```

其中 `market_cap_rank` 是通过硬过滤后的升序市值排名，市值越小 rank 越靠前；未通过硬过滤的币不进入候选列表，也不计算 `small_cap_score`。如果数据源暂不支持 `depth_usd`，`liquidity_score` 可只使用成交额和 OI 两项，但必须在返回 metadata 中标记 `depth_available=false`。

## 数据模型/API/流程变更

### Store 配置

扩展 `CoinSourceConfig`：

```go
SourceType string // "static" | "ai500" | "oi_top" | "oi_low" | "hyper_all" | "hyper_main" | "small_market_value" | "mixed"
UseSmallMarketValue bool `json:"use_small_market_value"`
SmallMarketValueLimit int `json:"small_market_value_limit,omitempty"`
SmallMarketValueSortBy string `json:"small_market_value_sort_by,omitempty"` // market_cap | fdv
Min24hQuoteVolumeUSD float64 `json:"min_24h_quote_volume_usd,omitempty"`
MinOpenInterestUSD float64 `json:"min_open_interest_usd,omitempty"`
MinDepthUSD float64 `json:"min_depth_usd,omitempty"`
```

`ClampLimits()` 需要限制 `SmallMarketValueLimit <= MaxCandidateCoins`，并在缺省时设置默认值。建议在 `store/strategy.go` const 区块定义默认值，避免散落硬编码：

```go
DefaultSmallMarketValueLimit     = 3
DefaultSmallMarketValueMinVolume = 5_000_000
DefaultSmallMarketValueMinOI     = 1_000_000
DefaultSmallMarketValueMinDepth  = 100_000
```

`normalizeCoinSourceFlags()` 中为 `small_market_value` 设置互斥 flag，并补齐 `hyper_all/hyper_main` 的现有枚举缺口。`mixed` 模式一期支持 `UseSmallMarketValue` 与其他来源组合，原因是现有 mixed 分支扩展成本低，且可以避免新增 flag 在 mixed 模式下语义悬空。

### Kernel 数据结构

可选扩展 `CandidateCoin`，保留来源和审计指标：

```go
type CandidateCoin struct {
    Symbol  string   `json:"symbol"`
    Sources []string `json:"sources"`
    Metrics map[string]float64 `json:"metrics,omitempty"`
}
```

一期只有 `small_market_value` 来源填充 `Metrics`；AI500、OI Top、OI Low、Hyperliquid 等现有来源可以继续为空。mixed 模式下如果同一个 symbol 来自多个来源，`Sources` 合并，`Metrics` 只保留 Small Market Value 的审计字段，不要求其他来源补 metrics。

新增内部结构：

```go
type SmallMarketValueCoin struct {
    Symbol string
    Price float64
    CirculatingSupply float64
    TotalSupply float64
    MarketCap float64
    FDV float64
    Volume24hUSD float64
    OpenInterestUSD float64
    DepthUSD float64
    MarketCapRank int
    LiquidityRank int
    SmallCapScore float64
    FilterReasons []string
}
```

`FilterReasons` 只用于 provider 内部、预览 API 或调试审计，不进入交易 AI prompt。交易主流程只返回最终候选币和必要 metrics；预览接口可以额外返回过滤统计摘要，例如 `low_volume_count`、`low_oi_count`、`missing_supply_count`。

### Provider/API

新增可 mock 的 provider interface，避免真实数据源未就绪时阻塞配置和 kernel 集成：

```go
type SmallMarketValueProvider interface {
    GetSmallMarketValueRanking(ctx context.Context, req SmallMarketValueRequest) (*SmallMarketValueRankingData, error)
}
```

真实实现应放在本仓库内，例如新增 `provider/marketdata/` 包，或在其内部适配现有 `provider/coinank` primitives：

- `GetSmallMarketValueRanking(ctx, req) (*SmallMarketValueRankingData, error)`
- 支持 TTL cache，建议定义 `DefaultMarketCapCacheTTL = 2 * time.Hour`。
- 一期优先评估复用 CoinAnk 单币市值和 ranking primitives；若其数据覆盖或调用成本不满足需求，再接入其他交易所/聚合数据源。若暂无可用真实数据源，则返回明确的 "not implemented" 错误，不伪造市值。
- 内部 endpoint 建议形态：`/api/market-cap/ranking?sort_by=market_cap&limit=...&min_volume=...&min_oi=...`

新增预览接口可选：

- `GET /api/small-market-value/coins`
- 参数：`limit`、`sort_by`、`min_24h_quote_volume_usd`、`min_open_interest_usd`、`min_depth_usd`
- 返回：候选币、过滤计数、过滤原因摘要、数据时间。
- 鉴权与现有数据预览接口保持一致；如果复用静默检查模式，应在调用端处理静默错误，不把 `silent` 设计成业务参数。

### 候选币流程

在 `StrategyEngine.GetCandidateCoins()` 中新增 `case "small_market_value"`：

- flag 未开启时按现有风格 fallback 到 static。
- provider 报错时返回错误，不自动退化为 AI500，避免用户误以为小市值策略已生效。
- 空结果视为正常但需要 warning，尤其展示过滤阈值过严。
- 最终仍调用 `filterExcludedCoins()`。

`mixed` 模式一期增加 `UseSmallMarketValue` 分支，来源标记为 `small_market_value`。如果某个币同时来自多个来源，按现有逻辑合并 `Sources`，并保留 Small Market Value metrics。

### 外部因子和 AI 上下文

Small Market Value 不作为技术指标，但可作为外部候选依据写入候选币 snapshot：

- `market_cap`
- `fdv`
- `market_cap_rank`
- `liquidity_rank`
- `small_cap_score`

这些字段用于解释“为什么这个币进入候选池”，不建议一期开放为规则编译 operand，除非同步补齐 `SupportedExternalFactors()` 与规则依赖检查。

### 前端

更新：

- `web/src/types/strategy.ts` 的 `CoinSourceConfig` union 增加 `small_market_value`，并补齐 `hyper_all/hyper_main`。
- `CoinSourceEditor.tsx` 增加一个来源卡片。
- `CoinSourceEditor.tsx` 的 `buildSourceTypeConfig()` 增加 `use_small_market_value`，并补齐所有单选 source 的互斥 flag。
- `strategy-translations.ts` 增加文案：`small_market_value`、`small_market_valueDesc`、数量上限、排序字段、成交额/OI/深度阈值。
- 策略 AI 测试页的候选币预览展示 `small_cap_score`、`market_cap`、`fdv` 和过滤 warning。

## 实现步骤

1. 定义 `SmallMarketValueProvider` interface、request/response 结构和 mock provider，先不绑定真实数据源，避免实现被数据源选择阻塞。
2. 扩展 `CoinSourceConfig`、默认值、clamp、normalize、token 估算，并补齐 `hyper_all/hyper_main` 枚举漂移。
3. 基于 mock provider 在 `kernel/engine.go` 新增 `getSmallMarketValueCoins()` 和 `GetCandidateCoins()` 分支，同步覆盖 mixed 模式。
4. 补 Store/Kernel 单测，覆盖排序、过滤、excluded、provider 错误和 mixed 合并。
5. 在本仓库内实现真实 provider，优先评估复用 `provider/coinank` 的 market cap/ranking primitives；若暂无可用数据源，返回明确的未实现错误。
6. 为 preview 增加 API handler 和 route，返回候选币、过滤统计和数据时间。
7. 将候选依据写入 AI 测试/审计输出，至少展示候选来源和核心 metrics；一期不开放为规则 operand。
8. 前端类型、i18n、`CoinSourceEditor` 增加配置 UI，并补 `buildSourceTypeConfig()` 的互斥 flag。
9. 更新用户文档，说明小市值选币是高风险来源且强制流动性过滤。

## 风险点

- 市值数据质量风险：供应量、价格、交易所合约映射不一致会影响排序。
- 小市值流动性风险：即使通过成交额过滤，实盘仍可能滑点大。
- 数据源可用性风险：市值/FDV/流动性数据源未接入或不稳定时该来源无法工作。
- 交易所 universe 不一致：数据源返回的币可能当前交易所不可交易。
- 候选币上限风险：小市值策略若返回过多币，会放大 AI token 和行情拉取成本。
- 策略语义风险：小市值只是选币维度，不代表入场信号，仍需配合技术/结构/风险规则。
- 配置兼容性风险：如果版本回滚，已保存的 `small_market_value` 策略可能被旧版本 `normalizeCoinSourceFlags()` default 分支降级为 `static`，需要在 UI 或日志中提示用户。

## 验证方式

- Store 单测：
  - `source_type=small_market_value` 正确设置 flags。
  - limit 被 clamp 到 `MaxCandidateCoins`。
  - static/mixed 兼容旧配置。
- Kernel 单测：
  - mock provider 返回含市值/流动性数据时，按过滤和升序排序返回候选币。
  - 低成交额、低 OI、缺 supply 的币被过滤。
  - `excluded_coins` 生效。
  - provider 错误时返回明确错误。
- API 测试：
  - preview 参数解析、默认值、错误信息。
- 前端测试：
  - 类型编译通过。
  - 新 source card 可选择并更新配置。
- 回归：
  - `go test ./...`
  - 前端 `npm test` 或至少 `npm run build`。

## 回滚方案

- 若 provider 数据不可用，可隐藏前端入口并保留后端字段兼容。
- 后端可将未知或不可用的 `small_market_value` source 明确返回错误，不影响既有 `static/ai500/oi_top/oi_low`。
- 如果已保存策略使用该 source，回滚时在 `normalizeCoinSourceFlags()` 中将其降级为 `static` 或 `ai500`，并在 UI 显示迁移提示。
- 新增 provider 文件和 route 可独立移除，不影响技术指标计算链路。

## 待确认问题

- 市值/FDV/流动性数据应接入哪个内部/外部数据源？（交易所直连、聚合数据服务、还是其他内部服务？）字段名和调用方式是什么？
- 目标交易所 universe 是否明确跟随 trader 的交易所？建议一期按 trader/exchange 配置过滤，不使用统一全市场列表。

## 已决策事项

- 一期排序默认用 `market_cap`，缺失时回退 `fdv`。
- 默认流动性阈值采用：24h 成交额 5M、OI 1M、盘口深度 100K，并允许配置覆盖。
- mixed 模式一期支持 Small Market Value。
- `small_cap_score` 一期仅用于审计展示和 AI 上下文，不开放为规则编译器 operand。
