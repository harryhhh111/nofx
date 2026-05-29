# Parabolic SAR — Stop and Reverse

## Module

- **File**: `market/indicator_sar.go`
- **Go Type**: `SARModule`
- **FactorSnapshot Key**: `sar`

## What It Measures

A trend-following indicator that provides **dynamic stop-loss** levels. When price crosses SAR, the trend is considered reversed.

## Formula

```
SAR[t] = SAR[t-1] + AF * (EP - SAR[t-1])
```

| Variable | Meaning |
|----------|---------|
| `EP` | Extreme Point: highest High in uptrend, lowest Low in downtrend |
| `AF` | Acceleration Factor: starts at 0.02, increases by 0.02 on new extremes, capped at 0.20 |

### Trend Reversal Rules

- **Uptrend**: if Low < SAR → flip to **downtrend**
- **Downtrend**: if High > SAR → flip to **uptrend**

On reversal:
- SAR = previous EP
- AF resets to 0.02
- EP resets to current extreme

### SAR Limits (Wilder's original rules)

- Uptrend SAR cannot exceed the Low of the previous 2 bars
- Downtrend SAR cannot be below the High of the previous 2 bars

## Configuration

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `SAR.Enabled` | `bool` | `false` | Whether to calculate SAR |

**Strategy config**:
```json
{
  "enable_sar": true
}
```

## Output Fields

| Name | Key | Value Type |
|------|-----|------------|
| SAR value | `sar` | float — current SAR level |
| Trend direction | `sar_uptrend` | `1` = uptrend, `0` = downtrend |
| Flip to up | `sar_flip_up` | `1` = just flipped up this bar |
| Flip to down | `sar_flip_down` | `1` = just flipped down this bar |

## Typical Usage

| Condition | Action |
|-----------|--------|
| Price > SAR | Long / hold long |
| Price < SAR | Short / hold short |
| SAR flips from below to above price | Close long, consider short |
| SAR flips from above to below price | Close short, consider long |
| SAR acts as trailing stop | Move stop to SAR level |

## Notes

- Minimum bars required: `2`
- AF range: `0.02` → `0.20` (increments of `0.02`)
- Works best in **trending markets**; generates whipsaws in sideways markets.
