# 快速开始与交易所接入

## 启动系统

推荐使用仓库根目录的 Docker 管理脚本：

```bash
cp .env.example .env
./start.sh start
```

默认前端端口为 `3011`，后端端口为 `8091`。实际端口以 `.env` 中的 `NOFX_FRONTEND_PORT` 和 `NOFX_BACKEND_PORT` 为准。

从源码运行需要 Go 1.25.3+ 和 Node.js 18+：

```bash
go run .
```

```bash
cd web
npm install
npm run dev
```

## 首次配置顺序

1. 创建模拟盘或交易所配置。
2. 创建策略，设置币种、周期角色、结构、证据和风险参数。
3. 创建交易员并绑定策略与交易所。
4. 启动交易员，观察决策记录和订单状态。
5. 样本充足后再使用参数回放和 AI 校准。

## 交易所指南

- [Binance API](binance-api.md)
- [Bybit API](bybit-api.md)
- [OKX API](okx-api.md)
- [Aster API 与钱包](aster-api-wallet.md)
- [Hyperliquid Agent Wallet](hyperliquid-agent-wallet.md)
- [Lighter Agent Wallet](lighter-agent-wallet.md)

请只授予交易所 API 读取和交易权限，不要授予提现权限。首次接入应使用模拟盘或小额账户验证仓位方向、精度、手续费、止损和止盈。

## AI 提供商

- [自定义 AI API](custom-api.md)
- [Custom AI API](custom-api.en.md)

AI 提供商只服务于显式的策略编译和校准建议。实时 setup 识别、风险门和下单不依赖 LLM。

## 下一步

- [交易引擎架构](../trading_engine_architecture.md)
- [使用与排障](../guides/README.zh-CN.md)
- [环境变量](../../.env.example)
