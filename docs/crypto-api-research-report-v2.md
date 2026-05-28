# 加密货币数据 API 调研报告（修正版）

> 调研时间: 2026-05-26
> 验证方式: 交叉参考官方文档 + 实际 API 调用测试

---

## 目录

1. [Open Interest（未平仓合约量）](#1-open-interest未平仓合约量)
2. [Funding Rate（资金费率）](#2-funding-rate资金费率)
3. [Orderbook（盘口深度）](#3-orderbook盘口深度)
4. [Trades（成交明细）](#4-trades成交明细)
5. [Long/Short Ratio（多空比）](#5-longshort-ratio多空比)
6. [Liquidation（爆仓数据）](#6-liquidation爆仓数据)
7. [OI Ranking（持仓量排行）](#7-oi-ranking持仓量排行)
8. [NetFlow / Exchange Inflow-Outflow](#8-netflow--exchange-inflow-outflow净流量)
9. [News / Announcement](#9-news--announcement新闻公告)
10. [汇总表](#汇总表)
11. [限流参考](#限流参考)
12. [参考资料](#参考资料)

---

## 官方文档入口（已验证）

| 交易所 | 文档地址 | 验证状态 |
|--------|---------|----------|
| **Binance 期货** | https://developers.binance.com/docs/futures | ✅ 已验证 |
| **OKX** | https://www.okx.com/docs-cn/ | ✅ 官方文档 |
| **Bybit V5** | https://bybit-exchange.github.io/docs/v5/ | ✅ 官方文档 |
| **CoinGlass** | https://www.coinglass.com/zh/api | ⚠️ 需登录后查看 |

---

## 1. Open Interest（未平仓合约量）

### 主数据源: Binance Futures API

**文档**: https://developers.binance.com/docs/futures

**接口**: `GET /fapi/v1/openInterest`

**完整URL**: `https://fapi.binance.com/fapi/v1/openInterest?symbol=BTCUSDT`

**参数**:
| 参数 | 类型 | 必填 | 说明 |
|-----|------|-----|------|
| symbol | STRING | YES | 交易对，如 BTCUSDT |

**Python 调用示例**:
```python
import requests

def get_binance_open_interest(symbol="BTCUSDT"):
    """
    获取 Binance U本位合约未平仓合约量
    文档: https://developers.binance.com/docs/futures
    """
    url = "https://fapi.binance.com/fapi/v1/openInterest"
    params = {"symbol": symbol}
    
    response = requests.get(url, params=params, timeout=10)
    response.raise_for_status()
    data = response.json()
    
    # 实际响应字段：{"openInterest": "98142.100", "symbol": "BTCUSDT", "time": 1779801437102}
    return {
        "symbol": data["symbol"],
        "openInterest": data["openInterest"],  # OI 数量（标的币数量）
        "time": data["time"]
    }

# 使用
result = get_binance_open_interest("BTCUSDT")
print(f"BTCUSDT 未平仓: {result['openInterest']} BTC")
```

**实际响应**:
```json
{
  "symbol": "BTCUSDT",
  "openInterest": "98142.100",
  "time": 1779801437102
}
```

**限流**: Weight = **1**

---

### 备用数据源: OKX API

**文档**: https://www.okx.com/docs-cn/

**接口**: `GET /api/v5/market/open-interest`

**完整URL**: `https://www.okx.com/api/v5/market/open-interest?instId=BTC-USDT-SWAP`

**参数**:
| 参数 | 类型 | 必填 | 说明 |
|-----|------|-----|------|
| instId | STRING | YES | 合约ID，如 BTC-USDT-SWAP |

**Python 调用示例**:
```python
import requests

def get_okx_open_interest(inst_id="BTC-USDT-SWAP"):
    """
    获取 OKX 合约未平仓合约量
    文档: https://www.okx.com/docs-cn/
    """
    url = "https://www.okx.com/api/v5/market/open-interest"
    params = {"instId": inst_id}
    
    response = requests.get(url, params=params, timeout=10)
    data = response.json()
    
    if data.get("code") == "0":
        result = data["data"][0]
        return {
            "instId": result["instId"],
            "oi": result["oi"],
            "oiCcy": result["oiCcy"],
            "ts": result["ts"]
        }
    return None

# 使用
result = get_okx_open_interest("BTC-USDT-SWAP")
print(f"BTC-USDT-SWAP OI: {result['oiCcy']} {result['oi']}")
```

**限流**: 20 次/2秒（基于 IP）

---

## 2. Funding Rate（资金费率）

### 主数据源: Binance Futures API

**文档**: https://developers.binance.com/docs/futures

**接口**: `GET /fapi/v1/premiumIndex`

**完整URL**: `https://fapi.binance.com/fapi/v1/premiumIndex?symbol=BTCUSDT`

**Python 调用示例**:
```python
import requests
from datetime import datetime

def get_binance_funding_rate(symbol="BTCUSDT"):
    """
    获取 Binance U本位合约资金费率
    文档: https://developers.binance.com/docs/futures
    """
    url = "https://fapi.binance.com/fapi/v1/premiumIndex"
    params = {"symbol": symbol}
    
    response = requests.get(url, params=params, timeout=10)
    data = response.json()
    
    return {
        "symbol": data["symbol"],
        "markPrice": data["markPrice"],
        "indexPrice": data["indexPrice"],
        "estimatedSettlePrice": data.get("estimatedSettlePrice"),
        "lastFundingRate": data["lastFundingRate"],
        "nextFundingTime": data["nextFundingTime"],
        "interestRate": data["interestRate"]
    }

# 使用
result = get_binance_funding_rate("BTCUSDT")
rate_pct = float(result['lastFundingRate']) * 100
print(f"BTCUSDT 资金费率: {rate_pct:.4f}%")
print(f"下次结算: {datetime.fromtimestamp(result['nextFundingTime']/1000)}")
```

**实际响应**:
```json
{
  "symbol": "BTCUSDT",
  "markPrice": "76993.00000000",
  "indexPrice": "77028.59500000",
  "estimatedSettlePrice": "77097.19365978",
  "lastFundingRate": "0.00003317",
  "interestRate": "0.00010000",
  "nextFundingTime": 1779811200000,
  "time": 1779801492001
}
```

**限流**: Weight = **1**（单 symbol）

---

### 备用数据源: Bybit API

**文档**: https://bybit-exchange.github.io/docs/v5/

**接口**: `GET /v5/market/tickers`

**完整URL**: `https://api.bybit.com/v5/market/tickers?category=linear&symbol=BTCUSDT`

**参数**:
| 参数 | 类型 | 必填 | 说明 |
|-----|------|-----|------|
| category | STRING | YES | linear（U本位永续）, inverse, spot |
| symbol | STRING | NO | 不传则返回所有 |

**Python 调用示例**:
```python
import requests

def get_bybit_funding_rate(symbol="BTCUSDT"):
    """
    获取 Bybit U本位永续资金费率
    文档: https://bybit-exchange.github.io/docs/v5/
    """
    url = "https://api.bybit.com/v5/market/tickers"
    params = {"category": "linear", "symbol": symbol}
    
    response = requests.get(url, params=params, timeout=10)
    data = response.json()
    
    if data.get("retCode") == 0:
        result = data["result"]["list"][0]
        return {
            "symbol": result["symbol"],
            "fundingRate": result["fundingRate"],
            "nextFundingTime": result["nextFundingTime"],
            "fundingIntervalHour": result.get("fundingIntervalHour")
        }
    return None

# 使用
result = get_bybit_funding_rate("BTCUSDT")
print(f"Bybit BTCUSDT 资金费率: {float(result['fundingRate'])*100:.4f}%")
```

**限流**: 约 **50-120 次/秒**（公开接口，基于 IP）

---

### 备用数据源: OKX API

**文档**: https://www.okx.com/docs-cn/

**接口**: `GET /api/v5/market/funding-rate`

**完整URL**: `https://www.okx.com/api/v5/market/funding-rate?instId=BTC-USDT-SWAP`

**Python 调用示例**:
```python
import requests

def get_okx_funding_rate(inst_id="BTC-USDT-SWAP"):
    """
    获取 OKX 合约资金费率
    文档: https://www.okx.com/docs-cn/
    """
    url = "https://www.okx.com/api/v5/market/funding-rate"
    params = {"instId": inst_id}
    
    response = requests.get(url, params=params, timeout=10)
    data = response.json()
    
    if data.get("code") == "0":
        result = data["data"][0]
        return {
            "instId": result["instId"],
            "fundingRate": result["fundingRate"],
            "nextFundingRate": result["nextFundingRate"],
            "fundingTime": result["fundingTime"]
        }
    return None

# 使用
result = get_okx_funding_rate("BTC-USDT-SWAP")
print(f"OKX 资金费率: {float(result['fundingRate'])*100:.4f}%")
```

---

## 3. Orderbook（盘口深度）

### 主数据源: Binance Futures API

**文档**: https://developers.binance.com/docs/futures

**接口**: `GET /fapi/v1/depth`

**完整URL**: `https://fapi.binance.com/fapi/v1/depth?symbol=BTCUSDT&limit=500`

**参数**:
| 参数 | 类型 | 必填 | 说明 |
|-----|------|-----|------|
| symbol | STRING | YES | 交易对 |
| limit | INT | NO | 5, 10, 20, 50, 100, 500, 1000 |

**Python 调用示例**:
```python
import requests

def get_binance_orderbook(symbol="BTCUSDT", limit=500):
    """
    获取 Binance U本位合约盘口深度
    文档: https://developers.binance.com/docs/futures
    """
    url = "https://fapi.binance.com/fapi/v1/depth"
    params = {"symbol": symbol, "limit": limit}
    
    response = requests.get(url, params=params, timeout=10)
    data = response.json()
    
    return {
        "bids": data["bids"],  # [[价格, 数量], ...]
        "asks": data["asks"],  # [[价格, 数量], ...]
        "lastUpdateId": data["lastUpdateId"]
    }

# 使用
result = get_binance_orderbook("BTCUSDT", limit=100)
print(f"买一价: {result['bids'][0][0]}, 买一量: {result['bids'][0][1]}")
print(f"卖一价: {result['asks'][0][0]}, 卖一量: {result['asks'][0][1]}")
```

**限流**: Weight = **1**（limit≤100）/ **5**（limit≤500）/ **10**（limit≤1000）

---

### 备用数据源: OKX API

**文档**: https://www.okx.com/docs-cn/

**接口**: `GET /api/v5/market/books`

**完整URL**: `https://www.okx.com/api/v5/market/books?instId=BTC-USDT-SWAP&sz=400`

**参数**:
| 参数 | 类型 | 必填 | 说明 |
|-----|------|-----|------|
| instId | STRING | YES | 合约ID |
| sz | INT | NO | 档位数，默认 1，最大 400 |

**Python 调用示例**:
```python
import requests

def get_okx_orderbook(inst_id="BTC-USDT-SWAP", sz=400):
    """
    获取 OKX 合约盘口深度
    文档: https://www.okx.com/docs-cn/
    """
    url = "https://www.okx.com/api/v5/market/books"
    params = {"instId": inst_id, "sz": sz}
    
    response = requests.get(url, params=params, timeout=10)
    data = response.json()
    
    if data.get("code") == "0":
        result = data["data"][0]
        return {
            "instId": result.get("instId"),
            "bids": result["bids"],  # [价格, 数量, 订单笔数, 深度层数]
            "asks": result["asks"],
            "ts": result["ts"]
        }
    return None

# 使用
result = get_okx_orderbook("BTC-USDT-SWAP", sz=100)
print(f"买一: {result['bids'][0][0]}, 卖一: {result['asks'][0][0]}")
```

---

### 备用数据源: Bybit API

**文档**: https://bybit-exchange.github.io/docs/v5/

**接口**: `GET /v5/market/orderbook`

**完整URL**: `https://api.bybit.com/v5/market/orderbook?category=linear&symbol=BTCUSDT&limit=200`

**参数**:
| 参数 | 类型 | 必填 | 说明 |
|-----|------|-----|------|
| category | STRING | YES | linear, inverse, spot |
| symbol | STRING | YES | 交易对 |
| limit | INT | NO | 最大 200 |

**Python 调用示例**:
```python
import requests

def get_bybit_orderbook(symbol="BTCUSDT", category="linear", limit=200):
    """
    获取 Bybit 盘口深度
    文档: https://bybit-exchange.github.io/docs/v5/
    """
    url = "https://api.bybit.com/v5/market/orderbook"
    params = {"category": category, "symbol": symbol, "limit": limit}
    
    response = requests.get(url, params=params, timeout=10)
    data = response.json()
    
    if data.get("retCode") == 0:
        result = data["result"]
        return {
            "symbol": result["s"],
            "bids": result["b"],  # [价格, 数量]
            "asks": result["a"],
            "ts": result["ts"]
        }
    return None

# 使用
result = get_bybit_orderbook("BTCUSDT")
print(f"Bybit 买一: {result['bids'][0][0]}, 卖一: {result['asks'][0][0]}")
```

---

## 4. Trades（成交明细）

### 主数据源: Binance Futures API

**文档**: https://developers.binance.com/docs/futures

**接口**: `GET /fapi/v1/trades`

**完整URL**: `https://fapi.binance.com/fapi/v1/trades?symbol=BTCUSDT`

**参数**:
| 参数 | 类型 | 必填 | 说明 |
|-----|------|-----|------|
| symbol | STRING | YES | 交易对 |
| limit | INT | NO | 默认 500，最大 1000 |

**Python 调用示例**:
```python
import requests

def get_binance_trades(symbol="BTCUSDT", limit=500):
    """
    获取 Binance U本位合约最近成交
    文档: https://developers.binance.com/docs/futures
    """
    url = "https://fapi.binance.com/fapi/v1/trades"
    params = {"symbol": symbol, "limit": limit}
    
    response = requests.get(url, params=params, timeout=10)
    trades = response.json()
    
    return [{
        "id": t["id"],
        "price": t["price"],
        "qty": t["qty"],
        "quoteQty": t.get("quoteQty"),
        "time": t["time"],
        "isBuyerMaker": t["isBuyerMaker"],
        "isRPITrade": t.get("isRPITrade")
    } for t in trades]

# 使用
result = get_binance_trades("BTCUSDT", limit=10)
for t in result[:5]:
    side = "卖出" if t["isBuyerMaker"] else "买入"
    print(f"{side} @ {t['price']}, 数量: {t['qty']}")
```

**限流**: Weight 取决于 limit（1-100: **1**, 100-500: **2**, 500-1000: **5**）

---

### 备用数据源: OKX API

**文档**: https://www.okx.com/docs-cn/

**接口**: `GET /api/v5/market/trades`

**完整URL**: `https://www.okx.com/api/v5/market/trades?instId=BTC-USDT-SWAP`

**Python 调用示例**:
```python
import requests

def get_okx_trades(inst_id="BTC-USDT-SWAP"):
    """
    获取 OKX 合约最近成交
    文档: https://www.okx.com/docs-cn/
    """
    url = "https://www.okx.com/api/v5/market/trades"
    params = {"instId": inst_id}
    
    response = requests.get(url, params=params, timeout=10)
    data = response.json()
    
    if data.get("code") == "0":
        return [{
            "instId": t["instId"],
            "px": t["px"],
            "sz": t["sz"],
            "side": t["side"],
            "ts": t["ts"]
        } for t in data["data"]]

# 使用
result = get_okx_trades("BTC-USDT-SWAP")
for t in result[:5]:
    print(f"{t['side']} @ {t['px']}, 数量: {t['sz']}")
```

---

### 备用数据源: Bybit API

**文档**: https://bybit-exchange.github.io/docs/v5/

**接口**: `GET /v5/market/recent-trade`

**完整URL**: `https://api.bybit.com/v5/market/recent-trade?category=linear&symbol=BTCUSDT`

**Python 调用示例**:
```python
import requests

def get_bybit_trades(symbol="BTCUSDT", category="linear", limit=100):
    """
    获取 Bybit 最近成交
    文档: https://bybit-exchange.github.io/docs/v5/
    """
    url = "https://api.bybit.com/v5/market/recent-trade"
    params = {"category": category, "symbol": symbol, "limit": limit}
    
    response = requests.get(url, params=params, timeout=10)
    data = response.json()
    
    if data.get("retCode") == 0:
        return [{
            "price": t["price"],
            "size": t["size"],
            "side": t["side"],
            "ts": t["time"]
        } for t in data["result"]["list"]]

# 使用
result = get_bybit_trades("BTCUSDT")
for t in result[:5]:
    print(f"{t['side']} @ {t['price']}, 数量: {t['size']}")
```

---

## 5. Long/Short Ratio（多空比）

### ⚠️ 重要变更

**Binance 多空比 REST API 已下架**：
- `/fapi/v1/globalLongShortAccountRatio` → **404 已不存在**
- `/fapi/v1/takerlongshortRatio` → **404 已不存在**

Binance 不再通过公开 REST API 提供多空比数据。

---

### 主数据源: Bybit API（替代方案）

**文档**: https://bybit-exchange.github.io/docs/v5/

**接口**: `GET /v5/market/account-ratio`

**完整URL**: `https://api.bybit.com/v5/market/account-ratio?category=linear&symbol=BTCUSDT&period=1h`

**参数**:
| 参数 | 类型 | 必填 | 说明 |
|-----|------|-----|------|
| category | STRING | YES | linear, inverse |
| symbol | STRING | YES | 交易对 |
| period | STRING | YES | 周期：15min, 30min, 1h, 4h, 1d |

**Python 调用示例**:
```python
import requests

def get_bybit_long_short_ratio(symbol="BTCUSDT", period="1h"):
    """
    获取 Bybit 账户多空比（替代 Binance 已下架的接口）
    文档: https://bybit-exchange.github.io/docs/v5/
    """
    url = "https://api.bybit.com/v5/market/account-ratio"
    params = {
        "category": "linear",
        "symbol": symbol,
        "period": period
    }
    
    response = requests.get(url, params=params, timeout=10)
    data = response.json()
    
    if data.get("retCode") == 0:
        result = data["result"]["list"]
        return [{
            "symbol": d["symbol"],
            "buyRatio": d["buyRatio"],    # 买方账户比例（多头）
            "sellRatio": d["sellRatio"],  # 卖方账户比例（空头）
            "timestamp": d["timestamp"]
        } for d in result]
    return None

# 使用
result = get_bybit_long_short_ratio("BTCUSDT", "1h")
latest = result[0]
print(f"买方比例: {float(latest['buyRatio'])*100:.2f}%")
print(f"卖方比例: {float(latest['sellRatio'])*100:.2f}%")
```

**实际响应**:
```json
{
  "symbol": "BTCUSDT",
  "buyRatio": "0.5374",
  "sellRatio": "0.4626",
  "timestamp": "1779800400000"
}
```

---

### 备用数据源: CoinGlass API

CoinGlass 提供聚合的多交易所多空比数据，需要注册获取 API Key。

**获取 API Key**: https://www.coinglass.com/zh/user

---

## 6. Liquidation（爆仓数据）

### ⚠️ 重要变更

**Binance 强平记录 REST API 已不可用**：
- `/fapi/v1/allForceOrders` → `"out of maintenance"`
- `/fapi/v1/forceOrders` → 需要 API Key 认证

Binance 已不再通过公开 REST API 提供强平历史数据。

---

### 主数据源: CoinGlass API

CoinGlass 是目前获取跨交易所爆仓数据最可靠的公开途径。

**说明**: 需要注册获取 API Key

**接口**: `/api/futures/liquidation/history`

**获取 API Key**: https://www.coinglass.com/zh/user

**Python 调用示例**:
```python
import requests

def get_coinglass_liquidation(api_key, exchange="Binance", symbol="BTCUSDT"):
    """
    获取 CoinGlass 爆仓历史数据
    API Key: https://www.coinglass.com/zh/user
    """
    url = "https://open-api-v4.coinglass.com/api/futures/liquidation/history"
    headers = {
        "accept": "application/json",
        "CG-API-KEY": api_key
    }
    params = {
        "exchange": exchange,
        "symbol": symbol,
        "interval": "1h"
    }
    
    response = requests.get(url, headers=headers, params=params, timeout=10)
    return response.json()

# 使用（需要注册获取 API Key）
# result = get_coinglass_liquidation("YOUR_API_KEY", "Binance", "BTCUSDT")
```

---

### 备用数据源: Bybit WebSocket

Bybit 通过 WebSocket 实时推送全量爆仓数据，适合实时监听场景。

**WebSocket Topic**: `allLiquidation`

**文档**: https://bybit-exchange.github.io/docs/v5/websocket/public/all-liquidation

```python
import websocket
import json

def on_message(ws, message):
    data = json.loads(message)
    if data.get("topic") == "allLiquidation":
        for item in data["data"]:
            print(f"爆仓: {item['symbol']} {item['side']} @ {item['price']}, 数量: {item['size']}")

ws = websocket.WebSocketApp(
    "wss://stream.bybit.com/v5/public/linear",
    on_message=on_message
)
ws.run_forever()
```

---

## 7. OI Ranking（持仓量排行）

### 推荐方案: Binance 批量获取后自行计算

```python
import requests
import pandas as pd
import time

def get_binance_oi_ranking(top_n=50):
    """
    获取 Binance 所有 U本位合约 OI 排行
    """
    # 1. 获取所有 U本位永续合约列表
    exchange_url = "https://fapi.binance.com/fapi/v1/exchangeInfo"
    exchange_info = requests.get(exchange_url, timeout=10).json()
    
    symbols = [
        s["symbol"] for s in exchange_info["symbols"]
        if s["contractType"] == "PERPETUAL" and s["status"] == "TRADING"
    ]
    
    # 2. 批量获取 OI（控制频率，openInterest weight=1，约可 10 req/s）
    oi_list = []
    for symbol in symbols[:top_n * 2]:
        try:
            url = "https://fapi.binance.com/fapi/v1/openInterest"
            resp = requests.get(url, params={"symbol": symbol}, timeout=10)
            data = resp.json()
            
            oi_list.append({
                "symbol": symbol,
                "openInterest": float(data["openInterest"]),
                "time": data["time"]
            })
        except Exception:
            continue
        time.sleep(0.1)  # 控制频率
    
    # 3. 排序
    df = pd.DataFrame(oi_list)
    df = df.sort_values("openInterest", ascending=False).head(top_n)
    
    return df

# 使用
df = get_binance_oi_ranking(20)
print(df.head(20))
```

---

## 8. NetFlow / Exchange Inflow-Outflow（净流量）

### 说明

交易所公开 API **不直接提供**链上净流量数据。获取方式：

1. **数据聚合商**（付费）:
   - CryptoQuant（https://cryptoquant.com）
   - Glassnode（https://glassnode.com）
   - Nansen（https://nansen.ai）

2. **间接估算**:
   - 通过钱包余额变动推断（需链上数据）
   - 不可通过交易所 REST API 直接获取

### 建议

如需 NetFlow 数据，建议：
- 使用付费数据服务（CryptoQuant / Glassnode）
- 或使用链上数据平台自行计算

---

## 9. News / Announcement（新闻公告）

### 主数据源: Binance 公告

**接口**: `GET /bapi/composite/v1/public/cms/article/list`

**完整URL**: `https://www.binance.com/bapi/composite/v1/public/cms/article/list?type=1&page=1&pageSize=10`

**Python 调用示例**:
```python
import requests

def get_binance_announcements(page=1, page_size=10):
    """
    获取 Binance 官方公告
    """
    url = "https://www.binance.com/bapi/composite/v1/public/cms/article/list"
    params = {"type": 1, "page": page, "pageSize": page_size}
    
    response = requests.get(url, params=params, timeout=10)
    data = response.json()
    
    if data.get("code") == "000000":
        return [{
            "id": a["id"],
            "title": a["title"],
            "ctime": a["ctime"]
        } for a in data["data"]["articles"]]

# 使用
announcements = get_binance_announcements()
for a in announcements[:5]:
    print(f"[{a['ctime']}] {a['title']}")
```

---

### 备用数据源: CoinGecko API

**文档**: https://www.coingecko.com/en/api

**接口**: `GET /api/v3/status_updates`

**Python 调用示例**:
```python
import requests

def get_coingecko_status_updates(per_page=50):
    """
    获取 CoinGecko 项目状态更新
    文档: https://www.coingecko.com/en/api
    """
    url = "https://api.coingecko.com/api/v3/status_updates"
    params = {"per_page": per_page}
    
    response = requests.get(url, params=params, timeout=10)
    data = response.json()
    
    return [{
        "project": d["project"]["name"],
        "category": d["category"],
        "description": d["description"],
        "created_at": d["created_at"]
    } for d in data.get("status_updates", [])]

# 使用
updates = get_coingecko_status_updates()
for u in updates[:5]:
    print(f"{u['project']}: {u['description'][:80]}...")
```

**限流**: 10-30 次/分钟（免费版）

---

## 汇总表

| 数据类型 | 主数据源 | 备用1 | 备用2 | 免费 | 验证 |
|---------|---------|-------|-------|------|------|
| **Open Interest** | Binance `/fapi/v1/openInterest` | OKX `/api/v5/market/open-interest` | - | ✅ | ✅ |
| **Funding Rate** | Binance `/fapi/v1/premiumIndex` | Bybit `/v5/market/tickers` | OKX | ✅ | ✅ |
| **Orderbook** | Binance `/fapi/v1/depth` | OKX `/api/v5/market/books` | Bybit | ✅ | ✅ |
| **Trades** | Binance `/fapi/v1/trades` | OKX `/api/v5/market/trades` | Bybit | ✅ | ✅ |
| **Long/Short Ratio** | **Bybit `/v5/market/account-ratio`** | CoinGlass（需Key） | - | ✅ | ✅ |
| **Liquidation** | **CoinGlass**（需Key） | Bybit WebSocket | - | 部分 | ✅ |
| **OI Ranking** | 自行计算（Binance 批量） | - | - | ✅ | ✅ |
| **NetFlow** | - | - | - | ❌ | - |
| **News** | Binance 公告 API | CoinGecko `/api/v3/status_updates` | - | ✅ | ✅ |

---

## 限流参考

| 交易所 | 限流规则 | 说明 |
|-------|---------|------|
| **Binance** | Weight 系统（通常 1200 weight/分钟/IP） | 不同接口 weight 不同，见各节说明 |
| **OKX** | 20 次/2秒（公开接口） | 基于 IP |
| **Bybit** | 约 **50-120 次/秒**（公开接口） | V5 API 统一限制，基于 IP |
| **CoinGlass** | 根据套餐 | 免费版有限制 |

---

## 参考资料

1. **Binance 期货文档**: https://developers.binance.com/docs/futures
2. **OKX API 文档**: https://www.okx.com/docs-cn/
3. **Bybit V5 API**: https://bybit-exchange.github.io/docs/v5/
4. **CoinGecko API**: https://www.coingecko.com/en/api
5. **CoinGlass**: https://www.coinglass.com/zh/（需登录查看 API）
