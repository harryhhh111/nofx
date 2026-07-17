# Architecture Index

NOFX consists of a Go backend, React administration UI, exchange adapters, and persistent strategy and execution data.

## Live Trading Path

```text
manager schedules traders
  -> trader loads account, positions, and closed market data
  -> market calculates indicators, swings, structure, and regime
  -> kernel detects setups, reviews evidence, selects protective levels, and applies risk
  -> trader executes and manages orders
  -> store persists decisions, episodes, trades, and calibration data
```

The live path is deterministic and does not call an LLM. AI is limited to explicit strategy compilation and offline calibration proposals.

## Modules

| Directory | Responsibility |
|---|---|
| `api/` | HTTP API, authentication, and administration data |
| `kernel/` | Evaluation, setups, risk, replay, and evolution |
| `market/` | K-lines, indicators, structure, and external data |
| `trader/` | Trading cycles, positions, and order execution |
| `store/` | SQLite/PostgreSQL persistence |
| `manager/` | Multi-trader lifecycle |
| `web/` | React administration UI |

## Further Reading

- [Trading engine architecture](../trading_engine_architecture.md)
- [Indicator reference](../indicators/README.md)
- [External market-data API](../api/API_REFERENCE.md)
- [x402 streaming payments](X402_STREAMING_PAYMENT.md)
