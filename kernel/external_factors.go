package kernel

import (
	"fmt"
	"nofx/market"
	"nofx/provider/nofxos"
	"strings"
	"time"
)

func EnrichExternalFactors(ctx *Context, snapshots map[string]*market.FactorSnapshot, asOf time.Time) {
	if ctx == nil || len(snapshots) == 0 {
		return
	}
	if asOf.IsZero() {
		asOf = time.Now().UTC()
	}
	enrichQuantFactors(ctx.QuantDataMap, snapshots, asOf)
	enrichOITopFactors(ctx.OITopDataMap, snapshots, asOf)
	enrichOIRankingFactors(ctx.OIRankingData, snapshots, asOf)
	enrichNetFlowRankingFactors(ctx.NetFlowRankingData, snapshots, asOf)
	enrichPriceRankingFactors(ctx.PriceRankingData, snapshots, asOf)
}

func enrichQuantFactors(data map[string]*QuantData, snapshots map[string]*market.FactorSnapshot, asOf time.Time) {
	for symbol, quant := range data {
		snapshot := snapshots[market.Normalize(symbol)]
		if snapshot == nil || quant == nil {
			continue
		}
		ensureExternal(snapshot)
		for duration, change := range quant.PriceChange {
			snapshot.External["quant_price_change_"+duration] = externalFactor("quant_price_change_"+duration, "nofxos_quant", duration, change, signedState(change), change*100, asOf, asOf, "paid", nil)
		}
		for exchange, oi := range quant.OI {
			if oi == nil {
				continue
			}
			name := "quant_oi_" + strings.ToLower(exchange)
			snapshot.External[name] = externalFactor(name, "nofxos_quant", "", oi.CurrentOI, availableState(oi.CurrentOI), 0, asOf, asOf, "paid", nil)
			for duration, delta := range oi.Delta {
				if delta == nil {
					continue
				}
				deltaName := fmt.Sprintf("quant_oi_delta_%s_%s", strings.ToLower(exchange), duration)
				snapshot.External[deltaName] = externalFactor(deltaName, "nofxos_quant", duration, delta.OIDeltaPercent, signedState(delta.OIDeltaPercent), delta.OIDeltaPercent, asOf, asOf, "paid", map[string]interface{}{
					"oi_delta":       delta.OIDelta,
					"oi_delta_value": delta.OIDeltaValue,
				})
			}
		}
		if quant.Netflow != nil {
			addFlowFactors(snapshot, "institution", quant.Netflow.Institution, asOf)
			addFlowFactors(snapshot, "personal", quant.Netflow.Personal, asOf)
		}
	}
}

func addFlowFactors(snapshot *market.FactorSnapshot, owner string, flow *FlowTypeData, asOf time.Time) {
	if flow == nil {
		return
	}
	for duration, value := range flow.Future {
		name := fmt.Sprintf("quant_netflow_%s_future_%s", owner, duration)
		snapshot.External[name] = externalFactor(name, "nofxos_quant", duration, value, signedState(value), flowScore(value), asOf, asOf, "paid", nil)
	}
	for duration, value := range flow.Spot {
		name := fmt.Sprintf("quant_netflow_%s_spot_%s", owner, duration)
		snapshot.External[name] = externalFactor(name, "nofxos_quant", duration, value, signedState(value), flowScore(value), asOf, asOf, "paid", nil)
	}
}

func enrichOITopFactors(data map[string]*OITopData, snapshots map[string]*market.FactorSnapshot, asOf time.Time) {
	for symbol, oi := range data {
		snapshot := snapshots[market.Normalize(symbol)]
		if snapshot == nil || oi == nil {
			continue
		}
		ensureExternal(snapshot)
		snapshot.External["oi_top_candidate"] = externalFactor("oi_top_candidate", "nofxos_oi_top", "1h", float64(oi.Rank), "top", oi.OIDeltaPercent, asOf, asOf, "paid", map[string]interface{}{
			"oi_delta_percent":    oi.OIDeltaPercent,
			"oi_delta_value":      oi.OIDeltaValue,
			"price_delta_percent": oi.PriceDeltaPercent,
		})
	}
}

func enrichOIRankingFactors(data *nofxos.OIRankingData, snapshots map[string]*market.FactorSnapshot, asOf time.Time) {
	if data == nil {
		return
	}
	sourceTime := nonZeroTime(data.FetchedAt, asOf)
	for _, pos := range data.TopPositions {
		addOIRankingPosition(pos, "oi_ranking_top", "top", data.Duration, sourceTime, asOf, snapshots)
	}
	for _, pos := range data.LowPositions {
		addOIRankingPosition(pos, "oi_ranking_low", "low", data.Duration, sourceTime, asOf, snapshots)
	}
}

func addOIRankingPosition(pos nofxos.OIPosition, name, state, timeframe string, sourceTime, asOf time.Time, snapshots map[string]*market.FactorSnapshot) {
	snapshot := snapshots[market.Normalize(pos.Symbol)]
	if snapshot == nil {
		return
	}
	ensureExternal(snapshot)
	snapshot.External[name] = externalFactor(name, "nofxos_oi_ranking", timeframe, float64(pos.Rank), state, pos.OIDeltaPercent, sourceTime, asOf, "paid", map[string]interface{}{
		"price":               pos.Price,
		"current_oi":          pos.CurrentOI,
		"oi_delta":            pos.OIDelta,
		"oi_delta_percent":    pos.OIDeltaPercent,
		"oi_delta_value":      pos.OIDeltaValue,
		"price_delta_percent": pos.PriceDeltaPercent,
		"net_long":            pos.NetLong,
		"net_short":           pos.NetShort,
	})
}

func enrichNetFlowRankingFactors(data *nofxos.NetFlowRankingData, snapshots map[string]*market.FactorSnapshot, asOf time.Time) {
	if data == nil {
		return
	}
	sourceTime := nonZeroTime(data.FetchedAt, asOf)
	addNetFlowPositions(data.InstitutionFutureTop, "netflow_institution_future_top", "institution_inflow", data.Duration, sourceTime, asOf, snapshots)
	addNetFlowPositions(data.InstitutionFutureLow, "netflow_institution_future_low", "institution_outflow", data.Duration, sourceTime, asOf, snapshots)
	addNetFlowPositions(data.PersonalFutureTop, "netflow_personal_future_top", "personal_inflow", data.Duration, sourceTime, asOf, snapshots)
	addNetFlowPositions(data.PersonalFutureLow, "netflow_personal_future_low", "personal_outflow", data.Duration, sourceTime, asOf, snapshots)
}

func addNetFlowPositions(positions []nofxos.NetFlowPosition, name, state, timeframe string, sourceTime, asOf time.Time, snapshots map[string]*market.FactorSnapshot) {
	for _, pos := range positions {
		snapshot := snapshots[market.Normalize(pos.Symbol)]
		if snapshot == nil {
			continue
		}
		ensureExternal(snapshot)
		snapshot.External[name] = externalFactor(name, "nofxos_netflow_ranking", timeframe, float64(pos.Rank), state, flowScore(pos.Amount), sourceTime, asOf, "paid", map[string]interface{}{
			"amount": pos.Amount,
			"price":  pos.Price,
		})
	}
}

func enrichPriceRankingFactors(data *nofxos.PriceRankingData, snapshots map[string]*market.FactorSnapshot, asOf time.Time) {
	if data == nil {
		return
	}
	sourceTime := nonZeroTime(data.FetchedAt, asOf)
	for duration, ranking := range data.Durations {
		if ranking == nil {
			continue
		}
		addPriceRankingItems(ranking.Top, "price_ranking_top_"+duration, "top_gainer", duration, sourceTime, asOf, snapshots)
		addPriceRankingItems(ranking.Low, "price_ranking_low_"+duration, "top_loser", duration, sourceTime, asOf, snapshots)
	}
}

func addPriceRankingItems(items []nofxos.PriceRankingItem, name, state, timeframe string, sourceTime, asOf time.Time, snapshots map[string]*market.FactorSnapshot) {
	for rank, item := range items {
		snapshot := snapshots[market.Normalize(item.Symbol)]
		if snapshot == nil {
			continue
		}
		ensureExternal(snapshot)
		snapshot.External[name] = externalFactor(name, "nofxos_price_ranking", timeframe, float64(rank+1), state, item.PriceDelta*100, sourceTime, asOf, "paid", map[string]interface{}{
			"pair":           item.Pair,
			"price":          item.Price,
			"price_delta":    item.PriceDelta,
			"future_flow":    item.FutureFlow,
			"spot_flow":      item.SpotFlow,
			"oi":             item.OI,
			"oi_delta":       item.OIDelta,
			"oi_delta_value": item.OIDeltaValue,
		})
	}
}

func ensureExternal(snapshot *market.FactorSnapshot) {
	if snapshot.External == nil {
		snapshot.External = map[string]market.ExternalFactor{}
	}
}

func externalFactor(name, source, timeframe string, value float64, state string, score float64, sourceTime, availableAt time.Time, costClass string, metadata map[string]interface{}) market.ExternalFactor {
	return market.ExternalFactor{
		Name:        name,
		Source:      source,
		Timeframe:   timeframe,
		Value:       value,
		State:       state,
		Score:       score,
		Available:   true,
		SourceTime:  sourceTime,
		AvailableAt: availableAt,
		CostClass:   costClass,
		Metadata:    metadata,
	}
}

func signedState(value float64) string {
	switch {
	case value > 0:
		return "positive"
	case value < 0:
		return "negative"
	default:
		return "neutral"
	}
}

func availableState(value float64) string {
	if value > 0 {
		return "available"
	}
	return "empty"
}

func flowScore(value float64) float64 {
	switch {
	case value > 0:
		return 50
	case value < 0:
		return -50
	default:
		return 0
	}
}

func nonZeroTime(value time.Time, fallback time.Time) time.Time {
	if value.IsZero() {
		return fallback
	}
	return value
}
