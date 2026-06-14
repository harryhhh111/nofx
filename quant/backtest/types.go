package backtest

import (
	"time"

	"nofx/market"
)

type Side string

const (
	SideLong  Side = "long"
	SideShort Side = "short"
)

type EntryType string

const (
	EntryTypeMarket EntryType = "market"
)

type ExitReason string

const (
	ExitReasonStopLoss   ExitReason = "stop_loss"
	ExitReasonTakeProfit ExitReason = "take_profit"
	ExitReasonEndOfData  ExitReason = "end_of_data"
)

type Strategy interface {
	Name() string
	OnBar(ctx BarContext) ([]StrategySignal, error)
}

type StrategyFunc struct {
	StrategyName string
	Fn           func(ctx BarContext) ([]StrategySignal, error)
}

func (s StrategyFunc) Name() string {
	if s.StrategyName == "" {
		return "strategy_func"
	}
	return s.StrategyName
}

func (s StrategyFunc) OnBar(ctx BarContext) ([]StrategySignal, error) {
	if s.Fn == nil {
		return nil, nil
	}
	return s.Fn(ctx)
}

type BacktestRun struct {
	RunID         string    `json:"run_id"`
	Strategy      string    `json:"strategy"`
	Symbol        string    `json:"symbol"`
	Timeframe     string    `json:"timeframe"`
	StartTime     time.Time `json:"start_time"`
	EndTime       time.Time `json:"end_time"`
	InitialEquity float64   `json:"initial_equity"`
	FeeModel      string    `json:"fee_model"`
	SlippageModel string    `json:"slippage_model"`
	ConfigVersion string    `json:"config_version"`
}

type Config struct {
	RunID             string
	Symbol            string
	Timeframe         string
	InitialEquity     float64
	FixedQuantity     float64
	FixedNotionalUSDT float64
	TakerFeeRate      float64
	SlippageBps       float64
	ConfigVersion     string
}

type BarContext struct {
	Run      BacktestRun
	Bar      market.Kline
	BarIndex int
	Time     time.Time
	Equity   float64
}

type FeatureSnapshot struct {
	Symbol    string    `json:"symbol"`
	Time      time.Time `json:"time"`
	Timeframe string    `json:"timeframe"`
	Open      float64   `json:"open"`
	High      float64   `json:"high"`
	Low       float64   `json:"low"`
	Close     float64   `json:"close"`
	Volume    float64   `json:"volume"`
}

type StrategySignal struct {
	Strategy   string    `json:"strategy"`
	Symbol     string    `json:"symbol"`
	Side       Side      `json:"side"`
	SignalTime time.Time `json:"signal_time"`
	EntryType  EntryType `json:"entry_type"`
	StopLoss   float64   `json:"stop_loss"`
	TakeProfit float64   `json:"take_profit"`
	Confidence float64   `json:"confidence,omitempty"`
	Reason     string    `json:"reason,omitempty"`
}

type RiskDecision struct {
	Action       string   `json:"action"`
	PositionSize float64  `json:"position_size"`
	RiskUSDT     float64  `json:"risk_usdt,omitempty"`
	Reasons      []string `json:"reasons,omitempty"`
}

type BacktestTrade struct {
	TradeID                  string          `json:"trade_id"`
	Symbol                   string          `json:"symbol"`
	Strategy                 string          `json:"strategy"`
	Side                     Side            `json:"side"`
	Quantity                 float64         `json:"quantity"`
	EntryTime                time.Time       `json:"entry_time"`
	EntryPrice               float64         `json:"entry_price"`
	ExitTime                 time.Time       `json:"exit_time"`
	ExitPrice                float64         `json:"exit_price"`
	ExitReason               ExitReason      `json:"exit_reason"`
	GrossPnL                 float64         `json:"gross_pnl"`
	Fees                     float64         `json:"fees"`
	Funding                  float64         `json:"funding"`
	NetPnL                   float64         `json:"net_pnl"`
	MaxFavorableExcursionPct float64         `json:"max_favorable_excursion_pct"`
	MaxAdverseExcursionPct   float64         `json:"max_adverse_excursion_pct"`
	Signal                   StrategySignal  `json:"signal"`
	FeatureSnapshot          FeatureSnapshot `json:"feature_snapshot"`
	RiskDecision             RiskDecision    `json:"risk_decision"`
}

type Metrics struct {
	InitialEquity       float64 `json:"initial_equity"`
	FinalEquity         float64 `json:"final_equity"`
	TotalReturnPct      float64 `json:"total_return_pct"`
	TotalTrades         int     `json:"total_trades"`
	WinningTrades       int     `json:"winning_trades"`
	LosingTrades        int     `json:"losing_trades"`
	WinRatePct          float64 `json:"win_rate_pct"`
	GrossPnL            float64 `json:"gross_pnl"`
	NetPnL              float64 `json:"net_pnl"`
	Fees                float64 `json:"fees"`
	AvgWin              float64 `json:"avg_win"`
	AvgLoss             float64 `json:"avg_loss"`
	ProfitFactor        float64 `json:"profit_factor"`
	MaxDrawdownPct      float64 `json:"max_drawdown_pct"`
	FeeToGrossProfitPct float64 `json:"fee_to_gross_profit_pct"`
}

type Result struct {
	Run         BacktestRun     `json:"run"`
	Trades      []BacktestTrade `json:"trades"`
	EquityCurve []EquityPoint   `json:"equity_curve"`
	Metrics     Metrics         `json:"metrics"`
}

type EquityPoint struct {
	Time   time.Time `json:"time"`
	Equity float64   `json:"equity"`
}
