# User Guide and Troubleshooting

## Daily Checks

1. Confirm trader status, market timestamps, and exchange account synchronization.
2. Inspect the detected setup, timeframe roles, factor evidence, and rejection reason.
3. Verify structural stop, executable stop, target, fees, and opening thesis for positions.
4. After changing parameters, confirm the intended strategy version is saved and active.
5. Replay measures classification stability. Evaluate profitability with independent setup episodes and closed net outcomes.

## Risk Notes

- Start new strategies in paper trading.
- Limit per-trade risk, margin usage, and concurrent positions together.
- ATR buffer protects a structural stop from ordinary volatility; it does not move the structural target.
- Low trade frequency alone is not a reason to lower every threshold. Distinguish no setup, insufficient evidence, risk rejection, and execution failure.
- AI calibration correctly refuses to propose changes when independent evidence is insufficient.

## Documents

- [Troubleshooting](TROUBLESHOOTING.md)
- [FAQ](faq.en.md)
- [故障排查](TROUBLESHOOTING.zh-CN.md)
- [FAQ 中文](faq.zh-CN.md)
- [PnL accounting](../pnl.md)
- [Trading engine architecture](../trading_engine_architecture.md)
