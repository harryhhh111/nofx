# SMA — Simple Moving Average

## Module

- **File**: `market/indicator_sma.go`
- **Go Type**: `SMAModule`
- **FactorSnapshot Key**: `sma`

## What It Measures

The arithmetic mean of closing prices over the last N bars. Unlike EMA, SMA gives equal weight to every bar in the window.

## Formula

```
SMA = (Close[t] + Close[t-1] + ... + Close[t-N+1]) / N
```

## Configuration

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `SMAPeriods` | `[]int` | `[5, 20, 50]` | Periods to compute |

**Strategy config**:
```json
{
  "enable_sma": true,
  "sma_periods": [5, 20, 50]
}
```

## Output Fields

For each configured period, one `IndicatorPoint`:

| Name | Key | Params |
|------|-----|--------|
| SMA value | `sma` | `period: N` |

## Typical Usage

- **SMA crossovers**: short-period SMA crossing above long-period SMA signals bullish momentum.
- **Support/Resistance**: price often bounces off major SMAs (e.g. SMA 200).
- **Comparison with EMA**: SMA is smoother but lags more; EMA reacts faster to recent price changes.

## Notes

- Minimum bars required: `N`
- Calculated in `O(N)` per period per timeframe.
