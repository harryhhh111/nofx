# Local Coin Supply Refresh Plan

## Background

The `small_market_value` coin source currently depends on the CoinGecko public API for real-time `circulating_supply`, `total_supply`, `market_cap` and `volume_24h`. The free CoinGecko tier is heavily rate-limited for our deployment IP (observed limit: ~2 requests per minute), which leads to frequent `429` and `context deadline exceeded` errors during trading.

Binance can provide price, volume, open interest and tradable-symbol universe, but **cannot** provide token supply or market-cap data. Therefore we need a persistent local cache of supply data, refreshed periodically from CoinGecko at a very conservative rate, and used at trading time instead of calling CoinGecko directly.

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
┌─────────────────┐     1 req/min      ┌─────────────┐
│  CoinGecko API  │ ◄───────────────── │  Scheduler  │
└─────────────────┘                    └──────┬──────┘
                                              │ upsert
┌─────────────────┐                    ┌──────▼──────┐
│   Binance API   │ ◄──── realtime ────┤  nofx app   │
└─────────────────┘                    └──────┬──────┘
                                              │ read
┌─────────────────┐                    ┌──────▼──────┐
│  coin_supply    │ ◄───────────────── │ smallcap    │
│  (PostgreSQL)   │                    │ provider    │
└─────────────────┘                    └─────────────┘
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
    MaxPagesPerRun: 10,  // one small-cap batch per 10 minutes
    Order:          "market_cap_asc",
}
```

Over time the refresher can page through the full CoinGecko universe (e.g. 40 pages) in a loop, returning to page 1 after the last page. Because supply changes slowly, a symbol that is missing on one cycle will likely be filled on the next.

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

Update `CoinGeckoProvider` (and optionally `CoinAnkProvider`) to:

1. Fetch the Binance tradable symbol universe (unchanged).
2. Query `coin_supply` for all known symbols.
3. For symbols missing locally, fallback to a single CoinGecko request (or skip if unavailable).
4. Compute `market_cap` / `fdv` from local supply × Binance price.
5. Apply liquidity filters and sorting.

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
# CoinGecko API key (recommended; free demo key reduces rate-limit pain)
COINGECKO_API_KEY=

# Supply refresher tuning
SUPPLY_REFRESH_PAGE_SIZE=250
SUPPLY_REFRESH_INTERVAL=60s
SUPPLY_REFRESH_MAX_PAGES_PER_RUN=10
SUPPLY_REFRESH_ORDER=market_cap_asc
```

## Migration Path

1. **Bootstrap** (one-time): run `go run ./cmd/seedcoinsupply` with `SEED_MAX_PAGES=10` to populate the table.
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
- CoinMarketCap or other alternative supply sources.

## Acceptance Criteria

- [ ] `coin_supply` table is populated by the background refresher on startup.
- [ ] Refresher runs at most 1 CoinGecko request per minute without 429.
- [ ] `small_market_value` candidate generation no longer calls CoinGecko `/coins/markets` repeatedly.
- [ ] Candidate generation still works when CoinGecko is unavailable, as long as local data exists.
- [ ] `go test ./...` passes.
- [ ] `cmd/seedcoinsupply` still works for manual bootstrap.
