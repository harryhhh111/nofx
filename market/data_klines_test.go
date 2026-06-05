package market

import "testing"

func TestParseBybitKlinePayloadSortsAscending(t *testing.T) {
	body := []byte(`{"retCode":0,"retMsg":"OK","result":{"list":[["2000","2","3","1","2.5","20","50"],["1000","1","2","0.5","1.5","10","15"]]}}`)

	klines, err := parseBybitKlinePayload(body)
	if err != nil {
		t.Fatalf("parseBybitKlinePayload returned error: %v", err)
	}
	if len(klines) != 2 {
		t.Fatalf("expected 2 klines, got %d", len(klines))
	}
	if klines[0].OpenTime != 1000 || klines[1].OpenTime != 2000 {
		t.Fatalf("expected ascending klines, got %+v", klines)
	}
	if klines[0].Close != 1.5 || klines[1].Close != 2.5 {
		t.Fatalf("unexpected close values: %+v", klines)
	}
}

func TestParseOKXKlinePayloadSortsAscending(t *testing.T) {
	body := []byte(`{"code":"0","msg":"","data":[["2000","2","3","1","2.5","20","20","50","0"],["1000","1","2","0.5","1.5","10","10","15","1"]]}`)

	klines, err := parseOKXKlinePayload(body)
	if err != nil {
		t.Fatalf("parseOKXKlinePayload returned error: %v", err)
	}
	if len(klines) != 2 {
		t.Fatalf("expected 2 klines, got %d", len(klines))
	}
	if klines[0].OpenTime != 1000 || klines[1].OpenTime != 2000 {
		t.Fatalf("expected ascending klines, got %+v", klines)
	}
	if klines[0].Close != 1.5 || klines[1].Close != 2.5 {
		t.Fatalf("unexpected close values: %+v", klines)
	}
}

func TestOfficialKlineIntervalMappings(t *testing.T) {
	bybit, err := bybitKlineInterval("15m")
	if err != nil || bybit != "15" {
		t.Fatalf("unexpected bybit interval: %q %v", bybit, err)
	}
	okx, err := okxKlineBar("1h")
	if err != nil || okx != "1H" {
		t.Fatalf("unexpected okx interval: %q %v", okx, err)
	}
	inst := okxSwapInstrument("BTCUSDT")
	if inst != "BTC-USDT-SWAP" {
		t.Fatalf("unexpected okx instrument: %s", inst)
	}
}

func TestOfficialKlineSourceRejectsUnsupportedExchange(t *testing.T) {
	_, err := getKlinesFromOfficialFuturesContext(t.Context(), "BTCUSDT", "15m", 2, "bitget")
	if err == nil {
		t.Fatalf("expected unsupported exchange to fail instead of falling back")
	}
}
