"""Patch strategy config to add Kronos external data sources."""
import json
import psycopg2

STRATEGY_ID = "d9da8725-1118-4b27-8de3-4672943c6b29"

conn = psycopg2.connect(
    host="192.168.66.221",
    port=5432,
    user="postgres",
    password="Haiyang123!!!",
    dbname="nofx",
)
cur = conn.cursor()

# 1. Read current config
cur.execute("SELECT name, config FROM strategies WHERE id = %s", (STRATEGY_ID,))
row = cur.fetchone()
if not row:
    print(f"Strategy {STRATEGY_ID} not found!")
    exit(1)

name, config_str = row
config = json.loads(config_str)

print(f"Strategy: {name}")
print(f"Current external_data_sources: {json.dumps(config.get('indicators', {}).get('external_data_sources', []), indent=2)}")

# 2. Add Kronos sources
kronos_sources = [
    {
        "name": "kronos_btc",
        "url": "http://localhost:8100/predict?symbol=BTCUSDT&timeframe=5m&pred_len=12&samples=5",
        "method": "GET",
        "context_label": "Kronos BTC Forecast (Supplementary)",
        "description": "Kronos-base (102M params) K-line pattern prediction model. It only sees price/volume — no OI, no fund flows, no news. Backtested: BTC direction accuracy ~60%, price error ~0.3%, range hit rate 60-80%. Use as a supplementary signal (weight ~10%): if Kronos direction agrees with your analysis → increase confidence by +5-8. If Kronos disagrees → decrease confidence by -3-5 but do NOT reverse your decision. NEVER open or close based on Kronos alone.",
        "refresh_secs": 180,
    },
    {
        "name": "kronos_eth",
        "url": "http://localhost:8100/predict?symbol=ETHUSDT&timeframe=5m&pred_len=12&samples=5",
        "method": "GET",
        "context_label": "Kronos ETH Forecast (Supplementary)",
        "description": "Kronos-base prediction for ETH. Direction accuracy ~30% (weaker than BTC), but price range hit rate ~80%. Use predicted range as expected price reference. Weight ~10%.",
        "refresh_secs": 180,
    },
]

if "indicators" not in config:
    config["indicators"] = {}

config["indicators"]["external_data_sources"] = kronos_sources

# 3. Write back
new_config_str = json.dumps(config, ensure_ascii=False)
cur.execute("UPDATE strategies SET config = %s, updated_at = NOW() WHERE id = %s", (new_config_str, STRATEGY_ID))
conn.commit()

print(f"\nUpdated! New external_data_sources:")
print(json.dumps(kronos_sources, indent=2, ensure_ascii=False))

cur.close()
conn.close()
