package api

import (
	"net/http"
	"nofx/kernel"
	"nofx/market"
	"nofx/store"
	"strconv"

	"github.com/gin-gonic/gin"
)

func (s *Server) handleStrategyReplayReport(c *gin.Context) {
	userID := c.GetString("user_id")
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}
	strategyID := c.Param("id")
	strategy, err := s.store.Strategy().Get(userID, strategyID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Strategy not found"})
		return
	}
	config, err := strategy.ParseConfig()
	if err != nil {
		SafeInternalError(c, "Failed to parse strategy config", err)
		return
	}
	limit := 500
	if raw := c.Query("limit"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			limit = parsed
		}
	}
	strategyVersion := requestedStrategyVersion(c, config)
	samples, err := s.store.SignalCalibration().RecentSamplesForVersion(strategyID, strategyVersion, limit)
	if err != nil {
		SafeInternalError(c, "Load strategy replay samples", err)
		return
	}
	report, err := kernel.BuildStrategyReplayReport(kernel.StrategyReplayRequest{
		StrategyID:      strategyID,
		StrategyVersion: strategyVersion,
		CurrentConfig:   config,
		Samples:         samples,
		KlineLoader: func(sample store.SignalCalibrationSample) (map[string][]market.Kline, error) {
			return s.store.SignalCalibration().ReplayKlineWindows(
				replaySampleSource(sample, config),
				sample.Symbol,
				replaySampleCutoff(sample),
				replayTimeframes(config),
				config.Indicators.Klines.ComputeLookback,
			)
		},
		Limit: limit,
	})
	if err != nil {
		SafeInternalError(c, "Build strategy replay report", err)
		return
	}
	c.JSON(http.StatusOK, report)
}

func replaySampleCutoff(sample store.SignalCalibrationSample) int64 {
	if sample.PrimaryBarTime > 0 {
		return sample.PrimaryBarTime
	}
	return sample.AsOf.UnixMilli()
}

func replaySampleSource(sample store.SignalCalibrationSample, config *store.StrategyConfig) string {
	if sample.MarketDataSource != "" {
		return sample.MarketDataSource
	}
	if config == nil {
		return "default"
	}
	return config.Indicators.Klines.MarketDataSource
}

func replayTimeframes(config *store.StrategyConfig) []string {
	if config == nil {
		return nil
	}
	values := append([]string(nil), config.Indicators.Klines.SelectedTimeframes...)
	values = append(values,
		config.Indicators.Klines.PrimaryTimeframe,
		config.Indicators.Klines.EntryTimeframe,
	)
	values = append(values, config.Indicators.Klines.ConfirmationTimeframes...)
	return values
}
