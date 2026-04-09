"""
Kronos K-line Prediction Service
Wraps the Kronos financial foundation model as an HTTP API for nofx-V2.

Usage:
    python server.py
    # Service runs at http://localhost:8100
    # Test: http://localhost:8100/predict?symbol=BTCUSDT&timeframe=5m

Requires:
    - Kronos repo cloned into ./Kronos/
    - PyTorch with CUDA (RTX 4060)
    - See requirements.txt
"""

import sys
import os
import time
import logging
import sqlite3
import threading
from datetime import datetime, timedelta, timezone
from pathlib import Path

import pandas as pd
import numpy as np
import requests as http_requests
import torch
from fastapi import FastAPI, Query, HTTPException
from fastapi.responses import JSONResponse
import uvicorn

# Add Kronos repo to Python path
KRONOS_DIR = Path(__file__).parent / "Kronos"
sys.path.insert(0, str(KRONOS_DIR))

# ---------------------------------------------------------------------------
# Logging
# ---------------------------------------------------------------------------
logging.basicConfig(level=logging.INFO, format="%(asctime)s [%(levelname)s] %(message)s")
logger = logging.getLogger("kronos_service")

# ---------------------------------------------------------------------------
# Config
# ---------------------------------------------------------------------------
MODEL_ID = os.getenv("KRONOS_MODEL", "NeoQuasar/Kronos-base")         # 102.3M params
TOKENIZER_ID = os.getenv("KRONOS_TOKENIZER", "NeoQuasar/Kronos-Tokenizer-base")
MAX_CONTEXT = 512   # max K-line bars for small/base
PORT = int(os.getenv("KRONOS_PORT", "8100"))
DEVICE = "cuda" if torch.cuda.is_available() else "cpu"

# ---------------------------------------------------------------------------
# Globals (loaded once at startup)
# ---------------------------------------------------------------------------
predictor = None

# ---------------------------------------------------------------------------
# Prediction Log (SQLite)
# ---------------------------------------------------------------------------
DB_PATH = Path(__file__).parent / "kronos_predictions.db"
_db_lock = threading.Lock()


def _init_db():
    with sqlite3.connect(str(DB_PATH)) as conn:
        conn.execute("""
            CREATE TABLE IF NOT EXISTS predictions (
                id INTEGER PRIMARY KEY AUTOINCREMENT,
                created_at TEXT NOT NULL,
                symbol TEXT NOT NULL,
                timeframe TEXT NOT NULL,
                pred_len INTEGER NOT NULL,
                current_price REAL NOT NULL,
                predicted_close REAL NOT NULL,
                predicted_high REAL NOT NULL,
                predicted_low REAL NOT NULL,
                change_pct REAL NOT NULL,
                direction TEXT NOT NULL,
                target_time TEXT NOT NULL,
                actual_close REAL DEFAULT NULL,
                actual_high REAL DEFAULT NULL,
                actual_low REAL DEFAULT NULL,
                verified INTEGER DEFAULT 0
            )
        """)
        conn.execute("CREATE INDEX IF NOT EXISTS idx_pred_verified ON predictions(verified, target_time)")


def _log_prediction(symbol, timeframe, pred_len, current_price, predicted_close,
                    predicted_high, predicted_low, change_pct, direction, target_time):
    with _db_lock:
        with sqlite3.connect(str(DB_PATH)) as conn:
            conn.execute(
                """INSERT INTO predictions
                   (created_at, symbol, timeframe, pred_len, current_price, predicted_close,
                    predicted_high, predicted_low, change_pct, direction, target_time)
                   VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)""",
                (datetime.now(timezone.utc).isoformat(), symbol, timeframe, pred_len,
                 current_price, predicted_close, predicted_high, predicted_low,
                 change_pct, direction, target_time),
            )


def _verify_pending():
    """Check unverified predictions whose target_time has passed, fetch actual prices."""
    now = datetime.now(timezone.utc)
    with _db_lock:
        with sqlite3.connect(str(DB_PATH)) as conn:
            conn.row_factory = sqlite3.Row
            rows = conn.execute(
                "SELECT * FROM predictions WHERE verified = 0 AND target_time < ? ORDER BY target_time",
                (now.isoformat(),),
            ).fetchall()

    if not rows:
        return 0

    # Limit to 5 verifications per call to avoid timeout
    rows = rows[:5]

    updated = 0
    for row in rows:
        try:
            symbol = row["symbol"]
            timeframe = row["timeframe"]
            pred_len = row["pred_len"]
            # Fetch actual klines covering the prediction period
            df = fetch_klines(symbol, timeframe, pred_len + 5)
            if df.empty:
                continue
            target_ts = datetime.fromisoformat(row["target_time"])
            # Find the bar closest to target_time
            df["ts_diff"] = abs(df["timestamp"] - target_ts)
            closest_idx = df["ts_diff"].idxmin()
            # Actual values over the predicted period (last pred_len bars)
            recent = df.tail(pred_len)
            actual_close = float(recent["close"].iloc[-1])
            actual_high = float(recent["high"].max())
            actual_low = float(recent["low"].min())

            with _db_lock:
                with sqlite3.connect(str(DB_PATH)) as conn:
                    conn.execute(
                        "UPDATE predictions SET actual_close=?, actual_high=?, actual_low=?, verified=1 WHERE id=?",
                        (actual_close, actual_high, actual_low, row["id"]),
                    )
            updated += 1
        except Exception as e:
            logger.warning(f"Failed to verify prediction #{row['id']}: {e}")

    return updated

# ---------------------------------------------------------------------------
# Binance data fetcher
# ---------------------------------------------------------------------------
TIMEFRAME_MINUTES = {
    "1m": 1, "3m": 3, "5m": 5, "15m": 15, "30m": 30,
    "1h": 60, "2h": 120, "4h": 240, "6h": 360, "12h": 720, "1d": 1440,
}

def fetch_klines(symbol: str, timeframe: str, limit: int) -> pd.DataFrame:
    """Fetch OHLCV klines from Binance Futures API."""
    url = "https://fapi.binance.com/fapi/v1/klines"
    params = {"symbol": symbol, "interval": timeframe, "limit": limit}
    try:
        resp = http_requests.get(url, params=params, timeout=10)
        resp.raise_for_status()
    except Exception as e:
        raise HTTPException(status_code=502, detail=f"Failed to fetch klines from Binance: {e}")

    data = resp.json()
    if not data:
        raise HTTPException(status_code=404, detail=f"No kline data for {symbol} {timeframe}")

    df = pd.DataFrame(data, columns=[
        "timestamp", "open", "high", "low", "close", "volume",
        "close_time", "amount", "trades", "taker_buy_vol", "taker_buy_amt", "ignore",
    ])
    df["timestamp"] = pd.to_datetime(df["timestamp"], unit="ms")
    for col in ["open", "high", "low", "close", "volume", "amount"]:
        df[col] = df[col].astype(float)
    return df[["timestamp", "open", "high", "low", "close", "volume", "amount"]]


# ---------------------------------------------------------------------------
# FastAPI app
# ---------------------------------------------------------------------------
app = FastAPI(title="Kronos Prediction Service", version="1.0.0")


@app.on_event("startup")
def load_model():
    """Load Kronos model + tokenizer once at startup."""
    global predictor
    from model import Kronos, KronosTokenizer, KronosPredictor

    _init_db()
    logger.info(f"Prediction log DB: {DB_PATH}")
    logger.info(f"Device: {DEVICE}")
    if DEVICE == "cuda":
        gpu_name = torch.cuda.get_device_name(0)
        gpu_mem = torch.cuda.get_device_properties(0).total_memory / 1024**3
        logger.info(f"GPU: {gpu_name} ({gpu_mem:.1f} GB)")

    logger.info(f"Loading tokenizer: {TOKENIZER_ID}")
    tokenizer = KronosTokenizer.from_pretrained(TOKENIZER_ID)

    logger.info(f"Loading model: {MODEL_ID}")
    model = Kronos.from_pretrained(MODEL_ID)
    model = model.to(DEVICE)
    model.eval()
    logger.info(f"Model loaded on {DEVICE}, params: {sum(p.numel() for p in model.parameters()):,}")

    predictor = KronosPredictor(model, tokenizer, max_context=MAX_CONTEXT)
    logger.info("KronosPredictor ready.")


@app.get("/health")
def health():
    return {
        "status": "ok",
        "model": MODEL_ID,
        "device": DEVICE,
        "gpu": torch.cuda.get_device_name(0) if DEVICE == "cuda" else None,
    }


@app.get("/predict")
def predict(
    symbol: str = Query(default="BTCUSDT", description="Trading pair"),
    timeframe: str = Query(default="5m", description="K-line timeframe"),
    lookback: int = Query(default=400, ge=50, le=2000, description="Number of historical bars"),
    pred_len: int = Query(default=12, ge=1, le=100, description="Number of bars to predict"),
    samples: int = Query(default=5, ge=1, le=20, description="Number of sample paths to average"),
    temperature: float = Query(default=0.8, ge=0.1, le=2.0, description="Sampling temperature"),
):
    """Run Kronos prediction for a given symbol."""
    global predictor
    if predictor is None:
        raise HTTPException(status_code=503, detail="Model not loaded yet")

    if timeframe not in TIMEFRAME_MINUTES:
        raise HTTPException(status_code=400, detail=f"Unsupported timeframe: {timeframe}")

    t0 = time.time()

    # 1. Fetch klines
    df = fetch_klines(symbol, timeframe, lookback + 10)
    df = df.tail(lookback).reset_index(drop=True)

    x_df = df[["open", "high", "low", "close", "volume", "amount"]]
    x_timestamp = df["timestamp"]

    # 2. Generate future timestamps
    delta = timedelta(minutes=TIMEFRAME_MINUTES[timeframe])
    last_ts = x_timestamp.iloc[-1]
    y_timestamp = pd.Series([last_ts + delta * (i + 1) for i in range(pred_len)])

    # 3. Run prediction
    try:
        with torch.no_grad():
            pred_df = predictor.predict(
                df=x_df,
                x_timestamp=x_timestamp,
                y_timestamp=y_timestamp,
                pred_len=pred_len,
                T=temperature,
                top_p=0.9,
                sample_count=samples,
            )
    except Exception as e:
        logger.error(f"Prediction failed for {symbol}: {e}")
        raise HTTPException(status_code=500, detail=f"Prediction failed: {e}")

    elapsed_ms = int((time.time() - t0) * 1000)

    # 4. Build response
    last_close = float(df["close"].iloc[-1])
    pred_close_end = float(pred_df["close"].iloc[-1])
    change_pct = ((pred_close_end - last_close) / last_close) * 100

    # Determine trend direction
    if change_pct > 0.15:
        direction = "bullish"
    elif change_pct < -0.15:
        direction = "bearish"
    else:
        direction = "neutral"

    # Predicted high/low range
    pred_high = float(pred_df["high"].max())
    pred_low = float(pred_df["low"].min())

    # Mid-point prediction (halfway through the forecast)
    mid_idx = pred_len // 2
    pred_close_mid = float(pred_df["close"].iloc[mid_idx])
    mid_change_pct = ((pred_close_mid - last_close) / last_close) * 100

    # Trend consistency: are most bars moving in the same direction?
    closes = [float(pred_df["close"].iloc[i]) for i in range(len(pred_df))]
    up_bars = sum(1 for i in range(1, len(closes)) if closes[i] > closes[i-1])
    trend_consistency = round(up_bars / max(len(closes)-1, 1) * 100, 1)

    # 5. Log prediction for accuracy tracking
    target_time = str(y_timestamp.iloc[-1])
    try:
        _log_prediction(symbol, timeframe, pred_len, last_close, pred_close_end,
                        pred_high, pred_low, change_pct, direction, target_time)
    except Exception as e:
        logger.warning(f"Failed to log prediction: {e}")

    return {
        "symbol": symbol,
        "direction": direction,
        "current_price": round(last_close, 4),
        "predicted_close": round(pred_close_end, 4),
        "change_pct": round(change_pct, 4),
        "predicted_range": f"{round(pred_low, 2)}-{round(pred_high, 2)}",
        "mid_point_change_pct": round(mid_change_pct, 4),
        "trend_consistency": f"{trend_consistency}% bars up",
        "forecast_period": f"{pred_len} x {timeframe}",
        "elapsed_ms": elapsed_ms,
    }


@app.get("/predict_multi")
def predict_multi(
    symbols: str = Query(default="BTCUSDT,ETHUSDT", description="Comma-separated symbols"),
    timeframe: str = Query(default="5m"),
    lookback: int = Query(default=400, ge=50, le=2000),
    pred_len: int = Query(default=12, ge=1, le=100),
    samples: int = Query(default=3, ge=1, le=10),
):
    """Predict multiple symbols in one call (for AI context)."""
    symbol_list = [s.strip() for s in symbols.split(",") if s.strip()]
    results = []
    for sym in symbol_list:
        try:
            result = predict(
                symbol=sym, timeframe=timeframe, lookback=lookback,
                pred_len=pred_len, samples=samples,
            )
            # Compact format for AI context
            results.append({
                "symbol": sym,
                "direction": result["direction"],
                "change_pct": result["change_pct"],
                "current": result["current_price"],
                "predicted_close": result["predicted_close"],
                "predicted_range": f"{result['predicted_low']}-{result['predicted_high']}",
            })
        except Exception as e:
            logger.warning(f"Failed to predict {sym}: {e}")
            results.append({"symbol": sym, "error": str(e)})

    return {"predictions": results}


@app.get("/report")
def report(
    symbol: str = Query(default="", description="Filter by symbol (empty=all)"),
    limit: int = Query(default=100, ge=1, le=1000),
):
    """
    Accuracy report based on real production predictions.
    Automatically verifies past predictions against actual prices.
    """
    # 1. Verify any pending predictions whose target time has passed (async, best-effort)
    try:
        verified_count = _verify_pending()
        if verified_count > 0:
            logger.info(f"Verified {verified_count} pending predictions")
    except Exception as e:
        logger.warning(f"Verification failed (will retry next call): {e}")

    # 2. Query verified predictions
    with _db_lock:
        with sqlite3.connect(str(DB_PATH)) as conn:
            conn.row_factory = sqlite3.Row
            if symbol:
                rows = conn.execute(
                    "SELECT * FROM predictions WHERE verified = 1 AND symbol = ? ORDER BY created_at DESC LIMIT ?",
                    (symbol, limit),
                ).fetchall()
            else:
                rows = conn.execute(
                    "SELECT * FROM predictions WHERE verified = 1 ORDER BY created_at DESC LIMIT ?",
                    (limit,),
                ).fetchall()

    if not rows:
        # Also count pending
        with sqlite3.connect(str(DB_PATH)) as conn:
            pending = conn.execute("SELECT COUNT(*) FROM predictions WHERE verified = 0").fetchone()[0]
        return {
            "message": "No verified predictions yet. Predictions need time to mature before verification.",
            "pending_predictions": pending,
            "tip": "Predictions are verified after their target_time passes. Keep the service running.",
        }

    # 3. Calculate stats
    direction_correct = 0
    price_errors = []
    range_hits = 0
    by_symbol = {}
    details = []

    for row in rows:
        sym = row["symbol"]
        current = row["current_price"]
        pred_close = row["predicted_close"]
        actual_close = row["actual_close"]
        pred_high = row["predicted_high"]
        pred_low = row["predicted_low"]

        # Actual direction
        actual_change = ((actual_close - current) / current) * 100
        pred_change = row["change_pct"]

        actual_dir = "bullish" if actual_change > 0.15 else ("bearish" if actual_change < -0.15 else "neutral")
        pred_dir = row["direction"]

        # Direction match
        dir_match = (actual_dir == pred_dir) or \
                    (actual_dir != "neutral" and pred_dir != "neutral" and
                     (actual_change > 0) == (pred_change > 0))

        if dir_match:
            direction_correct += 1

        # Price error
        err = abs(pred_close - actual_close) / current * 100
        price_errors.append(err)

        # Range hit
        in_range = pred_low <= actual_close <= pred_high
        if in_range:
            range_hits += 1

        # Per-symbol stats
        if sym not in by_symbol:
            by_symbol[sym] = {"total": 0, "dir_correct": 0, "range_hits": 0, "errors": []}
        by_symbol[sym]["total"] += 1
        if dir_match:
            by_symbol[sym]["dir_correct"] += 1
        if in_range:
            by_symbol[sym]["range_hits"] += 1
        by_symbol[sym]["errors"].append(err)

        details.append({
            "time": row["created_at"],
            "symbol": sym,
            "pred_dir": pred_dir,
            "actual_dir": actual_dir,
            "dir_correct": dir_match,
            "pred_close": round(pred_close, 2),
            "actual_close": round(actual_close, 2),
            "error_pct": round(err, 4),
            "pred_range": f"{round(pred_low, 2)}-{round(pred_high, 2)}",
            "in_range": in_range,
        })

    n = len(rows)

    # Per-symbol summary
    symbol_summary = {}
    for sym, stats in by_symbol.items():
        t = stats["total"]
        symbol_summary[sym] = {
            "predictions": t,
            "direction_accuracy": f"{stats['dir_correct']}/{t} ({stats['dir_correct']/t*100:.1f}%)",
            "avg_price_error": f"{sum(stats['errors'])/t:.4f}%",
            "range_hit_rate": f"{stats['range_hits']}/{t} ({stats['range_hits']/t*100:.1f}%)",
        }

    # Pending count
    with sqlite3.connect(str(DB_PATH)) as conn:
        pending = conn.execute("SELECT COUNT(*) FROM predictions WHERE verified = 0").fetchone()[0]

    return {
        "model": MODEL_ID,
        "total_verified": n,
        "pending_verification": pending,
        "overall": {
            "direction_accuracy": f"{direction_correct}/{n} ({direction_correct/n*100:.1f}%)",
            "avg_price_error": f"{sum(price_errors)/n:.4f}%",
            "range_hit_rate": f"{range_hits}/{n} ({range_hits/n*100:.1f}%)",
        },
        "by_symbol": symbol_summary,
        "recent_predictions": details[:20],
    }


if __name__ == "__main__":
    logger.info(f"Starting Kronos Prediction Service on port {PORT}...")
    uvicorn.run(app, host="0.0.0.0", port=PORT)
