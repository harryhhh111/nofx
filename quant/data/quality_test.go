package data

import (
	"testing"
	"time"

	"nofx/market"
)

func TestValidateKlinesOK(t *testing.T) {
	start := time.Date(2026, 6, 14, 0, 0, 0, 0, time.UTC)
	klines := []market.Kline{
		bar(start, 100, 102, 99, 101),
		bar(start.Add(5*time.Minute), 101, 103, 100, 102),
		bar(start.Add(10*time.Minute), 102, 104, 101, 103),
	}

	report := ValidateKlines("btcusdt", "5m", klines, KlineValidationOptions{
		StartTime: start,
		EndTime:   start.Add(15 * time.Minute),
	})

	if !report.OK {
		t.Fatalf("expected report OK, got issues: %+v", report.Issues)
	}
	if report.MissingBars != 0 || report.InvalidOHLCBars != 0 || report.NonIncreasingBars != 0 {
		t.Fatalf("unexpected counters: %+v", report)
	}
}

func TestValidateKlinesDetectsGapInvalidOHLCAndJump(t *testing.T) {
	start := time.Date(2026, 6, 14, 0, 0, 0, 0, time.UTC)
	klines := []market.Kline{
		bar(start, 100, 102, 99, 101),
		bar(start.Add(10*time.Minute), 160, 120, 90, 130),
	}

	report := ValidateKlines("BTC", "5m", klines, KlineValidationOptions{
		StartTime:       start,
		EndTime:         start.Add(15 * time.Minute),
		AbnormalJumpPct: 0.10,
	})

	if report.OK {
		t.Fatalf("expected report to fail")
	}
	if report.MissingBars != 1 {
		t.Fatalf("expected one missing bar, got %d", report.MissingBars)
	}
	if report.InvalidOHLCBars != 1 {
		t.Fatalf("expected one invalid OHLC bar, got %d", report.InvalidOHLCBars)
	}
	if report.AbnormalJumps != 1 {
		t.Fatalf("expected one abnormal jump, got %d", report.AbnormalJumps)
	}
	if !hasCode(report.Issues, "kline_gap") || !hasCode(report.Issues, "invalid_ohlc") || !hasCode(report.Issues, "abnormal_price_jump") {
		t.Fatalf("expected gap, OHLC, and jump issues, got %+v", report.Issues)
	}
}

func TestValidateKlinesDetectsNonIncreasingTime(t *testing.T) {
	start := time.Date(2026, 6, 14, 0, 0, 0, 0, time.UTC)
	klines := []market.Kline{
		bar(start, 100, 102, 99, 101),
		bar(start, 101, 103, 100, 102),
	}

	report := ValidateKlines("BTCUSDT", "5m", klines, KlineValidationOptions{})

	if report.OK {
		t.Fatalf("expected report to fail")
	}
	if report.NonIncreasingBars != 1 {
		t.Fatalf("expected one non-increasing bar, got %d", report.NonIncreasingBars)
	}
	if !hasCode(report.Issues, "non_increasing_time") {
		t.Fatalf("expected non_increasing_time issue, got %+v", report.Issues)
	}
}

func TestValidateFundingCoverage(t *testing.T) {
	start := time.Date(2026, 6, 14, 0, 0, 0, 0, time.UTC)
	rates := []FundingRateRecord{
		funding("BTCUSDT", start, 0.0001),
		funding("BTCUSDT", start.Add(8*time.Hour), -0.0001),
		funding("BTCUSDT", start.Add(16*time.Hour), 0.0002),
	}

	report := ValidateFundingCoverage("BTCUSDT", rates, start, start.Add(24*time.Hour), 8*time.Hour, 0)

	if !report.OK {
		t.Fatalf("expected funding report OK, got issues: %+v", report.Issues)
	}
}

func TestValidateFundingCoverageDetectsGap(t *testing.T) {
	start := time.Date(2026, 6, 14, 0, 0, 0, 0, time.UTC)
	rates := []FundingRateRecord{
		funding("ETHUSDT", start, 0.0001),
		funding("ETHUSDT", start.Add(16*time.Hour), 0.0002),
	}

	report := ValidateFundingCoverage("ETHUSDT", rates, start, start.Add(24*time.Hour), 8*time.Hour, 0)

	if report.OK {
		t.Fatalf("expected funding report to fail")
	}
	if report.MissingIntervals != 1 {
		t.Fatalf("expected one missing interval, got %d", report.MissingIntervals)
	}
	if !hasCode(report.Issues, "funding_gap") {
		t.Fatalf("expected funding_gap issue, got %+v", report.Issues)
	}
}

func bar(openTime time.Time, open, high, low, close float64) market.Kline {
	return market.Kline{
		OpenTime:  openTime.UnixMilli(),
		Open:      open,
		High:      high,
		Low:       low,
		Close:     close,
		Volume:    1,
		CloseTime: openTime.Add(5*time.Minute - time.Millisecond).UnixMilli(),
	}
}

func funding(symbol string, ts time.Time, rate float64) FundingRateRecord {
	return FundingRateRecord{
		Symbol:            symbol,
		FundingTime:       ts,
		FundingTimeUnixMs: ts.UnixMilli(),
		FundingRate:       rate,
	}
}

func hasCode(issues []QualityIssue, code string) bool {
	for _, item := range issues {
		if item.Code == code {
			return true
		}
	}
	return false
}
