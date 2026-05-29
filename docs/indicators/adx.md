# ADX/DMI — Average Directional Index

## Module

- **File**: `market/indicator_adx.go`
- **Go Type**: `ADXModule`
- **FactorSnapshot Key**: `adx`

## What It Measures

Trend **strength** (ADX) and **direction** (+DI / -DI).

- **ADX**: 0–100 scale. Higher = stronger trend. Below 20 = weak/no trend.
- **+DI** (Positive Directional Indicator): strength of upward movement.
- **-DI** (Negative Directional Indicator): strength of downward movement.

## Formula

### Step 1: True Range & Directional Movement
```
TR  = max(High - Low, |High - PrevClose|, |Low - PrevClose|)
+DM = max(High - PrevHigh, 0)  if up > down else 0
-DM = max(PrevLow - Low, 0)    if down > up else 0
```

### Step 2: Wilder Smoothing
```
ATR    = RMA(TR,  N)
+DI    = 100 * RMA(+DM, N) / ATR
-DI    = 100 * RMA(-DM, N) / ATR
DX     = 100 * |+DI - -DI| / (+DI + -DI)
ADX    = RMA(DX, N)
```

Where `RMA` (Wilder's Smoothing) is:
- First value = SMA of first N values
- Subsequent = (prev * (N-1) + current) / N

## Configuration

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `ADX.Period` | `int` | `14` | Lookback period for smoothing |

**Strategy config**:
```json
{
  "enable_adx": true,
  "adx_period": 14
}
```

## Output Fields

| Name | Key | Description |
|------|-----|-------------|
| ADX | `adx` | Trend strength (0–100) |
| +DI | `plus_di` | Bullish directional strength |
| -DI | `minus_di` | Bearish directional strength |

All three share `period: N`.

## Typical Usage

| Condition | Interpretation |
|-----------|----------------|
| ADX > 25 | Trend is strong enough to trade |
| ADX < 20 | No clear trend — avoid trend-following |
| +DI > -DI | Bullish direction dominates |
| -DI > +DI | Bearish direction dominates |
| +DI crosses above -DI | Potential long signal |
| -DI crosses above +DI | Potential short signal |

## Notes

- Minimum bars required: `2*N + 1` (need N bars for first ATR smoothing, N bars for DX smoothing, plus initial bar).
- Period is fixed at **14** in the current engine (industry standard).
