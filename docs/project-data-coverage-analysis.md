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
| 8 | **Long/Short Ratio** | ✅ Bybit | ⚠️ **代码存在，未调用** | 闲置 |
| 9 | **Liquidation** | ✅ CoinGlass / Bybit WS | ⚠️ **代码存在，未调用** | 闲置 |
| 10 | **Orderbook** | ✅ Binance / OKX / Bybit | ❌ **未接入** | 缺失 |
| 11 | **成交明细 (Trades)** | ✅ Binance / OKX / Bybit | ❌ **未接入** | 缺失 |
| 12 | **News/Announcement** | ✅ Binance 公告 / CoinGecko | ❌ **未接入** | 缺失 |

**实际运行覆盖率：5/10（50%）**

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
```

---

### 2.4 OI Ranking

```
kernel/engine.go:FetchOIRankingData()
└── nofxosClient.GetOIRanking(duration, limit)
    ├── GET /api/oi/top-ranking?limit=N&duration=1h
    └── GET /api/oi/low-ranking?limit=N&duration=1h
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
```

---

## 3. 闲置但未调用的接口

### 3.1 CoinAnk 闲置接口

`provider/coinank/instrument_agg_rank.go` 已定义但未被 kernel/market 层调用：

| 接口 | 功能 | 数据源 | 认证要求 |
|-----|------|--------|---------|
| `OiRank()` | OI 总量排行 | `open-api.coinank.com` | **需 API Key** |
| `LongShortRank()` | 多空比排行 | `open-api.coinank.com` | **需 API Key** |
| `LiquidationRank()` | 爆仓排行 | `open-api.coinank.com` | **需 API Key** |
| `PriceRank()` | 价格变动排行 | `open-api.coinank.com` | **需 API Key** |
| `VolumeRank()` | 成交量排行 | `open-api.coinank.com` | **需 API Key** |
| `VisualScreener()` | 可视化筛选器 | `open-api.coinank.com` | **需 API Key** |

**说明**：CoinAnk 有两套 API：
- `api.coinank.com` —— 免费公开 API（仅 Kline、BaseCoin）
- `open-api.coinank.com` —— Open API，需要 `apikey` header（所有 ranking 接口）

---

## 4. 缺失数据

### 4.1 Orderbook（盘口深度）

- 项目中没有任何获取盘口深度的代码
- **影响**: AI 无法感知当前市场买卖压力

### 4.2 成交明细 (Trades)

- 没有任何获取逐笔成交的代码
- **影响**: 无法分析大单动向

### 4.3 News/Announcement（新闻公告）

- 没有任何获取新闻/公告的代码
- **影响**: AI 无法感知宏观事件

---

## 5. 补充建议（按实现难度排序）

### 🔥 优先级 1：已有接口，直接接入

**Long/Short Ratio + Liquidation**

- 已有 `coinank.LongShortRank()` 和 `coinank.LiquidationRank()`
- **前提**：需要 CoinAnk Open API Key
- 只需在 `kernel/engine.go` 增加获取逻辑，在 `engine_prompt.go` 加入 Prompt 输出
- **预计工作量**: 2-3 小时

---

### 🔥 优先级 2：新增接口，但文档已验证

**Orderbook + Trades**

- 可使用 Binance 期货公开 API（无需认证）
- `GET /fapi/v1/depth?symbol=XXX&limit=500`
- `GET /fapi/v1/trades?symbol=XXX&limit=1000`
- **预计工作量**: 4-6 小时

---

### 🔥 优先级 3：新增能力

**News/Announcement**

- Binance 公告 API: `GET /bapi/composite/v1/public/cms/article/list`
- **预计工作量**: 1-2 天

---

## 6. 项目核心数据来源汇总

| 数据源 | Base URL | 认证 | 提供数据 |
|-------|---------|------|---------|
| **CoinAnk (免费)** | `https://api.coinank.com` | 无需 | K线（主） |
| **CoinAnk (Open API)** | `https://open-api.coinank.com` | **需 API Key** | Ranking 接口（闲置） |
| **Binance Futures** | `https://fapi.binance.com` | 无需 | OI、Funding Rate、备用 K线 |
| **Hyperliquid** | `https://api.hyperliquid.xyz/info` | 无需 | XYZ dex 资产 K线 |
| **NofxOS** | `https://nofxos.ai` | AuthKey (claw402 兜底) | AI500、OI Ranking、NetFlow、Price Ranking、QuantData |

---

## 7. 下一步行动

- [ ] 获取 CoinAnk Open API Key，接入 LongShortRank / LiquidationRank
- [ ] 新增 Binance `depth` 获取 → AI Prompt
- [ ] 新增 Binance `trades` 获取 → AI Prompt
- [ ] 评估是否需要 News 数据源
