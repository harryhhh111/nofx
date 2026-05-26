# 项目数据接入现状与覆盖度分析

> 分析时间: 2026-05-26
> 分析范围: `market/`、`kernel/`、`provider/`、`trader/` 核心代码

---

## 1. 覆盖度总览

| # | 数据类型 | 文档调研 | 项目实际接入 | 状态 |
|---|---------|---------|-------------|------|
| 1 | **当前 OI** | ✅ Binance / OKX / CoinGlass | ✅ **已接入** | 运行中 |
| 2 | **Funding Rate** | ✅ Binance / Bybit / OKX | ✅ **已接入** | 运行中 |
| 3 | **K 线数据** | ✅ Binance / OKX / Bybit | ✅ **已接入** | 运行中 |
| 4 | **OI Ranking** | ✅ 自行计算 | ✅ **已接入** | 运行中 |
| 5 | **NetFlow Ranking** | ❌ 无公开 API | ✅ **已接入** | 运行中 |
| 6 | **Price Ranking** | — | ✅ **已接入** | 运行中 |
| 7 | **Exchange Inflow/Outflow** | ❌ 需付费服务 | ⚠️ **部分** | 概念不同 |
| 8 | **Long/Short Ratio** | ✅ Bybit | ✅ **已接入（可配置）** | 运行中 |
| 9 | **Liquidation** | ✅ CoinGlass / Bybit WS | ✅ **已接入（可配置）** | 运行中 |
| 10 | **Orderbook** | ✅ Binance / OKX / Bybit | ❌ **未接入** | 缺失 |
| 11 | **成交明细 (Trades)** | ✅ Binance / OKX / Bybit | ❌ **未接入** | 缺失 |
| 12 | **News/Announcement** | ✅ Binance 公告 / CoinGecko | ❌ **未接入** | 缺失 |

**实际运行覆盖率：7/10（70%）**

---

## 2. 已接入数据详细链路

### 2.1 K 线数据

```
market/data_klines.go
├── 常规加密资产
│   └── getKlinesFromCoinAnk() ──► provider/coinank/coinank_api/kline.go
│                                  └── GET https://api.coinank.com/api/kline/list/open
│                                      (免费公开 API，无需认证)
│                                      支持交易所: Binance, Bybit, OKX, Bitget, Gate, Hyperliquid, Aster
│
└── XYZ dex 资产 (股票/外汇/商品)
    └── getKlinesFromHyperliquid() ──► provider/hyperliquid/kline.go
                                       └── POST https://api.hyperliquid.xyz/info
                                           body: {"type":"candleSnapshot","req":{...}}

备用: market/api_client.go ──► GET https://fapi.binance.com/fapi/v1/klines
```

**时间帧映射（Hyperliquid 不支持的时间帧）**：
- `3m` → `5m`
- `2h` → `1h`
- `6h` → `4h`
- `3d` → `1d`

---

### 2.2 当前 OI

```
market/data.go:getOpenInterestData()
└── GET https://fapi.binance.com/fapi/v1/openInterest?symbol=XXX

补充: kernel/engine.go:FetchQuantData()
└── nofxosClient.GetCoinData(symbol, "oi,price")
    └── 返回跨交易所 OI + OI Delta（变化量/变化率）
```

---

### 2.3 Funding Rate

```
market/data.go:getFundingRate()
└── GET https://fapi.binance.com/fapi/v1/premiumIndex?symbol=XXX
    └── 缓存: 1 小时内存缓存 (sync.Map)

注意: Funding Rate 每 8 小时更新一次，1 小时缓存足够
```

---

### 2.4 OI Ranking

```
kernel/engine.go:FetchOIRankingData()
└── nofxosClient.GetOIRanking(duration, limit)
    ├── GET /api/oi/top-ranking?limit=N&duration=1h
    └── GET /api/oi/low-ranking?limit=N&duration=1h
        └── 返回: OIPosition[] {Symbol, Rank, CurrentOI, OIDelta, OIDeltaPercent, PriceDeltaPercent, NetLong, NetShort}
```

---

### 2.5 NetFlow Ranking

```
kernel/engine.go:FetchNetFlowRankingData()
└── nofxosClient.GetNetFlowRanking(duration, limit)
    └── 返回: InstitutionFutureTop/Low, PersonalFutureTop/Low
```

---

### 2.6 Price Ranking

```
kernel/engine.go:FetchPriceRankingData()
└── nofxosClient.GetPriceRanking(durations, limit)
    └── 返回: 各周期涨跌排行 (Top/Low gainers)
```

---

### 2.7 量化数据 (QuantData)

```
kernel/engine.go:FetchQuantData()
└── nofxosClient.GetCoinData(symbol, include)
    ├── include="oi,price"      → OI + 价格变动
    └── include="netflow,oi,price" → 上述 + NetFlow

数据结构:
├── Price
├── OI {CurrentOI, Delta{1h/4h/24h: {OIDelta, OIDeltaValue, OIDeltaPercent}}}
└── Netflow {Institution{Future, Spot}, Personal{Future, Spot}}
```

---

### 2.8 Long/Short Ratio Ranking（多空比排行）

```
kernel/engine.go:FetchLongShortRankingData()
└── coinankClient.LongShortRank(ctx, sortBy=LongShortPerson, sortType=Desc, page=1, size=limit)
    └── GET https://open-api.coinank.com/api/instruments/longShortRank?page=1&size=N&sortBy=longShortPerson&sortType=desc
        └── 返回: LongShortRankResponse[] {BaseCoin, Price, LongShortPerson, LsPersonChg5M/15M/30M/1H/4H, ExchangeName}

配置项:
├── EnableLongShortRanking   bool  // 是否启用（默认 false）
└── LongShortRankingLimit    int   // 返回条目数（默认 10）
```

**AI Prompt 输出示例**：
```
📊 市场多空比排行（Top 多头/空头情绪）：
币种 | 价格 | 多空比 | 5m变化 | 15m变化 | 1h变化 | 4h变化
BTC | 65000.0000 | 2.35 | +0.05% | +0.12% | +0.30% | -0.08%
```

---

### 2.9 Liquidation Ranking（爆仓排行）

```
kernel/engine.go:FetchLiquidationRankingData()
└── coinankClient.LiquidationRank(ctx, sortBy=LiquidationH24, sortType=Desc, page=1, size=limit)
    └── GET https://open-api.coinank.com/api/instruments/liquidationRank?page=1&size=N&sortBy=liquidationH24&sortType=desc
        └── 返回: LiquidationRankResponse[] {BaseCoin, Price, PriceChangeH24, LiquidationH1/H4/H12/H24 {Total/Long/Short}}

配置项:
├── EnableLiquidationRanking   bool  // 是否启用（默认 false）
└── LiquidationRankingLimit    int   // 返回条目数（默认 10）
```

**AI Prompt 输出示例**：
```
💥 市场爆仓排行（强制平仓统计）：
币种 | 价格 | 24h涨跌 | 1h爆仓(多/空) | 4h爆仓(多/空) | 24h爆仓(多/空)
BTC | 65000.0000 | -1.20% | 1.50(0.80/0.70) | 5.20(2.50/2.70) | 45.80(22.30/23.50)
```

---

## 3. 闲置接口

| 接口 | 功能 | 状态 |
|-----|------|------|
| `OiRank()` | OI 总量排行 | 闲置（项目用 nofxos 的 OI Ranking） |
| `PriceRank()` | 价格变动排行 | 闲置（项目用 nofxos 的 Price Ranking） |
| `VolumeRank()` | 成交量排行 | 闲置 |
| `VisualScreener()` | 可视化筛选器 | 闲置 |

---

## 4. 缺失数据

### 4.1 Orderbook（盘口深度）

- **现状**: 项目中没有任何获取盘口深度的代码
- `market/` 目录下 grep `orderbook/depth/bid/ask` 无匹配
- CoinAnk 虽有 `depth_ws.go`（WebSocket 深度），但未被调用
- **影响**: AI 无法感知当前市场买卖压力、支撑阻力位

### 4.2 成交明细 (Trades)

- **现状**: 没有任何获取逐笔成交的代码
- `market/types.go` 中 `Kline.Trades` 只是 K线聚合的成交笔数，不是逐笔成交
- **影响**: 无法分析大单动向、买卖力量对比

### 4.3 News/Announcement（新闻公告）

- **现状**: 没有任何获取新闻/公告的代码
- **影响**: AI 无法感知宏观事件、交易所公告、项目方动态

---

## 5. 补充建议（按实现难度排序）

### 🔥 优先级 1：新增接口，但文档已验证

**Orderbook + Trades**

- 可使用 Binance 期货公开 API（无需认证）
- `GET /fapi/v1/depth?symbol=XXX&limit=500`
- `GET /fapi/v1/trades?symbol=XXX&limit=1000`
- **预计工作量**: 4-6 小时（含数据格式化入 Prompt）

---

### 🔥 优先级 3：新增能力

**News/Announcement**

- Binance 公告 API: `GET /bapi/composite/v1/public/cms/article/list`
- 或接入 RSS/Twitter/X 监控
- **预计工作量**: 1-2 天（含筛选、去重、相关性判断）

---

## 6. 项目核心数据来源汇总

| 数据源 | Base URL | 认证 | 提供数据 |
|-------|---------|------|---------|
| **CoinAnk** | `https://api.coinank.com` | 无需 | K线（主）、多空比排行、爆仓排行、其他闲置排名接口 |
| **Binance Futures** | `https://fapi.binance.com` | 无需 | OI、Funding Rate、备用 K线 |
| **Hyperliquid** | `https://api.hyperliquid.xyz/info` | 无需 | XYZ dex 资产 K线 |
| **NofxOS** | `https://nofxos.ai` | AuthKey | AI500、OI Ranking、NetFlow、Price Ranking、QuantData |

---

## 7. 下一步行动（供调整）

- [x] ~~接入 CoinAnk `LongShortRank` → AI Prompt~~（已完成）
- [x] ~~接入 CoinAnk `LiquidationRank` → AI Prompt~~（已完成）
- [ ] 新增 Binance `depth` 获取 → AI Prompt
- [ ] 新增 Binance `trades` 获取 → AI Prompt
- [ ] 评估是否需要 News 数据源
- [ ] 清理 CoinAnk 闲置接口（OiRank/PriceRank/VolumeRank/VisualScreener）
