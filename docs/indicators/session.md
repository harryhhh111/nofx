# Session OHLCV — Previous Session Data

## Module

- **File**: `market/indicator_session.go`
- **Go Type**: `SessionModule`
- **FactorSnapshot Key**: `session`

## What It Measures

Aggregates OHLCV data by **trading session** — essential structural context for crypto markets (24/7) where there is no natural market close.

In traditional markets, "yesterday's high/low" is obvious. In crypto, you must define what "yesterday" means:
- **UTC day**: 00:00–23:59 UTC
- **Asia session**: 08:00–07:59 next day (Shanghai)
- **Custom offset**: trader-defined session start

This module outputs both the **current session** and **previous session** aggregates, plus breakout signals against the previous session's high/low.

## Phase 1: UTC Day Session

Currently supports `timezone: "UTC"`, `offset: "00:00"`, `duration: 1440` only.
Custom timezones and offsets will be added in Phase 2.

## Formula

### Session Aggregation

For all K-lines whose `CloseTime` falls within the same session window:

```
session_open   = Open of the first bar in the session
session_high   = max(High of all bars in the session)
session_low    = min(Low of all bars in the session)
session_close  = Close of the last bar in the session
session_volume = sum(Volume of all bars in the session)
```

### Bars Since Session Open

```
bars_since_session_open = count of bars in the current session up to and including the latest bar
```

### Breakout Signals vs Previous Session

```
break_above_prev_session_high = 1 if current_close > prev_session_high else 0
break_below_prev_session_low  = 1 if current_close < prev_session_low  else 0
```

## Configuration

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `Sessions` | `[]SessionSpec` | `[{UTC, 00:00, 1440}]` | Session definitions |

**SessionSpec fields**:

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `timezone` | `string` | `"UTC"` | IANA timezone name |
| `offset` | `string` | `"00:00"` | Session start time (HH:MM) |
| `duration` | `int` | `1440` | Session length in minutes |

**Strategy config**:
```json
{
  "enable_session": true
}
```

> Note: Session module is automatically included when `EnableRawKlines` is true (default). It does not have a separate toggle in the current implementation.

## Output Fields

| Name | Key | Description |
|------|-----|-------------|
| Session ID | `session_id` | Encoded as `YYYYMMDD` float (e.g. `20260509`) in `Params["session"]` |
| Session Open | `session_open` | Opening price of current session |
| Session High | `session_high` | Highest price of current session |
| Session Low | `session_low` | Lowest price of current session |
| Session Close | `session_close` | Latest closing price in current session |
| Session Volume | `session_volume` | Total volume of current session |
| Bars Since Open | `bars_since_session_open` | Count of bars since session started |
| Prev Session High | `prev_session_high` | Previous session's highest price |
| Prev Session Low | `prev_session_low` | Previous session's lowest price |
| Prev Session Close | `prev_session_close` | Previous session's closing price |
| Prev Session Volume | `prev_session_volume` | Previous session's total volume |
| Break Above Prev High | `break_above_prev_session_high` | `1` if current price > prev high |
| Break Below Prev Low | `break_below_prev_session_low` | `1` if current price < prev low |

## Typical Usage

### Previous Session Breakout Strategy

| Signal | Condition |
|--------|-----------|
| **Long entry** | Price breaks above `prev_session_high` |
| **Short entry** | Price breaks below `prev_session_low` |
| **Exit long** | Price falls back below `session_open` |
| **Exit short** | Price rises back above `session_open` |

### Session Strength Context

| Context | Interpretation |
|---------|----------------|
| Current price > prev_session_high | Bullish continuation |
| Current price < prev_session_low | Bearish continuation |
| Price inside prev session range | Consolidation / mean reversion zone |
| `bars_since_session_open` < 12 | Early session — avoid overcommitting |
| `bars_since_session_open` > 48 | Late session — liquidity may shift |

## Notes

- Minimum bars required: enough to cover **2 complete sessions** + current partial session.
- For a 1h timeframe with UTC day sessions, you need at least ~72 bars (3 days × 24h).
- Session aggregation uses `CloseTime` to assign bars to sessions.
- Phase 2 will support custom timezones (`Asia/Shanghai`, `America/New_York`) and non-24h sessions (4h, 8h).
