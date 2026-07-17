# NOFX

NOFX 是一个面向加密货币永续合约的自动交易与策略研究系统。项目包含 Go 后端、React 管理后台、多交易所执行层，以及可回放、可校准的确定性交易引擎。

> 自动交易具有显著风险。请先使用模拟盘和小额资金验证策略、交易所权限、手续费与风控配置。

## 核心设计

实盘和模拟盘使用同一条确定性决策链路：

```text
已收盘 K 线与外部数据
  -> 指标、波段结构和市场状态
  -> setup 识别
  -> 多周期证据复核
  -> setup-aware 结构止损与目标
  -> 仓位计算和风险门
  -> 下单、持仓管理与样本记录
```

- **结构优先**：先识别趋势延续、回调、突破、假突破、区间反转等 setup，因子评分用于解释和复核，不直接替代交易逻辑。
- **多周期角色**：机会周期识别结构，入场周期确认触发，确认周期处理方向冲突。
- **结构保护位**：止损和止盈依据 setup 的失效锚点与目标锚点；ATR 缓冲只放宽执行止损并影响仓位，不反推止盈。
- **确定性实时链路**：实时交易周期不调用 LLM，AI 服务不可用不会阻塞行情评估或风控。
- **可学习数据**：保存 setup episode、因子证据、K 线窗口、MFE/MAE、执行结果和净收益，支持参数回放与校准。
- **显式策略演进**：AI 可用于把自然语言编译成结构化配置，或基于充分样本提出未保存的参数建议；建议必须人工审核、保存并启用。

更完整的边界说明见 [交易引擎架构](docs/trading_engine_architecture.md)。

## 功能范围

- 模拟盘和实盘交易
- Binance、Bybit、OKX、Bitget、Gate、KuCoin、Hyperliquid、Aster、Lighter、Indodax
- 多周期 K 线、技术指标、市场结构和市场状态识别
- setup-aware 信号、保护位、仓位与风险控制
- 持仓 thesis、回撤保护、手续费与执行分析
- 策略版本、样本、参数回放、校准报告和 AI 演进建议
- 管理后台、决策审计、交易记录与排行榜
- SQLite 和 PostgreSQL

## 快速启动

### Docker

需要 Docker 与 Docker Compose V2。

```bash
cp .env.example .env
./start.sh start
```

`start.sh` 会检查并生成缺失的 JWT、数据加密和传输加密密钥。默认访问地址由 `.env` 决定：

- 前端：`http://localhost:3011`
- 后端：`http://localhost:8091`

常用命令：

```bash
./start.sh status
./start.sh logs
./start.sh restart
./start.sh stop
```

### 从源码运行

需要 Go 1.25.3+、Node.js 18+ 和可用的 SQLite 或 PostgreSQL。

```bash
cp .env.example .env
go run .
```

另开终端启动前端：

```bash
cd web
npm install
npm run dev
```

前端开发服务器默认使用 Vite 端口；后端监听端口以运行时配置为准。

## 首次使用

1. 在管理后台创建交易所配置；建议先选模拟盘。
2. 创建策略并设置币种来源、周期角色、因子、结构参数和风险限制。
3. 创建交易员，将策略与交易所绑定。
4. 启动交易员，在决策记录中核对 setup、证据、保护位和拒绝原因。
5. 积累独立 setup episode 和已平仓结果后，再使用参数回放与 AI 校准。

运行中的策略不会被未保存的 AI 建议静默修改。修改策略配置后，应确认已保存并按界面提示重启或重新加载交易员。

## 配置

关键环境变量见 [.env.example](.env.example)：

| 变量 | 用途 |
|---|---|
| `NOFX_BACKEND_PORT` | Docker 暴露的后端端口 |
| `NOFX_FRONTEND_PORT` | Docker 暴露的前端端口 |
| `JWT_SECRET` | 登录令牌签名 |
| `DATA_ENCRYPTION_KEY` | 数据库敏感字段加密 |
| `RSA_PRIVATE_KEY` | 浏览器到服务端的敏感字段传输 |
| `TRANSPORT_ENCRYPTION` | 是否启用浏览器端加密 |
| `DB_TYPE` | `sqlite` 或 `postgres` |
| `DB_PATH` | SQLite 数据库路径 |
| `DB_HOST` 等 | PostgreSQL 连接参数 |

不要提交 `.env`、交易所密钥、钱包私钥或生产数据库。

## 项目结构

```text
api/       HTTP API 与管理后台接口
kernel/    策略评估、setup、复核、风控、回放与演进
market/    K 线、指标、市场结构与外部市场数据
trader/    调度、持仓管理和订单执行
store/     配置、样本和交易数据持久化
manager/   多交易员生命周期管理
web/       React 管理后台
```

## 文档

- [文档索引](docs/README.md)
- [交易引擎架构](docs/trading_engine_architecture.md)
- [架构索引](docs/architecture/README.zh-CN.md)
- [快速开始与交易所接入](docs/getting-started/README.zh-CN.md)
- [使用与排障](docs/guides/README.zh-CN.md)
- [指标参考](docs/indicators/README.md)
- [外部市场数据 API](docs/api/API_REFERENCE.md)
- [安全策略](SECURITY.md)
- [传输加密](ENCRYPTION_README.md)

## 验证

```bash
go build ./...
cd web && npm run build
```

## License

[GNU Affero General Public License v3.0](LICENSE)
