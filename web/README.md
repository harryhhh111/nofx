# NOFX Web

React 18、TypeScript 和 Vite 构建的 NOFX 管理后台。

## 开发

```bash
npm install
npm run dev
```

开发服务器默认监听 `http://localhost:3000`，并将 `/api` 代理到 `http://localhost:8080`。

## 构建

```bash
npm run build
```

产物输出到 `dist/`。

## 常用检查

```bash
npm run lint
npm run format:check
npm run build
```

前端负责策略配置、交易员管理、决策审计、持仓和校准结果展示。交易机会、保护位、仓位和风险判断由后端确定性交易引擎完成。
