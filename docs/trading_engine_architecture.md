# Trading Engine Architecture

## Core Flow

User prompt is compiled once into structured rules and stored in strategy config.

Runtime flow:

1. Fetch market data with enough calculation history.
2. Convert market data into structured factor snapshots.
3. Signal engine checks compiled rules against factor snapshots.
4. LLM trading engine reviews only candidate signals and context.
5. Risk gate enforces leverage, position size, stop loss, take profit, and risk ratio.
6. Approved decisions are passed to the existing execution layer.

## Engine Boundaries

LLM trading engine:

- Reads candidate signals, structured factors, current positions, and trade memory.
- Reviews risk and market context.
- Does not calculate indicators.
- Does not infer support, resistance, or Fibonacci anchors from raw K-lines.
- Does not directly bypass the risk gate.

Indicator and factor engine:

- Calculates K-line derived indicators such as EMA, RSI, ATR, BOLL, and MACD.
- Adapts external data such as funding rate and open interest into factor snapshots.
- Exposes structure placeholders for parameterized indicators such as Fibonacci and support/resistance.
- Marks unavailable factors explicitly instead of letting the LLM guess.

## K-line Windows

`compute_lookback` is used for deterministic calculation.

`prompt_display_count` is only the raw K-line window shown to AI or preview.

This avoids sending excessive raw K-lines to the LLM while keeping indicator calculation stable.

## Current Implementation Scope

Implemented:

- New trading flow interfaces.
- Rule-based signal engine.
- LLM review engine.
- Risk gate adapter using existing validation.
- Factor snapshot adapter from existing market data.
- Separate calculation lookback and prompt display count.
- Strategy config support for compiled rules.

Not implemented yet:

- Natural language strategy compiler.
- Persistent trade memory store.
- Real Fibonacci/support/resistance structure detection.
- Rule-driven sizing, stop loss, and take profit generation.
