package store

import (
	"encoding/json"
	"fmt"
	"nofx/market"
	"sort"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// CalibrationKline stores each market bar once. Replay samples reference the
// primary close time instead of embedding the same multi-timeframe window on
// every scan.
type CalibrationKline struct {
	Source              string  `gorm:"primaryKey;size:24" json:"source"`
	Symbol              string  `gorm:"primaryKey;size:32" json:"symbol"`
	Timeframe           string  `gorm:"primaryKey;size:12" json:"timeframe"`
	OpenTime            int64   `gorm:"primaryKey;autoIncrement:false" json:"open_time"`
	Open                float64 `gorm:"not null" json:"open"`
	High                float64 `gorm:"not null" json:"high"`
	Low                 float64 `gorm:"not null" json:"low"`
	Close               float64 `gorm:"not null" json:"close"`
	Volume              float64 `gorm:"not null" json:"volume"`
	CloseTime           int64   `gorm:"not null;index" json:"close_time"`
	QuoteVolume         float64 `gorm:"not null" json:"quote_volume"`
	Trades              int     `gorm:"not null" json:"trades"`
	TakerBuyBaseVolume  float64 `gorm:"not null" json:"taker_buy_base_volume"`
	TakerBuyQuoteVolume float64 `gorm:"not null" json:"taker_buy_quote_volume"`
}

func (CalibrationKline) TableName() string { return "calibration_klines" }

func upsertCalibrationKlines(tx *gorm.DB, samples []*SignalCalibrationSample) error {
	rowsByKey := map[string]CalibrationKline{}
	for _, sample := range samples {
		if sample == nil || strings.TrimSpace(sample.KlineWindowsJSON) == "" {
			continue
		}
		var windows map[string][]market.Kline
		if err := json.Unmarshal([]byte(sample.KlineWindowsJSON), &windows); err != nil {
			return fmt.Errorf("parse calibration kline windows for %s: %w", sample.Symbol, err)
		}
		source := strings.ToLower(strings.TrimSpace(sample.MarketDataSource))
		if source == "" {
			source = "default"
		}
		symbol := strings.ToUpper(strings.TrimSpace(sample.Symbol))
		for timeframe, bars := range windows {
			timeframe = strings.TrimSpace(timeframe)
			if symbol == "" || timeframe == "" {
				continue
			}
			for _, bar := range bars {
				if bar.OpenTime <= 0 || bar.CloseTime <= 0 {
					continue
				}
				row := CalibrationKline{
					Source: source, Symbol: symbol, Timeframe: timeframe, OpenTime: bar.OpenTime,
					Open: bar.Open, High: bar.High, Low: bar.Low, Close: bar.Close,
					Volume: bar.Volume, CloseTime: bar.CloseTime, QuoteVolume: bar.QuoteVolume,
					Trades: bar.Trades, TakerBuyBaseVolume: bar.TakerBuyBaseVolume,
					TakerBuyQuoteVolume: bar.TakerBuyQuoteVolume,
				}
				rowsByKey[fmt.Sprintf("%s|%s|%s|%d", source, symbol, timeframe, bar.OpenTime)] = row
			}
		}
	}
	if len(rowsByKey) == 0 {
		return nil
	}
	rows := make([]CalibrationKline, 0, len(rowsByKey))
	for _, row := range rowsByKey {
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Source != rows[j].Source {
			return rows[i].Source < rows[j].Source
		}
		if rows[i].Symbol != rows[j].Symbol {
			return rows[i].Symbol < rows[j].Symbol
		}
		if rows[i].Timeframe != rows[j].Timeframe {
			return rows[i].Timeframe < rows[j].Timeframe
		}
		return rows[i].OpenTime < rows[j].OpenTime
	})
	rows, err := missingCalibrationKlineRows(tx, rows)
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		return nil
	}
	return tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "source"}, {Name: "symbol"}, {Name: "timeframe"}, {Name: "open_time"}},
		DoNothing: true,
	}).CreateInBatches(rows, 200).Error
}

func missingCalibrationKlineRows(tx *gorm.DB, rows []CalibrationKline) ([]CalibrationKline, error) {
	type group struct {
		Source    string
		Symbol    string
		Timeframe string
		OpenTimes []int64
	}
	groups := map[string]*group{}
	for _, row := range rows {
		key := row.Source + "|" + row.Symbol + "|" + row.Timeframe
		current := groups[key]
		if current == nil {
			current = &group{Source: row.Source, Symbol: row.Symbol, Timeframe: row.Timeframe}
			groups[key] = current
		}
		current.OpenTimes = append(current.OpenTimes, row.OpenTime)
	}
	existing := map[string]bool{}
	for key, current := range groups {
		var openTimes []int64
		if err := tx.Model(&CalibrationKline{}).
			Where("source = ? AND symbol = ? AND timeframe = ? AND open_time IN ?", current.Source, current.Symbol, current.Timeframe, current.OpenTimes).
			Pluck("open_time", &openTimes).Error; err != nil {
			return nil, fmt.Errorf("load existing calibration klines for %s: %w", key, err)
		}
		for _, openTime := range openTimes {
			existing[fmt.Sprintf("%s|%d", key, openTime)] = true
		}
	}
	missing := make([]CalibrationKline, 0, len(rows))
	for _, row := range rows {
		key := fmt.Sprintf("%s|%s|%s|%d", row.Source, row.Symbol, row.Timeframe, row.OpenTime)
		if !existing[key] {
			missing = append(missing, row)
		}
	}
	return missing, nil
}

// ReplayKlineWindows reconstructs the exact closed-bar window available at a
// setup sample's primary close time.
func (s *SignalCalibrationStore) ReplayKlineWindows(source, symbol string, cutoffCloseTime int64, timeframes []string, lookback int) (map[string][]market.Kline, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("signal calibration store is unavailable")
	}
	source = strings.ToLower(strings.TrimSpace(source))
	if source == "" {
		source = "default"
	}
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	if symbol == "" || cutoffCloseTime <= 0 {
		return nil, fmt.Errorf("symbol and cutoff close time are required")
	}
	if lookback <= 0 || lookback > 5000 {
		lookback = 500
	}
	result := map[string][]market.Kline{}
	seen := map[string]bool{}
	for _, timeframe := range timeframes {
		timeframe = strings.TrimSpace(timeframe)
		if timeframe == "" || seen[timeframe] {
			continue
		}
		seen[timeframe] = true
		var rows []CalibrationKline
		if err := s.db.Where("source = ? AND symbol = ? AND timeframe = ? AND close_time <= ?", source, symbol, timeframe, cutoffCloseTime).
			Order("open_time DESC").Limit(lookback).Find(&rows).Error; err != nil {
			return nil, fmt.Errorf("load replay klines for %s %s: %w", symbol, timeframe, err)
		}
		if len(rows) == 0 {
			continue
		}
		bars := make([]market.Kline, len(rows))
		for i, row := range rows {
			bars[len(rows)-1-i] = market.Kline{
				OpenTime: row.OpenTime, Open: row.Open, High: row.High, Low: row.Low,
				Close: row.Close, Volume: row.Volume, CloseTime: row.CloseTime,
				QuoteVolume: row.QuoteVolume, Trades: row.Trades,
				TakerBuyBaseVolume:  row.TakerBuyBaseVolume,
				TakerBuyQuoteVolume: row.TakerBuyQuoteVolume,
			}
		}
		result[timeframe] = bars
	}
	return result, nil
}
