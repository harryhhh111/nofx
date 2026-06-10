package market

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"nofx/provider/hyperliquid"
	"strconv"
	"strings"
	"time"
)

// Note: production K-line calculations use official public exchange APIs where
// supported, without authentication.

func getKlinesFromOfficialFuturesContext(ctx context.Context, symbol, interval string, limit int, exchange string) ([]Kline, error) {
	symbol = Normalize(symbol)
	interval, err := NormalizeTimeframe(interval)
	if err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 300
	}
	if limit > binanceMaxKlineLimit {
		limit = binanceMaxKlineLimit
	}

	switch strings.ToLower(strings.TrimSpace(exchange)) {
	case "", "paper", "binance":
		return getKlinesFromBinanceFuturesContext(ctx, symbol, interval, limit)
	case "bybit":
		return getKlinesFromBybitLinearContext(ctx, symbol, interval, limit)
	case "okx":
		return getKlinesFromOKXSwapContext(ctx, symbol, interval, limit)
	case "aster":
		return getKlinesFromAsterFuturesContext(ctx, symbol, interval, limit)
	case "hyperliquid":
		return getKlinesFromHyperliquidContext(ctx, symbol, interval, limit)
	default:
		return nil, fmt.Errorf("official kline source for exchange %q is not implemented", exchange)
	}
}

func getKlinesFromBinanceFuturesContext(ctx context.Context, symbol, interval string, limit int) ([]Kline, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", binanceFuturesKlinesURL, nil)
	if err != nil {
		return nil, err
	}
	q := req.URL.Query()
	q.Set("symbol", symbol)
	q.Set("interval", interval)
	q.Set("limit", strconv.Itoa(limit))
	req.URL.RawQuery = q.Encode()

	body, err := getKlineBody(req)
	if err != nil {
		return nil, err
	}
	return parseBinanceKlinePayload(body)
}

func getKlinesFromBybitLinearContext(ctx context.Context, symbol, interval string, limit int) ([]Kline, error) {
	bybitInterval, err := bybitKlineInterval(interval)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, "GET", "https://api.bybit.com/v5/market/kline", nil)
	if err != nil {
		return nil, err
	}
	q := req.URL.Query()
	q.Set("category", "linear")
	q.Set("symbol", symbol)
	q.Set("interval", bybitInterval)
	q.Set("limit", strconv.Itoa(minInt(limit, 1000)))
	req.URL.RawQuery = q.Encode()

	body, err := getKlineBody(req)
	if err != nil {
		return nil, err
	}
	return parseBybitKlinePayload(body)
}

func getKlinesFromOKXSwapContext(ctx context.Context, symbol, interval string, limit int) ([]Kline, error) {
	okxBar, err := okxKlineBar(interval)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, "GET", "https://www.okx.com/api/v5/market/candles", nil)
	if err != nil {
		return nil, err
	}
	q := req.URL.Query()
	q.Set("instId", okxSwapInstrument(symbol))
	q.Set("bar", okxBar)
	q.Set("limit", strconv.Itoa(minInt(limit, 300)))
	req.URL.RawQuery = q.Encode()

	body, err := getKlineBody(req)
	if err != nil {
		return nil, err
	}
	return parseOKXKlinePayload(body)
}

func getKlinesFromAsterFuturesContext(ctx context.Context, symbol, interval string, limit int) ([]Kline, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", "https://fapi.asterdex.com/fapi/v3/klines", nil)
	if err != nil {
		return nil, err
	}
	q := req.URL.Query()
	q.Set("symbol", symbol)
	q.Set("interval", interval)
	q.Set("limit", strconv.Itoa(minInt(limit, 1500)))
	req.URL.RawQuery = q.Encode()

	body, err := getKlineBody(req)
	if err != nil {
		return nil, err
	}
	return parseBinanceKlinePayload(body)
}

func getKlineBody(req *http.Request) ([]byte, error) {
	return doMarketRequestBody(&http.Client{Timeout: 10 * time.Second}, req)
}

func parseBinanceKlinePayload(body []byte) ([]Kline, error) {
	var raw []KlineResponse
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, err
	}
	klines := make([]Kline, 0, len(raw))
	for _, item := range raw {
		kline, err := parseKline(item)
		if err != nil {
			return nil, err
		}
		klines = append(klines, kline)
	}
	return klines, nil
}

func parseBybitKlinePayload(body []byte) ([]Kline, error) {
	var resp struct {
		RetCode int    `json:"retCode"`
		RetMsg  string `json:"retMsg"`
		Result  struct {
			List [][]string `json:"list"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	if resp.RetCode != 0 {
		return nil, fmt.Errorf("bybit returned %d: %s", resp.RetCode, resp.RetMsg)
	}
	klines := make([]Kline, 0, len(resp.Result.List))
	for _, item := range resp.Result.List {
		if len(item) < 7 {
			return nil, fmt.Errorf("invalid bybit kline row")
		}
		kline, err := stringKline(item[0], item[1], item[2], item[3], item[4], item[5], "")
		if err != nil {
			return nil, err
		}
		klines = append(klines, kline)
	}
	sortKlines(klines)
	return klines, nil
}

func parseOKXKlinePayload(body []byte) ([]Kline, error) {
	var resp struct {
		Code string     `json:"code"`
		Msg  string     `json:"msg"`
		Data [][]string `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	if resp.Code != "0" {
		return nil, fmt.Errorf("okx returned %s: %s", resp.Code, resp.Msg)
	}
	klines := make([]Kline, 0, len(resp.Data))
	for _, item := range resp.Data {
		if len(item) < 9 {
			return nil, fmt.Errorf("invalid okx kline row")
		}
		kline, err := stringKline(item[0], item[1], item[2], item[3], item[4], item[5], "")
		if err != nil {
			return nil, err
		}
		klines = append(klines, kline)
	}
	sortKlines(klines)
	return klines, nil
}

func stringKline(openTime, open, high, low, closePrice, volume, closeTime string) (Kline, error) {
	openTimeMs, err := strconv.ParseInt(openTime, 10, 64)
	if err != nil {
		return Kline{}, err
	}
	closeTimeMs := int64(0)
	if closeTime != "" {
		closeTimeMs, err = strconv.ParseInt(closeTime, 10, 64)
		if err != nil {
			return Kline{}, err
		}
	}
	openValue, err := strconv.ParseFloat(open, 64)
	if err != nil {
		return Kline{}, err
	}
	highValue, err := strconv.ParseFloat(high, 64)
	if err != nil {
		return Kline{}, err
	}
	lowValue, err := strconv.ParseFloat(low, 64)
	if err != nil {
		return Kline{}, err
	}
	closeValue, err := strconv.ParseFloat(closePrice, 64)
	if err != nil {
		return Kline{}, err
	}
	volumeValue, err := strconv.ParseFloat(volume, 64)
	if err != nil {
		return Kline{}, err
	}
	return Kline{
		OpenTime:  openTimeMs,
		Open:      openValue,
		High:      highValue,
		Low:       lowValue,
		Close:     closeValue,
		Volume:    volumeValue,
		CloseTime: closeTimeMs,
	}, nil
}

func sortKlines(klines []Kline) {
	for i := 1; i < len(klines); i++ {
		for j := i; j > 0 && klines[j-1].OpenTime > klines[j].OpenTime; j-- {
			klines[j-1], klines[j] = klines[j], klines[j-1]
		}
	}
}

func bybitKlineInterval(interval string) (string, error) {
	switch interval {
	case "1m":
		return "1", nil
	case "3m":
		return "3", nil
	case "5m":
		return "5", nil
	case "15m":
		return "15", nil
	case "30m":
		return "30", nil
	case "1h":
		return "60", nil
	case "2h":
		return "120", nil
	case "4h":
		return "240", nil
	case "6h":
		return "360", nil
	case "12h":
		return "720", nil
	case "1d":
		return "D", nil
	default:
		return "", fmt.Errorf("unsupported bybit interval: %s", interval)
	}
}

func okxKlineBar(interval string) (string, error) {
	switch interval {
	case "1m", "3m", "5m", "15m", "30m":
		return interval, nil
	case "1h":
		return "1H", nil
	case "2h":
		return "2H", nil
	case "4h":
		return "4H", nil
	case "6h":
		return "6H", nil
	case "12h":
		return "12H", nil
	case "1d":
		return "1D", nil
	default:
		return "", fmt.Errorf("unsupported okx interval: %s", interval)
	}
}

func okxSwapInstrument(symbol string) string {
	symbol = strings.TrimSuffix(Normalize(symbol), "PERP")
	if strings.HasSuffix(symbol, "USDT") {
		return strings.TrimSuffix(symbol, "USDT") + "-USDT-SWAP"
	}
	return symbol
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// getKlinesFromHyperliquid fetches kline data from Hyperliquid API for xyz dex assets
func getKlinesFromHyperliquid(symbol, interval string, limit int) ([]Kline, error) {
	return getKlinesFromHyperliquidContext(context.Background(), symbol, interval, limit)
}

func getKlinesFromHyperliquidContext(ctx context.Context, symbol, interval string, limit int) ([]Kline, error) {
	// Remove xyz: prefix if present for the API call
	baseCoin := strings.TrimPrefix(hyperliquidCoin(symbol), "xyz:")

	// Map interval to Hyperliquid format
	hlInterval := hyperliquid.MapTimeframe(interval)

	// Create Hyperliquid client
	client := hyperliquid.NewClient()

	// Fetch candles
	candles, err := client.GetCandles(ctx, baseCoin, hlInterval, limit)
	if err != nil {
		return nil, fmt.Errorf("Hyperliquid API error: %w", err)
	}

	// Convert to market.Kline format
	klines := make([]Kline, len(candles))
	for i, c := range candles {
		open, _ := strconv.ParseFloat(c.Open, 64)
		high, _ := strconv.ParseFloat(c.High, 64)
		low, _ := strconv.ParseFloat(c.Low, 64)
		closePrice, _ := strconv.ParseFloat(c.Close, 64)
		volume, _ := strconv.ParseFloat(c.Volume, 64)

		klines[i] = Kline{
			OpenTime:  c.OpenTime,
			Open:      open,
			High:      high,
			Low:       low,
			Close:     closePrice,
			Volume:    volume,
			CloseTime: c.CloseTime,
		}
	}

	return klines, nil
}

// calculateTimeframeSeries calculates series data for a single timeframe
func calculateTimeframeSeries(klines []Kline, timeframe string, count int, smaPeriods ...int) *TimeframeSeriesData {
	if count <= 0 {
		count = 10 // default
	}

	data := &TimeframeSeriesData{
		Timeframe:     timeframe,
		ComputeBars:   append([]Kline(nil), klines...),
		Klines:        make([]KlineBar, 0, count),
		MidPrices:     make([]float64, 0, count),
		EMA20Values:   make([]float64, 0, count),
		EMA50Values:   make([]float64, 0, count),
		SMAValues:     make(map[int][]float64),
		ADXValues:     make([]float64, 0, count),
		PlusDIValues:  make([]float64, 0, count),
		MinusDIValues: make([]float64, 0, count),
		SARValues:     make([]float64, 0, count),
		SARUptrend:    make([]bool, 0, count),
		SARFlipUp:     make([]bool, 0, count),
		SARFlipDown:   make([]bool, 0, count),
		MACDValues:    make([]float64, 0, count),
		RSI7Values:    make([]float64, 0, count),
		RSI14Values:   make([]float64, 0, count),
		Volume:        make([]float64, 0, count),
		BOLLUpper:     make([]float64, 0, count),
		BOLLMiddle:    make([]float64, 0, count),
		BOLLLower:     make([]float64, 0, count),
	}

	// Get latest N data points based on count from config
	start := len(klines) - count
	if start < 0 {
		start = 0
	}

	for i := start; i < len(klines); i++ {
		// Store full OHLCV kline data
		data.Klines = append(data.Klines, KlineBar{
			Time:   klines[i].OpenTime,
			Open:   klines[i].Open,
			High:   klines[i].High,
			Low:    klines[i].Low,
			Close:  klines[i].Close,
			Volume: klines[i].Volume,
		})

		// Keep MidPrices and Volume for backward compatibility
		data.MidPrices = append(data.MidPrices, klines[i].Close)
		data.Volume = append(data.Volume, klines[i].Volume)

		// Calculate EMA20 for each point
		if i >= 19 {
			ema20 := calculateEMA(klines[:i+1], 20)
			data.EMA20Values = append(data.EMA20Values, ema20)
		}

		// Calculate EMA50 for each point
		if i >= 49 {
			ema50 := calculateEMA(klines[:i+1], 50)
			data.EMA50Values = append(data.EMA50Values, ema50)
		}

		// Calculate SMA for configured periods
		for _, p := range smaPeriods {
			if p > 0 && i >= p-1 {
				sma := calculateSMA(klines[:i+1], p)
				data.SMAValues[p] = append(data.SMAValues[p], sma)
			}
		}

		// Calculate MACD for each point
		if i >= 25 {
			macd := calculateMACD(klines[:i+1])
			data.MACDValues = append(data.MACDValues, macd)
		}

		// Calculate RSI for each point
		if i >= 7 {
			rsi7 := calculateRSI(klines[:i+1], 7)
			data.RSI7Values = append(data.RSI7Values, rsi7)
		}
		if i >= 14 {
			rsi14 := calculateRSI(klines[:i+1], 14)
			data.RSI14Values = append(data.RSI14Values, rsi14)
		}

		// Calculate Bollinger Bands (period 20, std dev multiplier 2)
		if i >= 19 {
			upper, middle, lower := calculateBOLL(klines[:i+1], 20, 2.0)
			data.BOLLUpper = append(data.BOLLUpper, upper)
			data.BOLLMiddle = append(data.BOLLMiddle, middle)
			data.BOLLLower = append(data.BOLLLower, lower)
		}

		// Calculate ADX for each point
		if i >= 28 {
			adx, plusDI, minusDI := calculateADX(klines[:i+1], 14)
			data.ADXValues = append(data.ADXValues, adx)
			data.PlusDIValues = append(data.PlusDIValues, plusDI)
			data.MinusDIValues = append(data.MinusDIValues, minusDI)
		}

		// Calculate Parabolic SAR for each point
		if i >= 1 {
			sar, isUp, flipUp, flipDown := calculateParabolicSAR(klines[:i+1])
			data.SARValues = append(data.SARValues, sar)
			data.SARUptrend = append(data.SARUptrend, isUp)
			data.SARFlipUp = append(data.SARFlipUp, flipUp)
			data.SARFlipDown = append(data.SARFlipDown, flipDown)
		}
	}

	// Calculate ATR14
	data.ATR14 = calculateATR(klines, 14)
	data.BBMACD = calculateBBMACD(klines)

	return data
}

// calculatePriceChangeByBars calculates how many K-lines to look back for price change based on timeframe
func calculatePriceChangeByBars(klines []Kline, timeframe string, targetMinutes int) float64 {
	if len(klines) < 2 {
		return 0
	}

	// Parse timeframe to minutes
	tfMinutes := parseTimeframeToMinutes(timeframe)
	if tfMinutes <= 0 {
		return 0
	}

	// Calculate how many K-lines to look back
	barsBack := targetMinutes / tfMinutes
	if barsBack < 1 {
		barsBack = 1
	}

	currentPrice := klines[len(klines)-1].Close
	idx := len(klines) - 1 - barsBack
	if idx < 0 {
		idx = 0
	}

	oldPrice := klines[idx].Close
	if oldPrice > 0 {
		return ((currentPrice - oldPrice) / oldPrice) * 100
	}
	return 0
}

// parseTimeframeToMinutes parses timeframe string to minutes
func parseTimeframeToMinutes(tf string) int {
	switch tf {
	case "1m":
		return 1
	case "3m":
		return 3
	case "5m":
		return 5
	case "15m":
		return 15
	case "30m":
		return 30
	case "1h":
		return 60
	case "2h":
		return 120
	case "4h":
		return 240
	case "6h":
		return 360
	case "8h":
		return 480
	case "12h":
		return 720
	case "1d":
		return 1440
	case "3d":
		return 4320
	case "1w":
		return 10080
	default:
		return 0
	}
}

// calculateIntradaySeries calculates intraday series data
func calculateIntradaySeries(klines []Kline, smaPeriods ...int) *IntradayData {
	data := &IntradayData{
		MidPrices:     make([]float64, 0, 10),
		EMA20Values:   make([]float64, 0, 10),
		SMAValues:     make(map[int][]float64),
		ADXValues:     make([]float64, 0, 10),
		PlusDIValues:  make([]float64, 0, 10),
		MinusDIValues: make([]float64, 0, 10),
		SARValues:     make([]float64, 0, 10),
		SARUptrend:    make([]bool, 0, 10),
		SARFlipUp:     make([]bool, 0, 10),
		SARFlipDown:   make([]bool, 0, 10),
		MACDValues:    make([]float64, 0, 10),
		RSI7Values:    make([]float64, 0, 10),
		RSI14Values:   make([]float64, 0, 10),
		Volume:        make([]float64, 0, 10),
	}

	// Get latest 10 data points
	start := len(klines) - 10
	if start < 0 {
		start = 0
	}

	for i := start; i < len(klines); i++ {
		data.MidPrices = append(data.MidPrices, klines[i].Close)
		data.Volume = append(data.Volume, klines[i].Volume)

		// Calculate EMA20 for each point
		if i >= 19 {
			ema20 := calculateEMA(klines[:i+1], 20)
			data.EMA20Values = append(data.EMA20Values, ema20)
		}

		// Calculate SMA for configured periods
		for _, p := range smaPeriods {
			if p > 0 && i >= p-1 {
				sma := calculateSMA(klines[:i+1], p)
				data.SMAValues[p] = append(data.SMAValues[p], sma)
			}
		}

		// Calculate MACD for each point
		if i >= 25 {
			macd := calculateMACD(klines[:i+1])
			data.MACDValues = append(data.MACDValues, macd)
		}

		// Calculate RSI for each point
		if i >= 7 {
			rsi7 := calculateRSI(klines[:i+1], 7)
			data.RSI7Values = append(data.RSI7Values, rsi7)
		}
		if i >= 14 {
			rsi14 := calculateRSI(klines[:i+1], 14)
			data.RSI14Values = append(data.RSI14Values, rsi14)
		}

		// Calculate ADX for each point
		if i >= 28 {
			adx, plusDI, minusDI := calculateADX(klines[:i+1], 14)
			data.ADXValues = append(data.ADXValues, adx)
			data.PlusDIValues = append(data.PlusDIValues, plusDI)
			data.MinusDIValues = append(data.MinusDIValues, minusDI)
		}

		// Calculate Parabolic SAR for each point
		if i >= 1 {
			sar, isUp, flipUp, flipDown := calculateParabolicSAR(klines[:i+1])
			data.SARValues = append(data.SARValues, sar)
			data.SARUptrend = append(data.SARUptrend, isUp)
			data.SARFlipUp = append(data.SARFlipUp, flipUp)
			data.SARFlipDown = append(data.SARFlipDown, flipDown)
		}
	}

	// Calculate 3m ATR14
	data.ATR14 = calculateATR(klines, 14)

	return data
}

// calculateLongerTermData calculates longer-term data
func calculateLongerTermData(klines []Kline, smaPeriods ...int) *LongerTermData {
	data := &LongerTermData{
		SMA:         make(map[int]float64),
		MACDValues:  make([]float64, 0, 10),
		RSI14Values: make([]float64, 0, 10),
	}

	// Calculate EMA
	data.EMA20 = calculateEMA(klines, 20)
	data.EMA50 = calculateEMA(klines, 50)

	// Calculate ATR
	data.ATR3 = calculateATR(klines, 3)
	data.ATR14 = calculateATR(klines, 14)

	// Calculate ADX
	if len(klines) >= 29 {
		data.ADX, data.PlusDI, data.MinusDI = calculateADX(klines, 14)
	}

	// Calculate Parabolic SAR
	if len(klines) >= 2 {
		data.SAR, data.SARIsUptrend, data.SARFlipUp, data.SARFlipDown = calculateParabolicSAR(klines)
	}

	// Calculate volume
	if len(klines) > 0 {
		data.CurrentVolume = klines[len(klines)-1].Volume
		// Calculate average volume
		sum := 0.0
		for _, k := range klines {
			sum += k.Volume
		}
		data.AverageVolume = sum / float64(len(klines))
	}

	// Calculate MACD and RSI series
	start := len(klines) - 10
	if start < 0 {
		start = 0
	}

	for i := start; i < len(klines); i++ {
		if i >= 25 {
			macd := calculateMACD(klines[:i+1])
			data.MACDValues = append(data.MACDValues, macd)
		}
		if i >= 14 {
			rsi14 := calculateRSI(klines[:i+1], 14)
			data.RSI14Values = append(data.RSI14Values, rsi14)
		}
	}

	return data
}

// GetBoxData fetches 1h klines and calculates box data for a symbol
func GetBoxData(symbol string) (*BoxData, error) {
	symbol = Normalize(symbol)

	// Fetch 500 1h klines
	var klines []Kline
	var err error

	if IsXyzDexAsset(symbol) {
		klines, err = getKlinesFromHyperliquid(symbol, "1h", LongBoxPeriod)
	} else {
		klines, err = getKlinesFromOfficialFuturesContext(context.Background(), symbol, "1h", LongBoxPeriod, "binance")
	}

	if err != nil {
		return nil, fmt.Errorf("failed to get 1h klines: %w", err)
	}

	if len(klines) == 0 {
		return nil, fmt.Errorf("no kline data available")
	}

	currentPrice := klines[len(klines)-1].Close

	return calculateBoxData(klines, currentPrice), nil
}
