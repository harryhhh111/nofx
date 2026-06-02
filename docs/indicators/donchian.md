# Donchian Channel

## Module

- **File**: `market/indicator_donchian.go`
- **Go Type**: `DonchianModule`
- **FactorSnapshot Key**: `donchian`

## What It Measures

The highest High and lowest Low over the preceding N bars, forming a price channel. The classic **Turtle Trading** system uses 20-day and 55-day Donchian Channels for entry/exit signals.

## Formula

Using the **Turtle Trading convention** (current bar excluded):

```
upper_n = max( High[t-1], High[t-2], ..., High[t-n] )
lower_n = min( Low[t-1],  Low[t-2],  ..., Low[t-n]  )
middle  = (upper_n + lower_n) / 2
```

The current bar (index `t`) is **excluded** so that a breakout is judged against the preceding channel, not the channel that the current bar might already be extending.

## Configuration

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `DonchianPeriods` | `[]int` | `[20, 55]` | Channel lookback periods |

**Strategy config**:
```json
{
  "enable_donchian": true,
  "donchian_periods": [20, 55]
}
```

## Output Fields

For each configured period, six `IndicatorPoint`s:

| Name | Key | Description |
|------|-----|-------------|
| Upper band | `donchian_upper` | Max high of preceding N bars |
| Lower band | `donchian_lower` | Min low of preceding N bars |
| Middle band | `donchian_middle` | `(upper + lower) / 2` |
| Break above | `break_above_donchian` | `1` if Close > upper, else `0` |
| Break below | `break_below_donchian` | `1` if Close < lower, else `0` |
| Channel width % | `channel_width_pct` | `(upper - lower) / lower * 100` |

All six share `period: N`.

## Typical Usage

### Turtle Trading System

| Signal | Condition |
|--------|-----------|
| **Long entry** | Price breaks above 20-period upper band |
| **Short entry** | Price breaks below 20-period lower band |
| **Long exit** | Price falls below 10-period lower band (system 1) |
| **Strong trend entry** | Price breaks above 55-period upper band (system 2) |

### Channel Width Interpretation

| Width % | Interpretation |
|---------|----------------|
| < 2% | Tight channel — potential breakout imminent |
| 2–5% | Normal range |
| > 5% | Wide channel — high volatility, be cautious |

## Notes

- Minimum bars required: `N + 1` (N bars for the channel + 1 current bar for breakout detection)
- The **current bar is excluded** from channel calculation (Turtle Trading convention).
- For BoxData (internal structure detection), the non-excluding version is used.
