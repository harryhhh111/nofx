package data

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"nofx/market"
)

const (
	binanceFundingRateURL = "https://fapi.binance.com/fapi/v1/fundingRate"
	binanceFundingLimit   = 1000
)

type BinanceFundingClient struct {
	HTTPClient *http.Client
}

func (c BinanceFundingClient) FetchRange(ctx context.Context, symbol string, start, end time.Time) ([]FundingRateRecord, error) {
	if !end.After(start) {
		return nil, fmt.Errorf("end time must be after start time")
	}

	client := c.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}

	normSymbol := market.Normalize(symbol)
	startMs := start.UnixMilli()
	endMs := end.UnixMilli()
	cursor := startMs

	var all []FundingRateRecord
	for cursor < endMs {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, binanceFundingRateURL, nil)
		if err != nil {
			return nil, err
		}
		q := req.URL.Query()
		q.Set("symbol", normSymbol)
		q.Set("startTime", strconv.FormatInt(cursor, 10))
		q.Set("endTime", strconv.FormatInt(endMs, 10))
		q.Set("limit", strconv.Itoa(binanceFundingLimit))
		req.URL.RawQuery = q.Encode()

		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		body, readErr := io.ReadAll(resp.Body)
		closeErr := resp.Body.Close()
		if readErr != nil {
			return nil, readErr
		}
		if closeErr != nil {
			return nil, closeErr
		}
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("binance funding api returned status %d: %s", resp.StatusCode, string(body))
		}

		var raw []struct {
			Symbol      string `json:"symbol"`
			FundingRate string `json:"fundingRate"`
			FundingTime int64  `json:"fundingTime"`
			MarkPrice   string `json:"markPrice"`
		}
		if err := json.Unmarshal(body, &raw); err != nil {
			return nil, err
		}
		if len(raw) == 0 {
			break
		}

		for _, item := range raw {
			rate, err := strconv.ParseFloat(item.FundingRate, 64)
			if err != nil {
				return nil, fmt.Errorf("parse funding rate at %d: %w", item.FundingTime, err)
			}
			markPrice := 0.0
			if item.MarkPrice != "" {
				markPrice, _ = strconv.ParseFloat(item.MarkPrice, 64)
			}
			all = append(all, FundingRateRecord{
				Symbol:            item.Symbol,
				FundingTime:       time.UnixMilli(item.FundingTime).UTC(),
				FundingTimeUnixMs: item.FundingTime,
				FundingRate:       rate,
				MarkPrice:         markPrice,
			})
		}

		last := raw[len(raw)-1].FundingTime
		nextCursor := last + 1
		if nextCursor <= cursor || len(raw) < binanceFundingLimit {
			break
		}
		cursor = nextCursor
	}

	return all, nil
}

func KlineToRecord(k market.Kline) KlineRecord {
	return KlineRecord{
		OpenTime:            time.UnixMilli(k.OpenTime).UTC(),
		OpenTimeUnixMs:      k.OpenTime,
		Open:                k.Open,
		High:                k.High,
		Low:                 k.Low,
		Close:               k.Close,
		Volume:              k.Volume,
		CloseTime:           time.UnixMilli(k.CloseTime).UTC(),
		CloseTimeUnixMs:     k.CloseTime,
		QuoteVolume:         k.QuoteVolume,
		Trades:              k.Trades,
		TakerBuyBaseVolume:  k.TakerBuyBaseVolume,
		TakerBuyQuoteVolume: k.TakerBuyQuoteVolume,
	}
}

func WriteKlinesJSONL(path string, klines []market.Kline) error {
	return writeJSONL(path, len(klines), func(enc *json.Encoder, i int) error {
		return enc.Encode(KlineToRecord(klines[i]))
	})
}

func WriteFundingJSONL(path string, rates []FundingRateRecord) error {
	return writeJSONL(path, len(rates), func(enc *json.Encoder, i int) error {
		return enc.Encode(rates[i])
	})
}

func WriteJSON(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()

	enc := json.NewEncoder(file)
	enc.SetIndent("", "  ")
	return enc.Encode(value)
}

func writeJSONL(path string, count int, encode func(*json.Encoder, int) error) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()

	writer := bufio.NewWriter(file)
	enc := json.NewEncoder(writer)
	for i := 0; i < count; i++ {
		if err := encode(enc, i); err != nil {
			return err
		}
	}
	return writer.Flush()
}
