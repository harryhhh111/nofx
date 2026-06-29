# Local Coin Supply Refresh Plan

## Background

The `small_market_value` coin source needs real-time `circulating_supply`, `total_supply`, `market_cap` and `volume_24h`. The original implementation used the CoinGecko public API, but the free tier is heavily rate-limited for our deployment IP (observed limit: ~2 requests per minute), leading to frequent `429` and `context deadline exceeded` errors.

当前生产部署已切换为 **CoinMarketCap** 作为主要数据源（通过 `COINMARKETCAP_API_KEY`），同时在本地 PostgreSQL `coin_supply` 表中维护一份缓存。CoinGecko 背景刷新器仍保留作为可选/降级路径。Binance 提供价格、成交量、开仓量和可交易标的 universe，但**不能**提供代币供应量或市值数据，因此本地缓存仍然是必需的。

## Goal

1. Maintain a local `coin_supply` table with supply / market-cap / volume metadata per symbol.
2. Refresh the table continuously in the background at a rate that never triggers CoinGecko free-tier limits (target: **1 page per minute, 250 coins per page**).
3. On application startup, start the background refresher automatically.
4. Change `smallcap` providers to read supply data from the local table first, falling back to CoinGecko only when a symbol is missing.
5. Keep the existing `cmd/seedcoinsupply` one-shot tool for manual bootstrap.

## Data Model

Table already created:

```sql
CREATE TABLE coin_supply (
    symbol              TEXT PRIMARY KEY,
    coingecko_id        TEXT,
    name                TEXT,
    circulating_supply  NUMERIC,
    total_supply        NUMERIC,
    market_cap_usd      NUMERIC,
    volume_24h_usd      NUMERIC,
    last_updated_at     TIMESTAMPTZ DEFAULT NOW(),
    source              TEXT DEFAULT 'coingecko'
);
```

Future optional additions (out of scope for the first PR):

- `max_supply NUMERIC`
- `fully_diluted_valuation NUMERIC`
- `market_cap_rank INT`
- `is_small_cap BOOLEAN` (materialized flag for fast queries)

## Architecture

```
                          ┌─────────────────┐
                          │  CoinMarketCap  │ ◄──── primary source
                          │      API        │       (limit=5000, ~25 credits)
                          └────────┬────────┘
                                   │ fetch on cache miss
                                   │ (max once per 6h)
                          ┌────────▼────────┐
                          │  CoinMarketCap  │
                          │    Provider     │
                          └────────┬────────┘
                                   │ upsert
┌─────────────────┐     1 req/min  │      ┌─────────────┐
│  CoinGecko API  │ ◄──────────────┼───── │  Scheduler  │
└─────────────────┘                │      └──────┬──────┘
                                   │             │ upsert
                          ┌────────▼────────┐    │
                          │   coin_supply   │ ◄──┘
                          │  (PostgreSQL)   │
                          └────────┬────────┘
                                   │ read
                          ┌────────▼────────┐
                          │    smallcap     │
                          │    provider     │
                          └─────────────────┘
```

## Components

### 1. Background refresher (`provider/supplyrefresher`)

New package with a single long-lived goroutine:

```go
type Refresher struct {
    db     *sql.DB
    client *coingecko.Client
    config Config
    stop   chan struct{}
}

type Config struct {
    PageSize        int
    SleepInterval   time.Duration
    MaxPagesPerRun  int
    Order           string // "market_cap_asc" for small-cap first
}
```

Behavior:

- On `Start()` spawn a goroutine.
- Loop forever:
  1. Fetch one page from CoinGecko `/coins/markets`.
  2. Upsert rows into `coin_supply`.
  3. Log inserted/updated count.
  4. Sleep `SleepInterval` (default 60s).
  5. After `MaxPagesPerRun` pages, reset to page 1 (or continue paging through the full universe over multiple cycles).
- On `Stop()` signal the goroutine to exit gracefully.

Recommended default:

```go
Config{
    PageSize:       250,
    SleepInterval:  60 * time.Second,
    MaxPagesPerRun: 10,  // enough for the small-market-value use case
    Order:          "market_cap_asc",
}
```

> **Note:** 10 pages (~2,500 raw rows, typically 700+ valid symbols after normalization) is enough for the small-market-value strategy. There is no need to crawl the full CoinGecko universe. After reaching page 10, the refresher resets to page 1 and starts the next cycle, keeping the local cache fresh for the same small-cap universe.

### 2. Store layer (`store/coin_supply.go`)

Lightweight CRUD:

```go
type CoinSupplyStore interface {
    Upsert(supplies []CoinSupply) error
    GetBySymbol(symbol string) (*CoinSupply, error)
    GetBySymbols(symbols []string) (map[string]CoinSupply, error)
    Count() (int64, error)
}
```

Implementation uses plain `database/sql` or GORM depending on project convention. The one-shot `cmd/seedcoinsupply` should be refactored to use this store.

### 3. Smallcap provider integration

`CoinMarketCapProvider` is the primary implementation when `COINMARKETCAP_API_KEY` is present. It:

1. Checks `coin_supply` for `source = 'coinmarketcap'` records updated within the 6-hour TTL.
2. If fresh data exists, builds the candidate universe from the local table.
3. Otherwise calls CMC `/cryptocurrency/listings/latest` (one page of 5000), validates against Binance futures, enriches OI, and writes results back to `coin_supply`.

`CoinGeckoProvider` still:

1. Fetches the Binance tradable symbol universe.
2. Queries `coin_supply` for all known symbols.
3. For symbols missing locally, falls back to a single CoinGecko request (or skips if unavailable).
4. Computes `market_cap` / `fdv` from local supply × Binance price.
5. Applies liquidity filters and sorting.

This removes the need to call CoinGecko `/coins/markets` repeatedly during every `GetSmallMarketValueRanking` invocation.

### 4. Application lifecycle integration

In `main.go` / server startup:

```go
supplyRefresher := supplyrefresher.New(db, coingeckoClient, cfg)
if err := supplyRefresher.Start(); err != nil {
    log.Fatalf("failed to start supply refresher: %v", err)
}
defer supplyRefresher.Stop()
```

The refresher should tolerate DB or CoinGecko failures without crashing the app. Errors are logged and retried on the next tick.

### 5. Configuration

Add to `.env.example` and `docker-compose.yml`:

```env
# CoinMarketCap API key (primary source for small-market-value)
COINMARKETCAP_API_KEY=

# CoinGecko API key (optional; used by background refresher)
COINGECKO_API_KEY=

# Supply refresher tuning
SUPPLY_REFRESH_PAGE_SIZE=250
SUPPLY_REFRESH_INTERVAL=60s
SUPPLY_REFRESH_MAX_PAGES_PER_RUN=10
SUPPLY_REFRESH_ORDER=market_cap_asc
```

## Migration Path

1. **Bootstrap** (one-time): run `SEED_MAX_PAGES=10 go run ./cmd/seedcoinsupply` to populate the table. Ten pages is sufficient; full universe coverage is not required.
2. **Deploy** the app with the new background refresher.
3. **Monitor**: check logs for `supply_refresher` insert/update counts and any CoinGecko 429 errors.
4. **Fallback**: if the local table is empty for a symbol, the provider can still call CoinGecko on demand as a last resort.

## Error Handling & Observability

- CoinGecko 429: wait `Retry-After` header before the next tick; log warning.
- DB errors: log and skip tick; do not crash the app.
- Missing symbols: log at debug level; do not fail ranking if at least some data is available.
- Metrics: log `coin_supply_count` periodically (e.g. every 10 minutes).

## Out of Scope (future)

- Chain-native supply calculations.
- Automatic deletion of stale / delisted symbols.
- A web UI for supply table status.
- Additional alternative supply sources beyond CoinMarketCap and CoinGecko.

## Acceptance Criteria

- [x] `coin_supply` table is populated by CMC provider and/or the background refresher.
- [x] `small_market_value` candidate generation uses CoinMarketCap when `COINMARKETCAP_API_KEY` is set.
- [x] CMC provider checks local DB freshness before calling the API, keeping credit usage bounded.
- [x] Refresher runs at most 1 CoinGecko request per minute without 429.
- [x] Candidate generation still works when upstream APIs are unavailable, as long as local data exists.
- [x] `go test ./kernel/... ./provider/smallcap/...` passes.
- [x] `cmd/seedcoinsupply` still works for manual bootstrap.
