# K 线全局缓存设计

**日期**: 2026-06-12（修订 2026-06-12）
**状态**: 草案,已根据审核修订

---

## 一、问题

### 1.1 现状

每个 trader 独立拉取 K 线数据，同一 symbol + exchange + timeframe 的组合被重复请求多次：

```
Trader A (paper):  BTCUSDT 5m → HTTP → CoinAnk API
Trader B (live):   BTCUSDT 5m → HTTP → CoinAnk API  ← 同一数据，重复请求
Trader C (paper):  BTCUSDT 5m → HTTP → CoinAnk API  ← 又一枪
```

调用链（以 `engine_analysis.go` 为例）：

```
GetFullDecisionWithStrategy()
  → market.GetWithTimeframesWindowContextWithExchange()
    → for each timeframe:
        getKlinesFromCoinAnk()  // HTTP 请求（CoinAnk API）
```

单个 AI 决策周期内，3 个 trader × 3 个候选币 × 2 个时间帧 = 18 次独立的 HTTP K 线请求，其中大量重复。即使有 client-side rate limiter（之前的修复），这些重复请求仍然浪费网络 IO 和 API 配额。

### 1.2 范围

本方案缓存的是一级数据源：**`getKlinesFromCoinAnk`** 和 **`getKlinesFromHyperliquid`** 的返回值。这些是 K 线获取的底层出口（见 `market/data_klines.go:18` 和 `market/data_klines.go:122`），所有上层调用（`GetWithTimeframes`、`GetBoxData`、`GetWithExchange`）最终都经过它们。

**不包含**：
- 各交易所 trader 的 `GetMarketPrice()`（那是交易所 private API，不走 public K-line）
- Paper trader 的 `marketPrice()`（走的是 CoinAnk 单根 K 线，另案处理）

---

## 二、设计

### 2.1 核心原则：缓存层不修改现有函数签名

现有函数 `getKlinesFromCoinAnk` 和 `getKlinesFromHyperliquid` **保持不变**（作为 "raw fetcher"）。新增一个带缓存的 wrapper，调用方改为调用 wrapper，wrapper 内部在 cache miss 时调用 raw fetcher。**无递归风险**。

```
调用方
  → getKlinesCached(ctx, symbol, exchange, interval, limit, fetchFn)
      → [cache hit] 直接返回
      → [cache miss] singleflight.Do() → fetchFn() → 写缓存 → 返回
                                        ↑
                                  getKlinesFromCoinAnk (不改)
                                  getKlinesFromHyperliquid (不改)
```

### 2.2 可注入 fetcher（便于单测）

`getKlinesCached` 的最后一个参数是 fetcher 函数，生产代码传入 raw fetcher，单测传入 mock：

```go
type klineFetcher func(ctx context.Context, symbol, interval string, limit int, exchange string) ([]Kline, error)

func getKlinesCached(ctx context.Context, symbol, exchange, interval string, limit int,
    fetch klineFetcher) ([]Kline, error) {
    // ...
}
```

生产调用：

```go
klines, err := getKlinesCached(ctx, symbol, "binance", "5m", 200, getKlinesFromCoinAnkContext)
```

### 2.3 缓存 Key

```
cacheKey = "{symbol}:{exchange}:{interval}"
```

示例：`BTCUSDT:binance:5m`、`ETHUSDT:bybit:15m`

### 2.4 缓存 Value

```go
type klineCacheEntry struct {
    klines    []Kline    // 全量 K 线（按 OpenTime 升序）
    fetchedAt time.Time  // 抓取时间
    limit     int        // 实际请求的 limit
}
```

**limit 向上取整策略**：如果缓存中的 `limit` ≥ 请求的 `limit`，直接截取尾部返回。如果缓存不够大（请求的 limit > 缓存的 limit），则穿透缓存重新请求更大的 limit。

```go
if entry.limit >= limit {
    start := len(entry.klines) - limit
    if start < 0 { start = 0 }
    return entry.klines[start:], nil
}
// 不够大 → 穿透
```

### 2.5 TTL 策略（含 Bar 边界检测）

K 线一旦收盘就不可变，唯一变化的是当前未收盘的 Bar。TTL 由两层逻辑决定：

**基础 TTL**：

| Timeframe | 基础 TTL | 理由 |
|-----------|---------|------|
| 1m | 30s | 半根 Bar 后刷新 |
| 3m | 90s | 同上 |
| 5m | 150s (2.5min) | 半根 Bar |
| ≥15m | 30s | 同一决策周期内复用即可 |

**Bar 边界检测**（覆盖基础 TTL 的盲区）：

即使基础 TTL 未过期，如果最后一根缓存 K 线的 `CloseTime < now - interval`（即至少有一根新 Bar 已收盘），缓存也视为过期。这解决了"12:00:59 抓 1m，12:01:20 基础 TTL 仍有效但已漏掉 12:01 的新 Bar"的问题：

```go
func cacheValid(entry *klineCacheEntry, interval string) bool {
    if time.Since(entry.fetchedAt) >= baseTTL(interval) {
        return false
    }
    // Bar-boundary check: a new bar should have closed since we fetched
    if len(entry.klines) > 0 {
        newestBar := entry.klines[len(entry.klines)-1]
        barDuration := intervalDuration(interval)
        if newestBar.CloseTime > 0 && time.UnixMilli(newestBar.CloseTime).Add(barDuration).Before(time.Now()) {
            return false // at least one new bar has closed
        }
    }
    return true
}
```

> **注意**：`CloseTime` 在部分数据源中可能为 0（如 CoinAnk 不返回 CloseTime）。此时退化为仅依赖基础 TTL。

### 2.6 并发合并（singleflight）

同一 key + limit 组合的并发请求**只发出一次 HTTP 调用**，所有等待者共享结果。**singleflight key 包含 limit**，避免小 limit 请求的返回结果被大 limit 请求误用（导致根数不足）：

```go
import "golang.org/x/sync/singleflight"

var klineSF singleflight.Group

func getKlinesCached(ctx context.Context, symbol, exchange, interval string, limit int,
    fetch klineFetcher) ([]Kline, error) {
    cKey := cacheKey(symbol, exchange, interval)
    sfKey := cKey + ":" + strconv.Itoa(limit) // singleflight key 包含 limit

    // Fast path: cache hit（singleflight 外检查）
    if entry, ok := cacheGet(cKey); ok && cacheValid(entry, interval) && entry.limit >= limit {
        return sliceTail(entry.klines, limit), nil
    }

    // Slow path: singleflight 内检查 + 抓取
    result, err, _ := klineSF.Do(sfKey, func() (interface{}, error) {
        // Double-check: 可能被前一个 singleflight 调用填满了
        if entry, ok := cacheGet(cKey); ok && cacheValid(entry, interval) && entry.limit >= limit {
            return sliceTail(entry.klines, limit), nil
        }

        // 计算实际抓取的 limit：取 max(requestLimit, cachedLimit)
        fetchLimit := limit
        if entry, ok := cacheGet(cKey); ok && entry.limit > fetchLimit {
            fetchLimit = entry.limit
        }

        klines, err := fetch(ctx, symbol, interval, fetchLimit, exchange)
        if err != nil {
            return nil, err
        }

        // 写缓存
        cacheSet(cKey, klines, fetchLimit)
        return klines, nil
    })

    if err != nil {
        return nil, err
    }
    return sliceTail(result.([]Kline), limit), nil
}
```

> **为什么 singleflight key 包含 limit？** 并发场景中，goroutine A 请求 limit=100、goroutine B 请求 limit=500。如果不区分，B 可能共享到 A 的 100 根结果 → 根数不足。区分后各自走自己的 singleflight，虽然少了一次合并机会，但保证了正确性。作为补偿，后续请求如果 limit ≤ 缓存的 limit（100 ≤ 500），直接走 fast path 命中。

### 2.7 不缓存 Fallback 结果

`getKlinesFromCoinAnk` 当前在非 Binance exchange 请求失败或返回空数据时会 **fallback 到 Binance**（见 `market/data_klines.go:81-96`）。这个 fallback 结果**不能**缓存到原始 exchange 的 key 下，否则会长期用 Binance 数据伪装成 Bybit/OKX 数据。

**规则**：只缓存原始请求 exchange 成功返回的结果。Fallback 路径不走缓存：

```go
// 在 getKlinesFromCoinAnk 中：
// 原始 exchange 请求成功 → 结果被缓存（在 getKlinesCached 层）
// 原始 exchange 失败/空 → fallback 到 Binance → 不缓存（直接 return，绕过 cacheSet）
```

实现方式：`getKlinesFromCoinAnk` 的 fallback 分支不走 `getKlinesCached`，直接调 raw Binance API。

### 2.8 Context 支持

当前 `getKlinesFromCoinAnk` 和 `getKlinesFromHyperliquid` 内部使用 `context.Background()`。为了支持超时控制和取消传播，需新增 context-aware 变体：

```go
// 新增：带 context 的版本（给缓存层调用）
func getKlinesFromCoinAnkContext(ctx context.Context, symbol, interval string, limit int, exchange string) ([]Kline, error) {
    // 与现有 getKlinesFromCoinAnk 逻辑相同，但 HTTP 请求使用传入的 ctx
}

// 现有函数改为调用 context 版本（向后兼容）
func getKlinesFromCoinAnk(symbol, interval, exchange string, limit int) ([]Kline, error) {
    return getKlinesFromCoinAnkContext(context.Background(), symbol, interval, limit, exchange)
}
```

### 2.9 Hyperliquid 路径

Hyperliquid 的 K 线走 `getKlinesFromHyperliquid`，同一缓存模式，exchange 固定为 `"hyperliquid"`。同样新增 `getKlinesFromHyperliquidContext`。

### 2.10 缓存容量控制

全局缓存不加 size cap — key 数量上界是 `symbols × exchanges × timeframes`。实际场景：
- symbol: 最多 10 个候选币 + 3 个持仓币 = 13 个
- exchange: 最多 3 个（binance + bybit + hyperliquid）
- timeframe: 最多 8 个（1m/3m/5m/15m/30m/1h/4h/1d）
- 总 key: 13 × 3 × 8 = 312 个

每个 key 存 200-500 根 K 线（~20KB），总计 ≤ 6MB 内存，可忽略。

每 5 分钟清理一次过期条目（`fetchedAt + TTL < now`），防止 symbol 退市后残留。

---

## 三、与 rate limiter 的关系

之前加的 `coinank_api.Kline` rate limiter（500ms/req）和本缓存是**互补**关系：

| 机制 | 作用 | 位置 |
|------|------|------|
| Rate limiter | 防 CoinAnk 静默限流 | `coinank_api/kline.go` |
| 共享缓存 | 防同数据跨 trader 重复请求 | `market/data_klines.go` |
| singleflight | 防同 key 并发重复请求 | `market/data_klines.go` |

缓存命中时，rate limiter 根本不会被触发（没走到 HTTP 层）。

---

## 四、代码改动清单

### 4.1 `market/data_klines.go`

- [ ] 新增 `klineCache` map + `sync.RWMutex`
- [ ] 新增 `klineSF` singleflight.Group
- [ ] 新增 `klineFetcher` type：`func(ctx, symbol, interval, limit, exchange) ([]Kline, error)`
- [ ] 新增 `getKlinesCached(ctx, symbol, exchange, interval, limit, fetch)` — 缓存 wrapper
- [ ] 新增 `cacheKey()`、`baseTTL()`、`cacheValid()`（含 Bar 边界检测）、`cacheGet()`、`cacheSet()` helper
- [ ] 新增 `cleanExpiredCache()` + `time.NewTicker(5*time.Minute)` goroutine
- [ ] 新增 `getKlinesFromCoinAnkContext(ctx, symbol, interval, limit, exchange)` — 带 ctx 的 raw fetcher
- [ ] 现有 `getKlinesFromCoinAnk` → 改为委托 `getKlinesFromCoinAnkContext(context.Background(), ...)`
- [ ] 新增 `getKlinesFromHyperliquidContext` — 同理
- [ ] 现有 `getKlinesFromHyperliquid` → 委托 context 版本
- [ ] `getKlinesFromCoinAnk` 的 fallback 分支 → 不经过缓存，直接调 Binance raw API
- [ ] 调用方（`GetWithExchange`、`GetWithTimeframes`、`GetBoxData`）→ 调用 `getKlinesCached(..., getKlinesFromCoinAnkContext)` 代替直接调 raw fetcher

### 4.2 `go.mod`

- [ ] `golang.org/x/sync` 从 indirect 提升为 direct（`go mod tidy`）

### 4.3 不需要改的文件

- `trader/paper/trader.go` — `marketPrice()` 走 CoinAnk 单根 K 线，不在本缓存范围
- `market/data.go` — 上层调用改一行（raw → cached），其余无需改动
- 各交易所 adapter — 不涉及

---

## 五、验证

### 5.1 单元测试

通过可注入的 `fetch klineFetcher` 参数，单测注入 mock fetcher 精确验证：

- [ ] 同一 key + limit 在 TTL 内第二次调用 → 缓存命中，fetch 不被调用
- [ ] Bar 边界：缓存中最新的 Bar 已收盘超过一个 interval → 缓存失效，重新 fetch
- [ ] 请求 limit ≤ 缓存 limit → 截取尾部返回，fetch 不被调用
- [ ] 请求 limit > 缓存 limit → 穿透，用更大 limit 重抓
- [ ] 3 个并发请求同一 key+limit → fetch 只被调用 1 次（singleflight 验证）
- [ ] 并发请求不同 limit（100 vs 500）→ 各自独立 singleflight，互不干扰
- [ ] fallback 路径（Bybit→Binance）→ 不写缓存
- [ ] 不同 symbol/exchange/timeframe → 独立缓存，互不影响
- [ ] 过期清理：TTL 过期后 fetch 被重新调用

### 5.2 集成测试

- [ ] 启动 2 个 paper trader，同一 symbol，同一决策周期 → 验证只有 1 次 HTTP K 线请求
- [ ] 缓存过期后下一周期 → 验证发起新请求
- [ ] `go build -o nofx .` 编译通过

### 5.3 观测指标

- [ ] 日志中同一 key 多次请求 → 确认缓存命中（可在 DEBUG 级别打印 `cache hit: {key}`）
- [ ] 对比缓存前后的 CoinAnk API 请求总数

---

## 六、风险

| 风险 | 缓解 |
|------|------|
| 缓存返回的 K 线不含当前未收盘 Bar 的最新价 | Bar 边界检测确保新 Bar 收盘后立即失效；未收盘 Bar 的 OHLC 变化对 15m 级别决策影响极小 |
| `CloseTime` 为 0 时 Bar 边界检测失效 | 退化为纯基础 TTL；仅在 CoinAnk 数据源有此问题 |
| 请求 limit 混用（100/200/500）导致缓存命中率下降 | 小 limit 可复用大 limit 缓存；大 limit 穿透时用更大值重抓，后续小请求命中 |
| singleflight key 含 limit 导致同 symbol 不同 limit 不合并 | 取舍：正确性优先。实际场景中同一时刻相同 symbol 的请求 limit 通常一致（同一决策周期），命中率影响小 |
| singleflight 中一个请求失败 → 所有等待者一起失败 | 不缓存 error；每个等待者各自收到 error，上层已有重试/跳过逻辑 |
| CoinAnk fallback 结果被缓存到错误 exchange key | 只缓存原始 exchange 的成功结果；fallback 路径绕过缓存 |
| 不同 exchange 的同 symbol 数据不同 | key 已包含 exchange，不会混淆 |
| 内存泄漏（退市 symbol 缓存残留） | 5 分钟清理 goroutine |

---

## 七、实施顺序

1. 新增 `getKlinesFromCoinAnkContext` / `getKlinesFromHyperliquidContext`（带 ctx 的 raw fetcher）
2. 现有函数委托到 context 版本（保持向后兼容）
3. 新增缓存层 `getKlinesCached` + helper
4. 调用方从 raw fetcher 切换到 `getKlinesCached`
5. `go mod tidy`
6. 单元测试 + 集成测试
7. 观察日志中的缓存命中率

预计改动量：~130 行 Go（含 ctx 变体 ~30 行 + 缓存层 ~80 行 + 调用方切换 ~20 行）。
