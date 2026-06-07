// Package research provides offline, read-only diagnostics over the data the
// live trading system already persists (trader_positions, signal_calibration_samples).
//
// Goal: replace "tune the scoring by feel" with "let historical facts decide".
// This file implements the first, purely descriptive layer: it answers whether
// the system is structurally positive- or negative-expectancy AFTER costs, and
// where the P&L is actually coming from (which setup / direction / exit reason).
//
// It deliberately does NOT change any trading logic. It only reads.
package research

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"nofx/store"

	"gorm.io/gorm"
)

// DiagnoseOptions controls which closed trades are analysed.
type DiagnoseOptions struct {
	StrategyID string // optional: restrict to one strategy
	TraderID   string // optional: restrict to one trader
	Limit      int    // 0 = no limit
}

// TradeStat is the cost-aware performance summary of a group of closed trades.
//
// Cost convention: exchange "realized_pnl" is treated as gross of trading fees
// (Binance reports commission separately), so Net = RealizedPnL - Fee. This is
// stated explicitly so the number is not silently mis-read.
type TradeStat struct {
	Group        string
	Trades       int
	Wins         int
	Losses       int
	Scratches    int // net pnl == 0
	WinRate      float64
	RealizedPnL  float64 // sum of exchange realized pnl (gross of fee)
	TotalFee     float64
	NetPnL       float64 // RealizedPnL - TotalFee
	AvgNetPnL    float64 // expectancy per trade (the number that actually matters)
	AvgWin       float64 // mean net pnl of winning trades
	AvgLoss      float64 // mean |net pnl| of losing trades (positive number)
	RealizedRR   float64 // AvgWin / AvgLoss
	ProfitFactor float64 // sum(wins) / sum(|losses|)
	Notional     float64 // sum of entry notional (entry_price * quantity)
	FeeDragBps   float64 // TotalFee / Notional * 10000
}

// DiagnoseReport is the full descriptive diagnostic.
type DiagnoseReport struct {
	Options       DiagnoseOptions
	Overall       TradeStat
	BySetup       []TradeStat
	BySide        []TradeStat
	ByCloseReason []TradeStat
}

// LoadClosedPositions loads CLOSED positions matching the options.
func LoadClosedPositions(db *gorm.DB, opt DiagnoseOptions) ([]store.TraderPosition, error) {
	if db == nil {
		return nil, fmt.Errorf("nil database handle")
	}
	q := db.Where("status = ?", "CLOSED")
	if strings.TrimSpace(opt.StrategyID) != "" {
		q = q.Where("strategy_id = ?", strings.TrimSpace(opt.StrategyID))
	}
	if strings.TrimSpace(opt.TraderID) != "" {
		q = q.Where("trader_id = ?", strings.TrimSpace(opt.TraderID))
	}
	q = q.Order("exit_time DESC")
	if opt.Limit > 0 {
		q = q.Limit(opt.Limit)
	}
	var positions []store.TraderPosition
	if err := q.Find(&positions).Error; err != nil {
		return nil, fmt.Errorf("query closed positions: %w", err)
	}
	return positions, nil
}

// Diagnose builds the descriptive report from closed positions.
func Diagnose(db *gorm.DB, opt DiagnoseOptions) (*DiagnoseReport, error) {
	positions, err := LoadClosedPositions(db, opt)
	if err != nil {
		return nil, err
	}

	report := &DiagnoseReport{Options: opt}
	report.Overall = aggregate("ALL", positions)

	report.BySetup = aggregateBy(positions, func(p store.TraderPosition) string {
		s := strings.TrimSpace(p.OpeningSetup)
		if s == "" {
			s = strings.TrimSpace(p.OpeningRuleID)
		}
		if s == "" {
			return "unclassified"
		}
		return s
	})
	report.BySide = aggregateBy(positions, func(p store.TraderPosition) string {
		side := strings.ToLower(strings.TrimSpace(p.Side))
		if side == "" {
			return "unknown"
		}
		return side
	})
	report.ByCloseReason = aggregateBy(positions, func(p store.TraderPosition) string {
		r := strings.TrimSpace(p.CloseReason)
		if r == "" {
			return "unknown"
		}
		return r
	})

	return report, nil
}

func aggregateBy(positions []store.TraderPosition, key func(store.TraderPosition) string) []TradeStat {
	groups := map[string][]store.TraderPosition{}
	order := []string{}
	for _, p := range positions {
		k := key(p)
		if _, ok := groups[k]; !ok {
			order = append(order, k)
		}
		groups[k] = append(groups[k], p)
	}
	stats := make([]TradeStat, 0, len(groups))
	for _, k := range order {
		stats = append(stats, aggregate(k, groups[k]))
	}
	// Largest sample first so the most statistically meaningful groups lead.
	sort.Slice(stats, func(i, j int) bool { return stats[i].Trades > stats[j].Trades })
	return stats
}

func aggregate(group string, positions []store.TraderPosition) TradeStat {
	stat := TradeStat{Group: group, Trades: len(positions)}
	var sumWin, sumLossAbs float64
	for _, p := range positions {
		net := p.RealizedPnL - p.Fee
		stat.RealizedPnL += p.RealizedPnL
		stat.TotalFee += p.Fee
		stat.NetPnL += net
		stat.Notional += math.Abs(p.EntryPrice * p.Quantity)
		switch {
		case net > 0:
			stat.Wins++
			sumWin += net
		case net < 0:
			stat.Losses++
			sumLossAbs += -net
		default:
			stat.Scratches++
		}
	}
	if stat.Trades > 0 {
		stat.AvgNetPnL = stat.NetPnL / float64(stat.Trades)
		decided := stat.Wins + stat.Losses
		if decided > 0 {
			stat.WinRate = float64(stat.Wins) / float64(decided)
		}
	}
	if stat.Wins > 0 {
		stat.AvgWin = sumWin / float64(stat.Wins)
	}
	if stat.Losses > 0 {
		stat.AvgLoss = sumLossAbs / float64(stat.Losses)
	}
	if stat.AvgLoss > 0 {
		stat.RealizedRR = stat.AvgWin / stat.AvgLoss
	}
	if sumLossAbs > 0 {
		stat.ProfitFactor = sumWin / sumLossAbs
	} else if sumWin > 0 {
		stat.ProfitFactor = math.Inf(1)
	}
	if stat.Notional > 0 {
		stat.FeeDragBps = stat.TotalFee / stat.Notional * 10000
	}
	return stat
}
