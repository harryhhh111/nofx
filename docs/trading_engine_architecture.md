# Trading Engine Architecture

## Runtime Flow

The live and paper-trading path is deterministic:

1. Load closed K-line windows and available external factors.
2. Calculate indicators, swing structure, market regime, and setup candidates.
3. Evaluate setup-specific evidence across primary, entry, and confirmation timeframes.
4. Select structural invalidation and target anchors for the setup type.
5. Apply deterministic signal review, position sizing, and the risk gate.
6. Execute approved actions and persist the decision, setup episode, thesis, and execution evidence.

No LLM call is made in this path. An unavailable AI provider cannot block a trading cycle.

## Setup-Centered Decisions

The engine first identifies a trading setup. Factor scores explain whether the setup has enough support; they do not independently create a trade.

Supported setup families include:

- trend continuation and trend pullback;
- breakout;
- failed breakout, range reversal, and support/resistance bounce;
- momentum exhaustion;
- explicit no-trade states.

Market-context handling is setup aware. A directional conflict may reject a trend setup while remaining a warning for a valid reversal setup.

## Protective Levels

Stop-loss and take-profit selection is setup aware:

- trend setups use primary-timeframe trend invalidation;
- breakouts use the broken range or failed retest boundary;
- range and bounce setups use zone boundaries;
- failed breakouts and exhaustion setups use the relevant extreme.

Structural risk/reward is calculated from the structural stop anchor and structural target. The ATR buffer only widens the executable stop and affects sizing; it never moves the target or rewrites structural risk/reward.

## Setup Episodes

`setup_episodes` stores one contiguous market opportunity instead of treating every scheduler scan as an independent sample.

An episode records:

- strategy version, symbol, setup family, direction, and regime;
- first and latest factor/setup evidence;
- observed, triggered, approved, executed, and ended lifecycle states;
- structural stop, executable stop, target, and both RR values;
- MFE/MAE and forward labels based on structural target, invalidation, or horizon return.

Repeated scans of the same closed primary bar update the same sample. K-lines are stored once in `calibration_klines` and replay windows are reconstructed at the original cutoff time.

## Offline Calibration

Calibration reports aggregate independent episodes by regime, setup, action, and factor. Parameter replay recalculates indicators and structure from the historical K-lines; it is a classification-stability scan, not a complete PnL backtest.

Automated evolution is deliberately disabled. Once evidence gates are met, AI may generate an unsaved parameter proposal. A user must review, save, and activate a new strategy version.

## AI Boundary

AI remains optional for explicit, asynchronous work:

- compiling a natural-language strategy into structured configuration;
- explaining calibration evidence and proposing parameter changes.

AI does not calculate indicators, choose live protective levels, review every live candidate, execute orders, or silently modify a running strategy.
