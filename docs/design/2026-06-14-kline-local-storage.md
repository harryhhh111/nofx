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
4. 为后续结构位计算、TP 锚点校验和量化回测提供可复用的本地 1m 数据基础

### 非目标

第一版不做：

- tick 级回放或盘口级撮合
- Hyperliquid / Aster 等非 Binance 数据的本地聚合替代
- 用未闭合 K 线参与指标计算
- 在数据完整性不足时强行切换到本地聚合

## 2. 为什么可行

### 2.1 聚合的数学正确性与前提

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

但这个等价成立有前提：

- 参与聚合的 1m K 线必须全部闭合
- 目标 bucket 必须完整，例如 15m 必须有 15 根 1m
- 1m 序列不能有缺口
- bucket 对齐必须使用交易所标准时间边界，而不是本地进程启动时间

因此本地聚合不能只实现 OHLCV 汇总，还必须实现完整性校验。

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
│  启动时:                                                           │
│    backfill active symbols 最近 35 天 1m K 线                       │
│                                                                    │
│  每 60 秒:                                                         │
│    symbols = getActiveSymbols()  // 从活跃 trader 获取币种列表       │
│    for each symbol:                                                │
│      klines = Binance.GetKlines(symbol, "1m", 5)  // 最近5根       │
│      filter closed bars only                                       │
│      INSERT INTO klines_1m ON CONFLICT DO NOTHING                  │
│                                                                    │
│  10币 × 1次/分钟 = 10 req/min，仅占 Binance 限额的 0.8%             │
└──────────────────────────────────────────────────────────────────┘

┌──────────────────────────────────────────────────────────────────┐
│             数据流 (market/data.go 改造点)                         │
│                                                                    │
│  GetWithTimeframes(symbol, ["5m","15m","1h","4h"], ...)           │
│    for each tf:                                                    │
│      coverage = store.GetCoverage(exchange, symbol, tf)            │
│      if coverage.complete >= required:      // 本地完整数据够        │
│        1m_bars = store.GetKlineRange(symbol, start, end)          │
│        klines = AggregateKlines1MCompleteBuckets(1m_bars, tf)      │
│        source = local_agg                                          │
│      else:                                                         │
│        klines = CoinAnk API              // 原有路径不变            │
│        source = coinank                                            │
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
    exchange   TEXT NOT NULL DEFAULT 'binance',
    symbol     TEXT NOT NULL,
    open_time  BIGINT NOT NULL,          -- K线起始时间，Unix毫秒
    open       DOUBLE PRECISION NOT NULL,
    high       DOUBLE PRECISION NOT NULL,
    low        DOUBLE PRECISION NOT NULL,
    close      DOUBLE PRECISION NOT NULL,
    volume     DOUBLE PRECISION NOT NULL DEFAULT 0,
    close_time BIGINT NOT NULL DEFAULT 0, -- K线结束时间，Unix毫秒
    source     TEXT NOT NULL DEFAULT 'binance_futures',
    created_at BIGINT NOT NULL DEFAULT 0,
    PRIMARY KEY (exchange, symbol, open_time)
);
```

- `(exchange, symbol, open_time)` 作为复合主键，天然防重
- 范围查询走主键索引，无需额外索引
- 使用已有的 PostgreSQL（与 config 库同一实例），不新增数据库
- `exchange/source` 必须保留，避免把 Binance 数据误当作所有交易所的全局数据

虽然第一版只写 Binance，但主键提前带上 `exchange`，可以避免后续支持 Hyperliquid/Aster 或回测数据源时做破坏性迁移。

### 4.2 新建 vs 复用 DB

**复用已有的 PostgreSQL 连接**（`store.DB()`），理由：
- PostgreSQL 连接池 25 个连接（`store/gorm.go:75`），1m 存储每秒写一次不会成为瓶颈
- 同一实例便于运维，不需要管理第二个数据库
- `INSERT ... ON CONFLICT` 语法 PostgreSQL 原生支持

### 4.3 建表方式

通过 GORM `AutoMigrate`，与项目其他表保持一致：

```go
type Kline1mRecord struct {
    Exchange  string  `gorm:"column:exchange;primaryKey;default:binance"`
    Symbol    string  `gorm:"column:symbol;primaryKey"`
    OpenTime  int64   `gorm:"column:open_time;primaryKey"`
    Open      float64 `gorm:"column:open"`
    High      float64 `gorm:"column:high"`
    Low       float64 `gorm:"column:low"`
    Close     float64 `gorm:"column:close"`
    Volume    float64 `gorm:"column:volume"`
    CloseTime int64   `gorm:"column:close_time"`
    Source    string  `gorm:"column:source;default:binance_futures"`
    CreatedAt int64   `gorm:"column:created_at"`
}
```

GORM 会自动处理 PostgreSQL 的类型差异。

## 5. 核心代码

### 5.1 聚合函数：只输出完整 bucket

```go
// AggregateKlines1MCompleteBuckets 将 1m K线聚合成任意高时间段。
// 只输出完整 bucket；最后一个不足 targetInterval 的 bucket 会被丢弃。
func AggregateKlines1MCompleteBuckets(oneMinKlines []Kline, targetInterval time.Duration) []Kline {
    intervalMs := targetInterval.Milliseconds()
    expectedBars := int(targetInterval / time.Minute)

    var result []Kline
    var current *Kline
    bucketCount := 0

    for _, bar := range oneMinKlines {
        bucketStart := (bar.OpenTime / intervalMs) * intervalMs

        if current == nil || current.OpenTime != bucketStart {
            if current != nil {
                if bucketCount == expectedBars {
                    result = append(result, *current)
                }
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
            bucketCount = 1
        } else {
            if bar.High > current.High { current.High = bar.High }
            if bar.Low < current.Low   { current.Low = bar.Low }
            current.Close = bar.Close
            current.Volume += bar.Volume
            current.CloseTime = bar.CloseTime
            bucketCount++
        }
    }
    if current != nil && bucketCount == expectedBars {
        result = append(result, *current)
    }
    return result
}
```

注意：

- 输入必须按 `open_time ASC` 排序
- 输入应只包含闭合 1m K 线
- 如果中间缺 1m，`bucketCount` 会不足，目标 bucket 不输出
- 实际实现中还应校验同一 bucket 内相邻 1m 的 `open_time` 连续递增，避免乱序、重复或异常间隔误聚合
- 最后一个未完整 bucket 不输出，避免把半根 15m/1h 当完整 K 线计算指标

### 5.2 切换判断：时间跨度 + COUNT 完整性

```go
func (s *Klines1MStore) HasSufficientCoverage(exchange, symbol string, tf time.Duration, targetBars int) (bool, error) {
    end := LastClosedMinute(time.Now()).Add(-tf)
    start := end.Add(-time.Duration(targetBars) * tf)

    expected1m := int(end.Sub(start) / time.Minute)
    actual1m, err := s.CountBars(exchange, symbol, start, end)
    if err != nil {
        return false, err
    }

    // 允许极小偏差，例如 Binance 上市初期或边界时间误差；
    // 第一版建议 required_ratio >= 0.995。
    return float64(actual1m) / float64(expected1m) >= 0.995, nil
}
```

不能只用 `min(open_time)` 和 `max(open_time)` 判断覆盖，因为中间有缺洞时，时间跨度看起来足够，但聚合结果会缺 bar。

`LastClosedMinute` 辅助函数——取当前时间对齐到上一分钟整点边界，再减去 1 分钟，确保返回的是**已闭合**的最后一分钟的起始时间：

```go
// LastClosedMinute returns the open_time of the most recently closed 1m bar.
// For example, at 12:34:56 UTC it returns the Unix milliseconds for 12:33:00.
func LastClosedMinute(now time.Time) time.Time {
    return now.Truncate(time.Minute).Add(-time.Minute)
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
        closed := FilterClosedKlines(klines, time.Now(), 2*time.Second)
        n, _ := s.store.InsertKlines("binance", symbol, closed)
        if n > 0 {
            logger.Debugf("syncer: %s +%d bars", symbol, n)
        }
    }
}
```

**当前 symbol 列表**：第一版硬编码为 `BTCUSDT`、`ETHUSDT`、`SOLUSDT` 三个币种，不动态感知 trader 活跃币种。后续扩展时可改为从活跃 trader 自动发现 symbol，并在首次见到新 symbol 时触发异步回填 35 天数据。

闭合 K 线过滤：

```go
func FilterClosedKlines(klines []Kline, now time.Time, safetyLag time.Duration) []Kline {
    cutoff := now.Add(-safetyLag).UnixMilli()
    out := make([]Kline, 0, len(klines))
    for _, k := range klines {
        if k.CloseTime <= cutoff {
            out = append(out, k)
        }
    }
    return out
}
```

Binance 返回最近 K 线时，最后一根可能是当前未闭合 1m。未闭合 bar 不应入库用于指标计算，否则本地聚合结果会随时间抖动。

### 5.4 启动回填

长期时间段如果只靠增量同步，4h 需要 33 天才能切换。本方案应从 MVP 就支持启动回填：

```go
func (s *Klines1MSyncer) BackfillSymbol(symbol string, days int) error {
    end := LastClosedMinute(time.Now())
    start := end.AddDate(0, 0, -days)
    klines, err := market.GetKlinesRange(symbol, "1m", start, end)
    if err != nil {
        return err
    }
    closed := FilterClosedKlines(klines, time.Now(), 2*time.Second)
    _, err = s.store.InsertKlines("binance", symbol, closed)
    return err
}
```

回填量估算：3 币 × 35 天 × 1440 根/天 ÷ 1500 根/次 ≈ **101 次请求**。加上 500ms 间隔 ≈ 50 秒完成，占 Binance 限额（1200 req/min）的 8.4%。实际实现时应在 `GetKlinesRange` 调用循环中加入 `time.Sleep(500 * time.Millisecond)` 限流保护。

建议默认回填：

- 活跃交易币种：最近 35 天
- 新加入币种：首次出现时异步回填 35 天
- 回填失败：不影响主流程，继续走 CoinAnk，后台重试

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

### 数据一致性与灰度切换

本地聚合结果与 CoinAnk API 返回的结果可能因为以下原因有微小差异：
1. 数据源不同（Binance vs CoinAnk 聚合的 Binance）
2. 当前未闭合 bar 的处理（聚合仅出已闭合 bar）

这些差异不应直接假设“可以忽略”。建议增加灰度对比：

1. 本地数据覆盖充分后，先不立即切换。
2. 对同一个 symbol/timeframe 同时计算 `local_agg` 与 `coinank` 指标。
3. 记录 close、RSI、EMA、MACD、ADX 的差异。
4. 连续 N 个周期差异低于阈值后，再启用本地聚合。

建议阈值：

| 指标 | 阈值 |
|---|---|
| close | < 0.05% |
| RSI | < 1.0 |
| EMA20 | < 0.05% |
| MACD histogram | < 5% |
| ADX | < 2.0 |

切换后仍应在日志中记录每个 timeframe 的数据源：

```text
BTCUSDT 5m source=local_agg bars=200 completeness=1.000
BTCUSDT 1h source=coinank reason=insufficient_coverage completeness=0.742
```

## 7. 需要修改的文件

| 文件 | 改动 | 说明 |
|------|------|------|
| `market/klines_1m_store.go` | **新建** | PostgreSQL 存储层 |
| `market/klines_1m_agg.go` | **新建** | 1m→高时间段聚合 |
| `market/klines_1m_syncer.go` | **新建** | 后台同步 worker |
| `market/klines_1m_quality.go` | **新建** | 覆盖率、缺口、闭合 bar 检查 |
| `market/data_klines.go` | 修改 | +`fetchKlinesBinanceRaw`，修改 fallback 链 |
| `market/data.go` | 修改 | `GetWithTimeframes` 内加切换逻辑 |
| `main.go` | 修改 | 初始化 kline store + 启动 syncer |

## 8. 风险与注意事项

1. **Binance API 限制**：虽然公开 API 限额很宽裕（1200 req/min），但仍需做好限流保护。当前 60s × 10币 = 10 req/min，远不到阈值。历史回填必须单独限流，避免启动时打满 Binance。

2. **Symbol 格式**：Binance 要求 `BTCUSDT` 格式，项目内 `market.Normalize()` 已处理。

3. **数据空洞**：不能只依赖 `GetCoverage` 的 min/max 时间。必须做 `COUNT(*)` vs 期望 1m bar 数检查，否则中间缺洞会悄悄污染聚合。

4. **未闭合 bar**：Binance 最近一根 1m 可能未闭合。必须过滤，否则高周期聚合会把临时价格写成正式指标输入。

5. **不完整 bucket**：最后一个 5m/15m/1h bucket 可能不足完整 1m 数量，必须丢弃，不能交给指标计算。

6. **Hyperliquid 资产**：xyz: 前缀的币种不适用此方案（Binance 上没有），继续走 Hyperliquid API。

7. **历史回填**：长期时间段（4h）需要 33 天数据。应在启动时用 `GetKlinesRange`（`market/historical.go:17`，已有分页逻辑）做一次回填，加速切换。

8. **多数据源混用**：过渡期可能出现 5m 来自 `local_agg`、1h 来自 `coinank`。这是可接受的，但必须记录 source，便于排查指标差异。

## 9. MVP 验收标准

第一版完成时至少满足：

- 表结构包含 `exchange/source`，主键为 `(exchange, symbol, open_time)`。
- syncer 只写入已闭合 1m bar。
- 聚合函数只输出完整 bucket。
- coverage 判断包含 `COUNT(*)` 完整性检查。
- 启动或首次见到 symbol 时支持 35 天异步回填。
- `GetWithTimeframes` 能记录每个 timeframe 的 `source` 与 `completeness`。
- 本地聚合不足时自动回退 CoinAnk。
- 单元测试覆盖：
  - 1m → 5m/15m 聚合 OHLCV 正确性
  - 最后不完整 bucket 被丢弃
  - 中间缺 1m 时不输出对应 bucket
  - 未闭合 bar 被过滤
  - coverage 有 min/max 但 count 不足时返回 false
  - `xyz:` 资产不走 Binance 本地聚合
  - 同一 `(exchange, symbol, open_time)` 重复插入时 `ON CONFLICT DO NOTHING`，不报错也不产生重复行
  - `xyz:` 前缀币种不触发回填
  - 回填请求超过 1500 根时分页拉取、结果连续无缺口

未满足这些标准前，不建议在生产决策中启用本地聚合。
