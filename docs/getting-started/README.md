# Getting Started and Exchange Setup

## Start NOFX

The repository Docker script is the recommended entry point:

```bash
cp .env.example .env
./start.sh start
```

The default frontend and backend ports are `3011` and `8091`. Override them with `NOFX_FRONTEND_PORT` and `NOFX_BACKEND_PORT` in `.env`.

For source development, install Go 1.25.3+ and Node.js 18+:

```bash
go run .
```

```bash
cd web
npm install
npm run dev
```

## Initial Setup

1. Add a paper or exchange account.
2. Create a strategy and configure symbols, timeframe roles, structure, evidence, and risk.
3. Create a trader that binds the strategy to the exchange.
4. Start it and inspect decision and order records.
5. Use replay and AI calibration only after enough independent samples exist.

## Exchange Guides

- [Binance API](binance-api.md)
- [Bybit API](bybit-api.md)
- [OKX API](okx-api.md)
- [Aster API and wallet](aster-api-wallet.md)
- [Hyperliquid Agent Wallet](hyperliquid-agent-wallet.md)
- [Lighter Agent Wallet](lighter-agent-wallet.md)

Grant read and trade permissions only. Never grant withdrawal permission. Validate precision, fees, position direction, stop loss, and take profit with paper trading or a small account first.

## AI Providers

- [Custom AI API](custom-api.en.md)
- [自定义 AI API](custom-api.md)

AI providers are used for explicit strategy compilation and calibration proposals. Live setup detection, risk gates, and order execution do not depend on an LLM.

## Next

- [Trading engine architecture](../trading_engine_architecture.md)
- [User guide and troubleshooting](../guides/README.md)
- [Environment variables](../../.env.example)
