package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"time"

	"nofx/market"
	quantdata "nofx/quant/data"
)

func main() {
	var (
		symbolsFlag     = flag.String("symbols", "BTCUSDT,ETHUSDT", "comma-separated symbols to export")
		timeframesFlag  = flag.String("timeframes", "5m,15m,1h", "comma-separated timeframes to export")
		startFlag       = flag.String("start", "", "UTC start time, RFC3339 or YYYY-MM-DD; defaults to 180 days before end")
		endFlag         = flag.String("end", "", "UTC end time, RFC3339 or YYYY-MM-DD; defaults to now")
		outDir          = flag.String("out", "data/quant/phase0", "output directory")
		exchange        = flag.String("exchange", "binance-futures", "data source label")
		makerFeeRate    = flag.Float64("maker-fee", 0.0002, "maker fee rate assumption")
		takerFeeRate    = flag.Float64("taker-fee", 0.0005, "taker fee rate assumption")
		jumpThreshold   = flag.Float64("jump-threshold", quantdata.DefaultAbnormalJumpPct, "close-to-close abnormal jump threshold as decimal")
		maxIssues       = flag.Int("max-issues", 200, "maximum issues stored per series")
		skipFundingFlag = flag.Bool("skip-funding", false, "skip funding export and mark funding as unavailable")
	)
	flag.Parse()

	end, err := parseTimeFlag(*endFlag, time.Now().UTC())
	if err != nil {
		log.Fatalf("parse -end: %v", err)
	}
	startDefault := end.AddDate(0, 0, -180)
	start, err := parseTimeFlag(*startFlag, startDefault)
	if err != nil {
		log.Fatalf("parse -start: %v", err)
	}
	if !end.After(start) {
		log.Fatalf("end must be after start")
	}

	symbols := splitCSV(*symbolsFlag)
	timeframes := splitCSV(*timeframesFlag)
	if len(symbols) == 0 || len(timeframes) == 0 {
		log.Fatalf("symbols and timeframes are required")
	}

	ctx := context.Background()
	exported := make(map[string]string)
	var klineReports []quantdata.KlineQualityReport
	var fundingReports []quantdata.FundingQualityReport
	fees := make([]quantdata.FeeSchedule, 0, len(symbols))

	for _, symbol := range symbols {
		normSymbol := market.Normalize(symbol)
		fees = append(fees, quantdata.FeeSchedule{
			Exchange:      *exchange,
			Symbol:        normSymbol,
			MakerFeeRate:  *makerFeeRate,
			TakerFeeRate:  *takerFeeRate,
			Source:        "static_phase0_config",
			EffectiveTime: start,
		})

		for _, timeframe := range timeframes {
			log.Printf("exporting klines symbol=%s timeframe=%s start=%s end=%s", normSymbol, timeframe, start.Format(time.RFC3339), end.Format(time.RFC3339))
			klines, err := market.GetKlinesRange(normSymbol, timeframe, start, end)
			if err != nil {
				log.Fatalf("export klines %s %s: %v", normSymbol, timeframe, err)
			}

			klinePath := filepath.Join(*outDir, "klines", fmt.Sprintf("%s_%s.jsonl", normSymbol, timeframe))
			if err := quantdata.WriteKlinesJSONL(klinePath, klines); err != nil {
				log.Fatalf("write klines %s %s: %v", normSymbol, timeframe, err)
			}
			exported[fmt.Sprintf("klines.%s.%s", normSymbol, timeframe)] = klinePath

			report := quantdata.ValidateKlines(normSymbol, timeframe, klines, quantdata.KlineValidationOptions{
				StartTime:          start,
				EndTime:            end,
				AbnormalJumpPct:    *jumpThreshold,
				MaxIssuesPerSeries: *maxIssues,
			})
			klineReports = append(klineReports, report)
		}

		if *skipFundingFlag {
			fundingReports = append(fundingReports, quantdata.FundingQualityReport{
				Symbol:            normSymbol,
				CoverageStartTime: start,
				CoverageEndTime:   end,
				ExpectedInterval:  quantdata.DefaultFundingInterval.String(),
				Issues: []quantdata.QualityIssue{{
					Severity: quantdata.SeverityWarning,
					Code:     "funding_export_skipped",
					Message:  "funding export was skipped by flag; backtest report must disclose incomplete funding data",
					Symbol:   normSymbol,
				}},
				OK: true,
			})
			continue
		}

		log.Printf("exporting funding symbol=%s start=%s end=%s", normSymbol, start.Format(time.RFC3339), end.Format(time.RFC3339))
		rates, err := quantdata.BinanceFundingClient{}.FetchRange(ctx, normSymbol, start, end)
		if err != nil {
			log.Fatalf("export funding %s: %v", normSymbol, err)
		}
		fundingPath := filepath.Join(*outDir, "funding", fmt.Sprintf("%s.jsonl", normSymbol))
		if err := quantdata.WriteFundingJSONL(fundingPath, rates); err != nil {
			log.Fatalf("write funding %s: %v", normSymbol, err)
		}
		exported[fmt.Sprintf("funding.%s", normSymbol)] = fundingPath

		fundingReports = append(fundingReports, quantdata.ValidateFundingCoverage(normSymbol, rates, start, end, quantdata.DefaultFundingInterval, *maxIssues))
	}

	feePath := filepath.Join(*outDir, "config", "fee_schedule.json")
	if err := quantdata.WriteJSON(feePath, fees); err != nil {
		log.Fatalf("write fee schedule: %v", err)
	}
	exported["fee_schedule"] = feePath

	reportPath := filepath.Join(*outDir, "reports", "quality_report.json")
	exported["quality_report"] = reportPath

	report := quantdata.BuildQualityReport(*exchange, start, end, klineReports, fundingReports, fees, exported)
	if err := quantdata.WriteJSON(reportPath, report); err != nil {
		log.Fatalf("write quality report: %v", err)
	}

	if report.OK {
		log.Printf("phase0 export completed: %s", reportPath)
		return
	}
	log.Fatalf("phase0 export completed with data-quality failures: %s", reportPath)
}

func splitCSV(value string) []string {
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			result = append(result, part)
		}
	}
	return result
}

func parseTimeFlag(value string, fallback time.Time) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback.UTC(), nil
	}
	if ts, err := time.Parse(time.RFC3339, value); err == nil {
		return ts.UTC(), nil
	}
	if date, err := time.Parse("2006-01-02", value); err == nil {
		return date.UTC(), nil
	}
	return time.Time{}, fmt.Errorf("unsupported time %q; use RFC3339 or YYYY-MM-DD", value)
}
