# 架构索引

NOFX 由 Go 后端、React 管理后台、交易所适配层和持久化层组成。

## 实时交易链路

```text
manager 调度交易员
  -> trader 取得账户、持仓和已收盘行情
  -> market 计算指标、波段结构和市场状态
  -> kernel 识别 setup、复核证据、选择保护位并执行风险门
  -> trader 下单并管理持仓
  -> store 保存决策、episode、交易和校准数据
```

实时链路是确定性的，不调用 LLM。AI 只用于显式的策略编译与离线校准建议。

## 模块

| 目录 | 责任 |
|---|---|
| `api/` | HTTP API、认证和管理后台数据 |
| `kernel/` | 策略评估、setup、风控、回放和演进 |
| `market/` | K 线、指标、结构和外部市场数据 |
| `trader/` | 交易周期、持仓与订单执行 |
| `store/` | SQLite/PostgreSQL 持久化 |
| `manager/` | 多交易员生命周期 |
| `web/` | React 管理后台 |

## 深入阅读

- [交易引擎架构](../trading_engine_architecture.md)
- [技术指标参考](../indicators/README.md)
- [外部市场数据 API](../api/API_REFERENCE.md)
- [x402 流式支付](X402_STREAMING_PAYMENT.md)
