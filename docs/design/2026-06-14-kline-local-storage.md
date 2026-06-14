# 1m K线本地存储 + 聚合方案

> 状态：设计中 | 作者：Claude Code | 日期：2026-06-14

## 1. 背景与动机

### 当前架构

每个决策周期需要 **N 个时间段 × M 个币** 次 K 线请求，全部走 CoinAnk 免费 API：

```
决策周期 (每 3-5min)
  └─ 对每个币:
       ├─ 拉 5m K线 × 200 根  (CoinAnk)
       ├─ 拉 15m K线 × 200 根 (CoinAnk)
       ├─ 拉 1h K线 × 200 根  (CoinAnk)
       └─ 拉 4h K线 × 200 根  (CoinAnk)
```

**问题**：
- CoinAnk 免费 API 限流 **2 req/s**（见 `market/data_klines.go:23` 的 rate limiter）
- 10 个币 × 4 个时间段 = 40 次 API 调用/周期，在限流下排队严重
- 同一币的不同时间段数据来自不同 API 调用，存在微小时间差，数据不完全一致
- 缓存是纯内存的，进程重启后全部丢失

### 目标

1. 后台持续从 Binance 同步 1m K 线到本地数据库
2. 积累足够历史数据后，自动从本地聚合出 5m/15m/1h/4h，不再走 CoinAnk
3. Binance 直连作为 CoinAnk 不可用时的 fallback

## 2. 为什么可行

### 2.1 聚合的数学正确性

交易所本身生成高时间段 K 线的方法，就是用低时间段的 tick/bar 聚合：

```
15m K线 = 15 根 1m K线 聚合:
  Open   = 第 1 根 1m 的 Open
  High   = 15 根 1m 的 max(High)
  Low    = 15 根 1m 的 min(Low)
  Close  = 第 15 根 1m 的 Close
  Volume = 15 根 1m 的 sum(Volume)
```

这在数学上与交易所直接返回的 15m K 线等价。实际上 CoinAnk 自己也是这样聚合的。

### 2.2 Binance API 能力

Binance 公开 K 线接口 `GET /fapi/v1/klines` **不需要 API Key**：

| 指标 | CoinAnk 免费 | Binance 公开 |
|------|-------------|-------------|
| 限流 | 2 req/s | 1200 req/min (20 req/s) |
| 单次最大 bar 数 | ~1000 | 1500（见 `market/historical.go:13`） |
| 数据源头 | 交易所二手 | 交易所一手 |
| 延迟 | 经过中间层 | 直连 |

**10 倍的限流差距**让后台同步完全可行。

### 2.3 项目已有 Binance 直连代码

`market/api_client.go:59` 的 `APIClient.GetKlines()` 已经封装了完整的 Binance HTTPS 调用和响应解析，只是从未被调用。可以直接复用。

### 2.4 数据量估算

10 个币 × 30 天 × 1440 根/天 ≈ 432,000 行。

PostgreSQL 中每月约 **15-20 MB**，完全可忽略。

## 3. 架构设计

```
┌─────────────────────────────────────────────────────────────────┐
│                        main.go 启动                              │
│  1. InitTables() ──► GORM AutoMigrate 建表                       │
│  2. Start()      ──► 启动后台同步 goroutine                       │
│  3. SetKlines1MStore() ──► 注册到 market 包                      │
└─────────────────────────────────────────────────────────────────┘

┌──────────────────────────────────────────────────────────────────┐
│              后台同步 (klines_1m_syncer.go)                        │
│                                                                    │
│  每 60 秒:                                                         │
│    symbols = getActiveSymbols()  // 从活跃 trader 获取币种列表       │
│    for each symbol:                                                │
│      klines = Binance.GetKlines(symbol, "1m", 5)  // 最近5根       │
│      INSERT INTO klines_1m ON CONFLICT DO NOTHING                  │
│                                                                    │
│  10币 × 1次/分钟 = 10 req/min，仅占 Binance 限额的 0.8%             │
└──────────────────────────────────────────────────────────────────┘

┌──────────────────────────────────────────────────────────────────┐
│             数据流 (market/data.go 改造点)                         │
│                                                                    │
│  GetWithTimeframes(symbol, ["5m","15m","1h","4h"], ...)           │
│    for each tf:                                                    │
│      coverage = store.GetCoverage(symbol)                         │
│      if coverage >= 200 * tf.Duration:      // 本地数据够           │
│        1m_bars = store.GetKlineRange(symbol, start, end)          │
│        klines = AggregateKlines1M(1m_bars, tf)                    │
│      else:                                                         │
│        klines = CoinAnk API              // 原有路径不变            │
│      series = calculateTimeframeSeries(klines, ...)  // 不变       │
└──────────────────────────────────────────────────────────────────┘

┌──────────────────────────────────────────────────────────────────┐
│              Fallback 链 (market/data_klines.go)                   │
│                                                                    │
│  1. CoinAnk (请求的交易所)                                          │
│  2. CoinAnk-Binance (fallback)                                     │
│  3. Binance 直连 API (新增，最后一层保障)                            │
└──────────────────────────────────────────────────────────────────┘
```

## 4. 数据库设计

### 4.1 表结构

```sql
CREATE TABLE klines_1m (
    symbol     TEXT NOT NULL,
    open_time  BIGINT NOT NULL,          -- K线起始时间，Unix毫秒
    open       DOUBLE PRECISION NOT NULL,
    high       DOUBLE PRECISION NOT NULL,
    low        DOUBLE PRECISION NOT NULL,
    close      DOUBLE PRECISION NOT NULL,
    volume     DOUBLE PRECISION NOT NULL DEFAULT 0,
    close_time BIGINT NOT NULL DEFAULT 0, -- K线结束时间，Unix毫秒
    PRIMARY KEY (symbol, open_time)
);
```

- `(symbol, open_time)` 作为复合主键，天然防重
- 范围查询走主键索引，无需额外索引
- 使用已有的 PostgreSQL（与 config 库同一实例），不新增数据库

### 4.2 新建 vs 复用 DB

**复用已有的 PostgreSQL 连接**（`store.DB()`），理由：
- PostgreSQL 连接池 25 个连接（`store/gorm.go:75`），1m 存储每秒写一次不会成为瓶颈
- 同一实例便于运维，不需要管理第二个数据库
- SQLite 模式（本地开发）自动兼容，`INSERT ... ON CONFLICT` 语法两者都支持

### 4.3 建表方式

通过 GORM `AutoMigrate`，与项目其他表保持一致：

```go
type Kline1mRecord struct {
    Symbol    string  `gorm:"column:symbol;primaryKey"`
    OpenTime  int64   `gorm:"column:open_time;primaryKey"`
    Open      float64 `gorm:"column:open"`
    High      float64 `gorm:"column:high"`
    Low       float64 `gorm:"column:low"`
    Close     float64 `gorm:"column:close"`
    Volume    float64 `gorm:"column:volume"`
    CloseTime int64   `gorm:"column:close_time"`
}
```

GORM 会自动处理 SQLite vs PostgreSQL 的类型差异。

## 5. 核心代码

### 5.1 聚合函数

```go
// AggregateKlines1M 将 1m K线聚合成任意高时间段
func AggregateKlines1M(oneMinKlines []Kline, targetInterval time.Duration) []Kline {
    intervalMs := targetInterval.Milliseconds()

    var result []Kline
    var current *Kline

    for _, bar := range oneMinKlines {
        bucketStart := (bar.OpenTime / intervalMs) * intervalMs

        if current == nil || current.OpenTime != bucketStart {
            if current != nil {
                result = append(result, *current)
            }
            current = &Kline{
                OpenTime:  bucketStart,
                Open:      bar.Open,
                High:      bar.High,
                Low:       bar.Low,
                Close:     bar.Close,
                Volume:    bar.Volume,
                CloseTime: bar.CloseTime,
            }
        } else {
            if bar.High > current.High { current.High = bar.High }
            if bar.Low < current.Low   { current.Low = bar.Low }
            current.Close = bar.Close
            current.Volume += bar.Volume
            current.CloseTime = bar.CloseTime
        }
    }
    if current != nil {
        result = append(result, *current)
    }
    return result
}
```

### 5.2 切换判断

```go
func (s *Klines1MStore) HasSufficientCoverage(symbol string, tf time.Duration) (bool, error) {
    minTime, maxTime, _, err := s.GetCoverage(symbol)
    if err != nil || minTime == 0 || maxTime == 0 {
        return false, err
    }
    required := 200 * tf  // 200根目标时间段 bar
    coverage := time.Duration(maxTime - minTime) * time.Millisecond
    return coverage >= required, nil
}
```

### 5.3 后台同步

```go
func (s *Klines1MSyncer) syncOneCycle() {
    for _, symbol := range s.getSymbols() {
        klines, err := s.apiClient.GetKlines(symbol, "1m", 5)
        if err != nil {
            logger.Warnf("syncer: %s failed: %v", symbol, err)
            continue
        }
        n, _ := s.store.InsertKlines(symbol, klines)
        if n > 0 {
            logger.Debugf("syncer: %s +%d bars", symbol, n)
        }
    }
}
```

## 6. 过渡策略

### 各时间段的切换时间表

| 时间段 | 需要积累 | 预计切换时间 |
|--------|---------|-------------|
| 5m | 16.7 小时 | 启动后 **~17h** |
| 15m | 50 小时 | 启动后 **~2 天** |
| 30m | 100 小时 | 启动后 **~4 天** |
| 1h | 8.3 天 | 启动后 **~8 天** |
| 4h | 33.3 天 | 启动后 **~33 天** |

在切换之前，原有 CoinAnk 路径完全不受影响。

### 数据一致性

本地聚合结果与 CoinAnk API 返回的结果可能因为以下原因有微小差异：
1. 数据源不同（Binance vs CoinAnk 聚合的 Binance）
2. 当前未闭合 bar 的处理（聚合仅出已闭合 bar）

**这些差异对指标计算的影响可以忽略**。EMA/MACD/RSI 等指标本身就是多个 bar 的平滑计算，单 bar 级别的微小差异会被稀释。

## 7. 需要修改的文件

| 文件 | 改动 | 说明 |
|------|------|------|
| `market/klines_1m_store.go` | **新建** | PostgreSQL 存储层 |
| `market/klines_1m_agg.go` | **新建** | 1m→高时间段聚合 |
| `market/klines_1m_syncer.go` | **新建** | 后台同步 worker |
| `market/data_klines.go` | 修改 | +`fetchKlinesBinanceRaw`，修改 fallback 链 |
| `market/data.go` | 修改 | `GetWithTimeframes` 内加切换逻辑 |
| `main.go` | 修改 | 初始化 kline store + 启动 syncer |

## 8. 风险与注意事项

1. **Binance API 限制**：虽然公开 API 限额很宽裕（1200 req/min），但仍需做好限流保护。当前 60s × 10币 = 10 req/min，远不到阈值。

2. **Symbol 格式**：Binance 要求 `BTCUSDT` 格式，项目内 `market.Normalize()` 已处理。

3. **数据空洞**：如果 syncer 短暂挂掉，`GetCoverage` 的 time range 可能跨过空洞，但空洞内的 1m bar 缺失会让聚合结果偏少。后续可加 `COUNT(*)` vs 期望 bar 数的 check。

4. **Hyperliquid 资产**：xyz: 前缀的币种不适用此方案（Binance 上没有），继续走 Hyperliquid API。

5. **历史回填**：长期时间段（4h）需要 33 天数据。可在启动时用 `GetKlinesRange`（`market/historical.go:17`，已有分页逻辑）做一次回填，加速切换。
