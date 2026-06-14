package data

import (
	"fmt"
	"math"
	"time"

	"nofx/market"
)

type KlineValidationOptions struct {
	StartTime          time.Time
	EndTime            time.Time
	AbnormalJumpPct    float64
	MaxIssuesPerSeries int
}

func ValidateKlines(symbol, timeframe string, klines []market.Kline, opts KlineValidationOptions) KlineQualityReport {
	normSymbol := market.Normalize(symbol)
	normTF, err := market.NormalizeTimeframe(timeframe)
	report := KlineQualityReport{
		Symbol:    normSymbol,
		Timeframe: timeframe,
		BarCount:  len(klines),
		OK:        true,
	}
	if err != nil {
		addKlineIssue(&report, issue(SeverityError, "unsupported_timeframe", err.Error(), normSymbol, timeframe, time.Time{}, 0), opts.MaxIssuesPerSeries)
		report.OK = false
		return report
	}
	report.Timeframe = normTF

	interval, err := market.TFDuration(normTF)
	if err != nil {
		addKlineIssue(&report, issue(SeverityError, "unsupported_timeframe", err.Error(), normSymbol, normTF, time.Time{}, 0), opts.MaxIssuesPerSeries)
		report.OK = false
		return report
	}
	intervalMs := interval.Milliseconds()
	report.ExpectedIntervalMs = intervalMs

	if opts.AbnormalJumpPct <= 0 {
		opts.AbnormalJumpPct = DefaultAbnormalJumpPct
	}

	if len(klines) == 0 {
		addKlineIssue(&report, issue(SeverityError, "empty_kline_series", "kline series is empty", normSymbol, normTF, time.Time{}, 0), opts.MaxIssuesPerSeries)
		report.OK = false
		return report
	}

	report.FirstOpenTime = unixMs(klines[0].OpenTime)
	report.LastOpenTime = unixMs(klines[len(klines)-1].OpenTime)
	report.ExpectedBarCount = expectedBarCount(opts.StartTime, opts.EndTime, interval)

	if !opts.StartTime.IsZero() && report.FirstOpenTime.After(opts.StartTime.Add(interval)) {
		addKlineIssue(&report, issue(SeverityError, "start_coverage_gap", fmt.Sprintf("first bar %s starts after requested start %s", report.FirstOpenTime.Format(time.RFC3339), opts.StartTime.Format(time.RFC3339)), normSymbol, normTF, report.FirstOpenTime, 0), opts.MaxIssuesPerSeries)
	}
	if !opts.EndTime.IsZero() && report.LastOpenTime.Before(opts.EndTime.Add(-interval)) {
		addKlineIssue(&report, issue(SeverityError, "end_coverage_gap", fmt.Sprintf("last bar %s ends before requested end %s", report.LastOpenTime.Format(time.RFC3339), opts.EndTime.Format(time.RFC3339)), normSymbol, normTF, report.LastOpenTime, len(klines)-1), opts.MaxIssuesPerSeries)
	}

	for i, bar := range klines {
		openTime := unixMs(bar.OpenTime)
		if !validOHLC(bar) {
			report.InvalidOHLCBars++
			addKlineIssue(&report, issue(SeverityError, "invalid_ohlc", fmt.Sprintf("invalid OHLC at %s: open=%f high=%f low=%f close=%f", openTime.Format(time.RFC3339), bar.Open, bar.High, bar.Low, bar.Close), normSymbol, normTF, openTime, i), opts.MaxIssuesPerSeries)
		}
		if bar.Volume < 0 || math.IsNaN(bar.Volume) || math.IsInf(bar.Volume, 0) {
			addKlineIssue(&report, issue(SeverityError, "invalid_volume", fmt.Sprintf("invalid volume at %s: volume=%f", openTime.Format(time.RFC3339), bar.Volume), normSymbol, normTF, openTime, i), opts.MaxIssuesPerSeries)
		}

		if i == 0 {
			continue
		}

		prev := klines[i-1]
		delta := bar.OpenTime - prev.OpenTime
		if delta <= 0 {
			report.NonIncreasingBars++
			addKlineIssue(&report, issue(SeverityError, "non_increasing_time", fmt.Sprintf("bar open time %d is not after previous open time %d", bar.OpenTime, prev.OpenTime), normSymbol, normTF, openTime, i), opts.MaxIssuesPerSeries)
		} else if delta != intervalMs {
			if delta > intervalMs {
				missing := int(delta/intervalMs) - 1
				if missing < 1 {
					missing = 1
				}
				report.MissingBars += missing
				addKlineIssue(&report, issue(SeverityError, "kline_gap", fmt.Sprintf("expected %s interval, got %s between %s and %s", interval, time.Duration(delta)*time.Millisecond, unixMs(prev.OpenTime).Format(time.RFC3339), openTime.Format(time.RFC3339)), normSymbol, normTF, openTime, i), opts.MaxIssuesPerSeries)
			} else {
				addKlineIssue(&report, issue(SeverityError, "unexpected_interval", fmt.Sprintf("expected %s interval, got %s", interval, time.Duration(delta)*time.Millisecond), normSymbol, normTF, openTime, i), opts.MaxIssuesPerSeries)
			}
		}

		if prev.Close > 0 {
			jump := math.Abs(bar.Close-prev.Close) / prev.Close
			if jump > opts.AbnormalJumpPct {
				report.AbnormalJumps++
				addKlineIssue(&report, issue(SeverityWarning, "abnormal_price_jump", fmt.Sprintf("close-to-close jump %.2f%% exceeds %.2f%%", jump*100, opts.AbnormalJumpPct*100), normSymbol, normTF, openTime, i), opts.MaxIssuesPerSeries)
			}
		}
	}

	report.OK = !hasError(report.Issues)
	return report
}

func ValidateFundingCoverage(symbol string, rates []FundingRateRecord, start, end time.Time, interval time.Duration, maxIssues int) FundingQualityReport {
	normSymbol := market.Normalize(symbol)
	if interval <= 0 {
		interval = DefaultFundingInterval
	}
	report := FundingQualityReport{
		Symbol:            normSymbol,
		RecordCount:       len(rates),
		ExpectedInterval:  interval.String(),
		CoverageStartTime: start,
		CoverageEndTime:   end,
		OK:                true,
	}

	if len(rates) == 0 {
		addFundingIssue(&report, issue(SeverityError, "empty_funding_series", "funding rate series is empty", normSymbol, "", time.Time{}, 0), maxIssues)
		report.OK = false
		return report
	}

	report.FirstFundingTime = rates[0].FundingTime
	report.LastFundingTime = rates[len(rates)-1].FundingTime

	if !start.IsZero() && report.FirstFundingTime.After(start.Add(interval)) {
		addFundingIssue(&report, issue(SeverityError, "funding_start_gap", fmt.Sprintf("first funding timestamp %s starts too far after requested start %s", report.FirstFundingTime.Format(time.RFC3339), start.Format(time.RFC3339)), normSymbol, "", report.FirstFundingTime, 0), maxIssues)
	}
	if !end.IsZero() && report.LastFundingTime.Before(end.Add(-interval)) {
		addFundingIssue(&report, issue(SeverityError, "funding_end_gap", fmt.Sprintf("last funding timestamp %s ends too far before requested end %s", report.LastFundingTime.Format(time.RFC3339), end.Format(time.RFC3339)), normSymbol, "", report.LastFundingTime, len(rates)-1), maxIssues)
	}

	for i := 1; i < len(rates); i++ {
		prev := rates[i-1]
		cur := rates[i]
		delta := cur.FundingTime.Sub(prev.FundingTime)
		if !cur.FundingTime.After(prev.FundingTime) {
			addFundingIssue(&report, issue(SeverityError, "non_increasing_funding_time", fmt.Sprintf("funding timestamp %s is not after previous timestamp %s", cur.FundingTime.Format(time.RFC3339), prev.FundingTime.Format(time.RFC3339)), normSymbol, "", cur.FundingTime, i), maxIssues)
			continue
		}
		if delta > interval+time.Minute {
			missing := int(delta/interval) - 1
			if missing < 1 {
				missing = 1
			}
			report.MissingIntervals += missing
			addFundingIssue(&report, issue(SeverityError, "funding_gap", fmt.Sprintf("expected funding interval near %s, got %s", interval, delta), normSymbol, "", cur.FundingTime, i), maxIssues)
		}
	}

	report.OK = !hasError(report.Issues)
	return report
}

func BuildQualityReport(exchange string, start, end time.Time, klineReports []KlineQualityReport, fundingReports []FundingQualityReport, fees []FeeSchedule, exported map[string]string) QualityReport {
	report := QualityReport{
		GeneratedAt:   time.Now().UTC(),
		Exchange:      exchange,
		StartTime:     start,
		EndTime:       end,
		Klines:        klineReports,
		Funding:       fundingReports,
		FeeSchedules:  fees,
		ExportedFiles: exported,
		OK:            true,
	}
	report.Summary.KlineSeriesChecked = len(klineReports)
	report.Summary.FundingSeriesChecked = len(fundingReports)

	for _, r := range klineReports {
		countIssues(&report.Summary, r.Issues)
		if !r.OK {
			report.OK = false
		}
	}
	for _, r := range fundingReports {
		countIssues(&report.Summary, r.Issues)
		if !r.OK {
			report.OK = false
		}
	}

	return report
}

func validOHLC(bar market.Kline) bool {
	values := []float64{bar.Open, bar.High, bar.Low, bar.Close}
	for _, v := range values {
		if v <= 0 || math.IsNaN(v) || math.IsInf(v, 0) {
			return false
		}
	}
	return bar.Low <= bar.Open && bar.Open <= bar.High &&
		bar.Low <= bar.Close && bar.Close <= bar.High &&
		bar.Low <= bar.High
}

func expectedBarCount(start, end time.Time, interval time.Duration) int {
	if start.IsZero() || end.IsZero() || !end.After(start) || interval <= 0 {
		return 0
	}
	return int(end.Sub(start) / interval)
}

func issue(severity QualitySeverity, code, message, symbol, timeframe string, ts time.Time, idx int) QualityIssue {
	return QualityIssue{
		Severity:  severity,
		Code:      code,
		Message:   message,
		Symbol:    symbol,
		Timeframe: timeframe,
		Time:      ts,
		Index:     idx,
	}
}

func addKlineIssue(report *KlineQualityReport, item QualityIssue, max int) {
	if max > 0 && len(report.Issues) >= max {
		return
	}
	report.Issues = append(report.Issues, item)
}

func addFundingIssue(report *FundingQualityReport, item QualityIssue, max int) {
	if max > 0 && len(report.Issues) >= max {
		return
	}
	report.Issues = append(report.Issues, item)
}

func hasError(issues []QualityIssue) bool {
	for _, item := range issues {
		if item.Severity == SeverityError {
			return true
		}
	}
	return false
}

func countIssues(summary *QualitySummary, issues []QualityIssue) {
	for _, item := range issues {
		switch item.Severity {
		case SeverityError:
			summary.ErrorCount++
		case SeverityWarning:
			summary.WarningCount++
		default:
			summary.InfoCount++
		}
	}
}

func unixMs(ms int64) time.Time {
	if ms <= 0 {
		return time.Time{}
	}
	return time.UnixMilli(ms).UTC()
}
