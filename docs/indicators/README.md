# Technical Indicators Reference

This directory documents every indicator module in the Nofx modular indicator engine.

Each indicator lives in its own file under `market/indicator_*.go`. Adding a new indicator only requires creating one new file — no existing files need to change.

---

## Indicator List

| # | Indicator | Module File | Status | Source |
|---|-----------|-------------|--------|--------|
| 1 | [SMA](./sma.md) | `market/indicator_sma.go` | ✅ Added | feature branch |
| 2 | [ADX/DMI](./adx.md) | `market/indicator_adx.go` | ✅ Added | feature branch |
| 3 | [Parabolic SAR](./sar.md) | `market/indicator_sar.go` | ✅ Added | feature branch |
| 4 | [Donchian Channel](./donchian.md) | `market/indicator_donchian.go` | ✅ Enhanced | feature branch |
| 5 | EMA | `market/indicator_ema.go` | ✅ Existing | kLine-calc |
| 6 | RSI | `market/indicator_rsi.go` | ✅ Existing | kLine-calc |
| 7 | ATR | `market/indicator_atr.go` | ✅ Existing | kLine-calc |
| 8 | Bollinger Bands | `market/indicator_boll.go` | ✅ Existing | kLine-calc |
| 9 | MACD | `market/indicator_macd.go` | ✅ Existing | kLine-calc |
| 10 | VWAP | `market/indicator_vwap.go` | ✅ Existing | kLine-calc |
| 11 | Volume | `market/indicator_volume.go` | ✅ Existing | kLine-calc |
| 12 | Price Change | `market/indicator_price_change.go` | ✅ Existing | kLine-calc |
| 13 | Realized Volatility | `market/indicator_realized_vol.go` | ✅ Existing | kLine-calc |

---

## Quick Reference: IndicatorRequest Fields

```go
type IndicatorRequest struct {
    // Moving Averages
    EMAPeriods         []int      // e.g. [20, 50]
    SMAPeriods         []int      // e.g. [5, 20, 50]

    // Oscillators
    RSIPeriods         []int      // e.g. [7, 14]
    MACD               *MACDSpec  // {Fast: 12, Slow: 26, Signal: 9}

    // Volatility / Trend Strength
    ATRPeriods         []int      // e.g. [14]
    ADX                *ADXSpec   // {Period: 14}
    BOLLPeriods        []BOLLSpec // [{Period: 20, Multiplier: 2}]
    RealizedVolPeriods []int      // e.g. [20]

    // Trend Following / Channels
    SAR                *SARSpec   // {Enabled: true}
    DonchianPeriods    []int      // e.g. [20, 55]

    // Volume
    VWAPPeriods        []int      // e.g. [20]
    VolumePeriods      []int      // e.g. [20]

    // Price Action
    PriceChangeWindows []int      // e.g. [12, 48]
}
```

---

## How to Add a New Indicator

1. **Create a new file**: `market/indicator_<name>.go`
2. **Implement the `Module` interface**:
   ```go
   type MyIndicatorModule struct{}
   func (m *MyIndicatorModule) Name() string { return "my_indicator" }
   func (m *MyIndicatorModule) Calculate(ctx market.CalcContext, req market.IndicatorRequest) ([]market.IndicatorPoint, error) {
       // ... calculation logic ...
   }
   ```
3. **Register in the engine**: add `&MyIndicatorModule{}` to the list in `market/indicator_engine.go`
4. **Add config field** (if needed): extend `IndicatorRequest` in `market/factor_snapshot.go`
5. **Add strategy config mapping** (if needed): update `IndicatorRequestFromStrategyConfig` in `kernel/engine_analysis.go`
6. **Document it**: create `docs/indicators/<name>.md` (copy from an existing doc as template)

That's it. No existing files besides the registration list need to change.
