package aster

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/assert"
	"nofx/trader/testutil"
	"nofx/trader/types"
)

// ============================================================
// 1. AsterTraderTestSuite - inherits base test suite
// ============================================================

// AsterTraderTestSuite Aster trader test suite
// Inherits TraderTestSuite and adds Aster specific mock logic
type AsterTraderTestSuite struct {
	*testutil.TraderTestSuite // Embeds base test suite
	mockServer              *httptest.Server
}

// NewAsterTraderTestSuite creates Aster test suite
func NewAsterTraderTestSuite(t *testing.T) *AsterTraderTestSuite {
	// Create mock HTTP server
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Return different mock responses based on URL path
		path := r.URL.Path

		var respBody interface{}

		switch {
		// Mock GetBalance - /fapi/v3/balance (returns array)
		case path == "/fapi/v3/balance":
			respBody = []map[string]interface{}{
				{
					"asset":              "USDT",
					"walletBalance":      "10000.00",
					"unrealizedProfit":   "100.50",
					"marginBalance":      "10100.50",
					"maintMargin":        "200.00",
					"initialMargin":      "2000.00",
					"maxWithdrawAmount":  "8000.00",
					"crossWalletBalance": "10000.00",
					"crossUnPnl":         "100.50",
					"availableBalance":   "8000.00",
				},
			}

		// Mock GetPositions - /fapi/v3/positionRisk
		case path == "/fapi/v3/positionRisk":
			respBody = []map[string]interface{}{
				{
					"symbol":           "BTCUSDT",
					"positionAmt":      "0.5",
					"entryPrice":       "50000.00",
					"markPrice":        "50500.00",
					"unRealizedProfit": "250.00",
					"liquidationPrice": "45000.00",
					"leverage":         "10",
					"positionSide":     "LONG",
				},
			}

		// Mock GetMarketPrice - /fapi/v3/ticker/price (returns single object)
		case path == "/fapi/v3/ticker/price":
			// Get symbol from query parameters
			symbol := r.URL.Query().Get("symbol")
			if symbol == "" {
				symbol = "BTCUSDT"
			}
			// Return different price based on symbol
			price := "50000.00"
			if symbol == "ETHUSDT" {
				price = "3000.00"
			} else if symbol == "INVALIDUSDT" {
				// Return error response
				w.WriteHeader(http.StatusBadRequest)
				json.NewEncoder(w).Encode(map[string]interface{}{
					"code": -1121,
					"msg":  "Invalid symbol",
				})
				return
			}
			respBody = map[string]interface{}{
				"symbol": symbol,
				"price":  price,
			}

		// Mock ExchangeInfo - /fapi/v3/exchangeInfo
		case path == "/fapi/v3/exchangeInfo":
			respBody = map[string]interface{}{
				"symbols": []map[string]interface{}{
					{
						"symbol":             "BTCUSDT",
						"pricePrecision":     1,
						"quantityPrecision":  3,
						"baseAssetPrecision": 8,
						"quotePrecision":     8,
						"filters": []map[string]interface{}{
							{
								"filterType": "PRICE_FILTER",
								"tickSize":   "0.1",
							},
							{
								"filterType": "LOT_SIZE",
								"stepSize":   "0.001",
							},
						},
					},
					{
						"symbol":             "ETHUSDT",
						"pricePrecision":     2,
						"quantityPrecision":  3,
						"baseAssetPrecision": 8,
						"quotePrecision":     8,
						"filters": []map[string]interface{}{
							{
								"filterType": "PRICE_FILTER",
								"tickSize":   "0.01",
							},
							{
								"filterType": "LOT_SIZE",
								"stepSize":   "0.001",
							},
						},
					},
				},
			}

		// Mock CreateOrder - /fapi/v1/order and /fapi/v3/order
		case (path == "/fapi/v1/order" || path == "/fapi/v3/order") && r.Method == "POST":
			// Parse parameters from request to determine symbol
			bodyBytes, _ := io.ReadAll(r.Body)
			var orderParams map[string]interface{}
			json.Unmarshal(bodyBytes, &orderParams)

			symbol := "BTCUSDT"
			if s, ok := orderParams["symbol"].(string); ok {
				symbol = s
			}

			respBody = map[string]interface{}{
				"orderId": 123456,
				"symbol":  symbol,
				"status":  "FILLED",
				"side":    orderParams["side"],
				"type":    orderParams["type"],
			}

		// Mock CancelOrder - /fapi/v3/order (DELETE)
		case path == "/fapi/v3/order" && r.Method == "DELETE":
			respBody = map[string]interface{}{
				"orderId": 123456,
				"symbol":  "BTCUSDT",
				"status":  "CANCELED",
			}

		// Mock ListOpenOrders - /fapi/v1/openOrders and /fapi/v3/openOrders
		case path == "/fapi/v1/openOrders" || path == "/fapi/v3/openOrders":
			respBody = []map[string]interface{}{}

		// Mock SetLeverage - /fapi/v1/leverage
		case path == "/fapi/v1/leverage":
			respBody = map[string]interface{}{
				"leverage": 10,
				"symbol":   "BTCUSDT",
			}

		// Mock SetMarginMode - /fapi/v1/marginType
		case path == "/fapi/v1/marginType":
			respBody = map[string]interface{}{
				"code": 200,
				"msg":  "success",
			}

		// Default: empty response
		default:
			respBody = map[string]interface{}{}
		}

		// Serialize response
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(respBody)
	}))

	// Generate a private key for testing
	privateKey, _ := crypto.GenerateKey()

	// Create mock trader using mock server's URL
	traderInstance := &AsterTrader{
		ctx:             context.Background(),
		user:            "0x1234567890123456789012345678901234567890",
		signer:          "0xabcdefabcdefabcdefabcdefabcdefabcdefabcd",
		privateKey:      privateKey,
		client:          mockServer.Client(),
		baseURL:         mockServer.URL, // Use mock server's URL
		symbolPrecision: make(map[string]SymbolPrecision),
	}

	// Create base suite
	baseSuite := testutil.NewTraderTestSuite(t, traderInstance)

	return &AsterTraderTestSuite{
		TraderTestSuite: baseSuite,
		mockServer:      mockServer,
	}
}

// Cleanup cleans up resources
func (s *AsterTraderTestSuite) Cleanup() {
	if s.mockServer != nil {
		s.mockServer.Close()
	}
	s.TraderTestSuite.Cleanup()
}

// ============================================================
// 2. Run common tests using AsterTraderTestSuite
// ============================================================

// TestAsterTrader_InterfaceCompliance tests interface compliance
func TestAsterTrader_InterfaceCompliance(t *testing.T) {
	var _ types.Trader = (*AsterTrader)(nil)
}

// TestAsterTrader_CommonInterface runs all common interface tests using test suite
func TestAsterTrader_CommonInterface(t *testing.T) {
	// Create test suite
	suite := NewAsterTraderTestSuite(t)
	defer suite.Cleanup()

	// Run all common interface tests
	suite.RunAllTests()
}

func TestAsterOpenLongUsesIOC(t *testing.T) {
	var captured url.Values
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/fapi/v3/allOpenOrders" && r.Method == "DELETE":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{})
		case r.URL.Path == "/fapi/v1/leverage":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"leverage": 10})
		case r.URL.Path == "/fapi/v3/ticker/price":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"symbol": "BTCUSDT", "price": "50000"})
		case r.URL.Path == "/fapi/v3/exchangeInfo":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"symbols": []map[string]interface{}{
					{
						"symbol":            "BTCUSDT",
						"pricePrecision":    1,
						"quantityPrecision": 3,
						"filters": []map[string]interface{}{
							{"filterType": "PRICE_FILTER", "tickSize": "0.1"},
							{"filterType": "LOT_SIZE", "stepSize": "0.001"},
						},
					},
				},
			})
		case r.URL.Path == "/fapi/v3/order" && r.Method == "POST":
			body, _ := io.ReadAll(r.Body)
			captured, _ = url.ParseQuery(string(body))
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"orderId": 1,
				"symbol":  "BTCUSDT",
				"status":  "NEW",
			})
		default:
			_ = json.NewEncoder(w).Encode(map[string]interface{}{})
		}
	}))
	defer mockServer.Close()

	privateKey, _ := crypto.GenerateKey()
	trader := &AsterTrader{
		ctx:             context.Background(),
		user:            "0x1234567890123456789012345678901234567890",
		signer:          "0xabcdefabcdefabcdefabcdefabcdefabcdefabcd",
		privateKey:      privateKey,
		client:          mockServer.Client(),
		baseURL:         mockServer.URL,
		symbolPrecision: make(map[string]SymbolPrecision),
	}

	if _, err := trader.OpenLong("BTCUSDT", 0.01, 10); err != nil {
		t.Fatalf("OpenLong failed: %v", err)
	}
	if got := captured.Get("timeInForce"); got != asterAggressiveLimitTIF {
		t.Fatalf("timeInForce = %q, want %q", got, asterAggressiveLimitTIF)
	}
}

func TestAsterClearsResidualOrdersForClosedSymbols(t *testing.T) {
	cancelCalls := 0
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/fapi/v3/allOpenOrders" && r.Method == "DELETE":
			cancelCalls++
			_ = json.NewEncoder(w).Encode(map[string]interface{}{})
		default:
			_ = json.NewEncoder(w).Encode(map[string]interface{}{})
		}
	}))
	defer mockServer.Close()

	privateKey, _ := crypto.GenerateKey()
	trader := &AsterTrader{
		ctx:             context.Background(),
		user:            "0x1234567890123456789012345678901234567890",
		signer:          "0xabcdefabcdefabcdefabcdefabcdefabcdefabcd",
		privateKey:      privateKey,
		client:          mockServer.Client(),
		baseURL:         mockServer.URL,
		symbolPrecision: make(map[string]SymbolPrecision),
	}

	trader.clearResidualOrdersForClosedSymbols(
		map[string]bool{"BTCUSDT": true},
		[]map[string]interface{}{},
	)
	if cancelCalls == 0 {
		t.Fatal("expected residual order cleanup to call CancelAllOrders at least once")
	}
}

func TestAsterCloseLongUsesReduceOnly(t *testing.T) {
	var captured url.Values
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/fapi/v3/ticker/price":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"symbol": "BTCUSDT", "price": "50000"})
		case r.URL.Path == "/fapi/v3/exchangeInfo":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"symbols": []map[string]interface{}{
					{
						"symbol":            "BTCUSDT",
						"pricePrecision":    1,
						"quantityPrecision": 3,
						"filters": []map[string]interface{}{
							{"filterType": "PRICE_FILTER", "tickSize": "0.1"},
							{"filterType": "LOT_SIZE", "stepSize": "0.001"},
						},
					},
				},
			})
		case r.URL.Path == "/fapi/v3/order" && r.Method == "POST":
			body, _ := io.ReadAll(r.Body)
			captured, _ = url.ParseQuery(string(body))
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"orderId": 1,
				"symbol":  "BTCUSDT",
				"status":  "FILLED",
			})
		case r.URL.Path == "/fapi/v3/allOpenOrders" && r.Method == "DELETE":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{})
		default:
			_ = json.NewEncoder(w).Encode(map[string]interface{}{})
		}
	}))
	defer mockServer.Close()

	privateKey, _ := crypto.GenerateKey()
	trader := &AsterTrader{
		ctx:             context.Background(),
		user:            "0x1234567890123456789012345678901234567890",
		signer:          "0xabcdefabcdefabcdefabcdefabcdefabcdefabcd",
		privateKey:      privateKey,
		client:          mockServer.Client(),
		baseURL:         mockServer.URL,
		symbolPrecision: make(map[string]SymbolPrecision),
	}

	if _, err := trader.CloseLong("BTCUSDT", 0.01); err != nil {
		t.Fatalf("CloseLong failed: %v", err)
	}
	if got := captured.Get("reduceOnly"); got != "true" {
		t.Fatalf("reduceOnly = %q, want %q", got, "true")
	}
}

func TestAsterStopOrdersUseReduceOnly(t *testing.T) {
	var captured []url.Values
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/fapi/v3/exchangeInfo":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"symbols": []map[string]interface{}{
					{
						"symbol":            "BTCUSDT",
						"pricePrecision":    1,
						"quantityPrecision": 3,
						"filters": []map[string]interface{}{
							{"filterType": "PRICE_FILTER", "tickSize": "0.1"},
							{"filterType": "LOT_SIZE", "stepSize": "0.001"},
						},
					},
				},
			})
		case r.URL.Path == "/fapi/v3/order" && r.Method == "POST":
			body, _ := io.ReadAll(r.Body)
			values, _ := url.ParseQuery(string(body))
			captured = append(captured, values)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"orderId": 1,
				"symbol":  "BTCUSDT",
				"status":  "NEW",
			})
		default:
			_ = json.NewEncoder(w).Encode(map[string]interface{}{})
		}
	}))
	defer mockServer.Close()

	privateKey, _ := crypto.GenerateKey()
	trader := &AsterTrader{
		ctx:             context.Background(),
		user:            "0x1234567890123456789012345678901234567890",
		signer:          "0xabcdefabcdefabcdefabcdefabcdefabcdefabcd",
		privateKey:      privateKey,
		client:          mockServer.Client(),
		baseURL:         mockServer.URL,
		symbolPrecision: make(map[string]SymbolPrecision),
	}

	if err := trader.SetStopLoss("BTCUSDT", "LONG", 0.01, 49000); err != nil {
		t.Fatalf("SetStopLoss failed: %v", err)
	}
	if err := trader.SetTakeProfit("BTCUSDT", "LONG", 0.01, 51000); err != nil {
		t.Fatalf("SetTakeProfit failed: %v", err)
	}
	if len(captured) != 2 {
		t.Fatalf("captured order count = %d, want 2", len(captured))
	}
	for i, values := range captured {
		if got := values.Get("reduceOnly"); got != "true" {
			t.Fatalf("order %d reduceOnly = %q, want %q", i, got, "true")
		}
	}
}

func TestAsterOpenLongFailsWhenPreCancelFails(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/fapi/v3/allOpenOrders" && r.Method == "DELETE":
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"code": -2011,
				"msg":  "cancel failed",
			})
		default:
			_ = json.NewEncoder(w).Encode(map[string]interface{}{})
		}
	}))
	defer mockServer.Close()

	privateKey, _ := crypto.GenerateKey()
	trader := &AsterTrader{
		ctx:             context.Background(),
		user:            "0x1234567890123456789012345678901234567890",
		signer:          "0xabcdefabcdefabcdefabcdefabcdefabcdefabcd",
		privateKey:      privateKey,
		client:          mockServer.Client(),
		baseURL:         mockServer.URL,
		symbolPrecision: make(map[string]SymbolPrecision),
	}

	if _, err := trader.OpenLong("BTCUSDT", 0.01, 10); err == nil {
		t.Fatal("expected OpenLong to fail when pre-cancel fails")
	}
}

func TestAsterCloseLongKeepsOrdersWhenPositionRemains(t *testing.T) {
	cancelCalls := 0
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/fapi/v3/ticker/price":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"symbol": "BTCUSDT", "price": "50000"})
		case r.URL.Path == "/fapi/v3/exchangeInfo":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"symbols": []map[string]interface{}{
					{
						"symbol":            "BTCUSDT",
						"pricePrecision":    1,
						"quantityPrecision": 3,
						"filters": []map[string]interface{}{
							{"filterType": "PRICE_FILTER", "tickSize": "0.1"},
							{"filterType": "LOT_SIZE", "stepSize": "0.001"},
						},
					},
				},
			})
		case r.URL.Path == "/fapi/v3/order" && r.Method == "POST":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"orderId": 1,
				"symbol":  "BTCUSDT",
				"status":  "PARTIALLY_FILLED",
			})
		case r.URL.Path == "/fapi/v3/positionRisk":
			_ = json.NewEncoder(w).Encode([]map[string]interface{}{
				{
					"symbol":           "BTCUSDT",
					"positionAmt":      "0.005",
					"entryPrice":       "50000.00",
					"markPrice":        "50500.00",
					"unRealizedProfit": "10.00",
					"liquidationPrice": "45000.00",
					"leverage":         "10",
				},
			})
		case r.URL.Path == "/fapi/v3/allOpenOrders" && r.Method == "DELETE":
			cancelCalls++
			_ = json.NewEncoder(w).Encode(map[string]interface{}{})
		default:
			_ = json.NewEncoder(w).Encode(map[string]interface{}{})
		}
	}))
	defer mockServer.Close()

	privateKey, _ := crypto.GenerateKey()
	trader := &AsterTrader{
		ctx:             context.Background(),
		user:            "0x1234567890123456789012345678901234567890",
		signer:          "0xabcdefabcdefabcdefabcdefabcdefabcdefabcd",
		privateKey:      privateKey,
		client:          mockServer.Client(),
		baseURL:         mockServer.URL,
		symbolPrecision: make(map[string]SymbolPrecision),
	}

	if _, err := trader.CloseLong("BTCUSDT", 0.01); err != nil {
		t.Fatalf("CloseLong failed: %v", err)
	}
	if cancelCalls != 0 {
		t.Fatalf("cancelCalls = %d, want 0 when position remains", cancelCalls)
	}
}

// ============================================================
// 3. Aster specific unit tests
// ============================================================

// TestNewAsterTrader tests creating Aster trader
func TestNewAsterTrader(t *testing.T) {
	tests := []struct {
		name          string
		user          string
		signer        string
		privateKeyHex string
		wantError     bool
		errorContains string
	}{
		{
			name:          "successful creation",
			user:          "0x1234567890123456789012345678901234567890",
			signer:        "0xabcdefabcdefabcdefabcdefabcdefabcdefabcd",
			privateKeyHex: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
			wantError:     false,
		},
		{
			name:          "invalid private key format",
			user:          "0x1234567890123456789012345678901234567890",
			signer:        "0xabcdefabcdefabcdefabcdefabcdefabcdefabcd",
			privateKeyHex: "invalid_key",
			wantError:     true,
			errorContains: "failed to parse private key",
		},
		{
			name:          "private key with 0x prefix",
			user:          "0x1234567890123456789012345678901234567890",
			signer:        "0xabcdefabcdefabcdefabcdefabcdefabcdefabcd",
			privateKeyHex: "0x0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
			wantError:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			at, err := NewAsterTrader(tt.user, tt.signer, tt.privateKeyHex)

			if tt.wantError {
				assert.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
				assert.Nil(t, at)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, at)
				if at != nil {
					assert.Equal(t, tt.user, at.user)
					assert.Equal(t, tt.signer, at.signer)
					assert.NotNil(t, at.privateKey)
				}
			}
		})
	}
}

func TestAsterCancelStopLossOrdersUsesV3Endpoint(t *testing.T) {
	var deletedOrderIDs []int64
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/fapi/v3/time":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"serverTime": time.Now().UnixMilli()})
		case r.URL.Path == "/fapi/v3/openOrders" && r.Method == "GET":
			_ = json.NewEncoder(w).Encode([]map[string]interface{}{
				{
					"orderId":      1001,
					"symbol":       "BTCUSDT",
					"type":         "STOP_MARKET",
					"side":         "SELL",
					"positionSide": "LONG",
					"stopPrice":    "49000",
					"origQty":      "0.01",
					"status":       "NEW",
				},
				{
					"orderId":      1002,
					"symbol":       "BTCUSDT",
					"type":         "TAKE_PROFIT_MARKET",
					"side":         "SELL",
					"positionSide": "LONG",
					"stopPrice":    "60000",
					"origQty":      "0.01",
					"status":       "NEW",
				},
			})
		case r.URL.Path == "/fapi/v3/order" && r.Method == "DELETE":
			orderID := r.URL.Query().Get("orderId")
			if id, err := strconv.ParseInt(orderID, 10, 64); err == nil {
				deletedOrderIDs = append(deletedOrderIDs, id)
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"orderId": orderID,
				"symbol":  "BTCUSDT",
				"status":  "CANCELED",
			})
		default:
			_ = json.NewEncoder(w).Encode(map[string]interface{}{})
		}
	}))
	defer mockServer.Close()

	privateKey, _ := crypto.GenerateKey()
	trader := &AsterTrader{
		ctx:             context.Background(),
		user:            "0x1234567890123456789012345678901234567890",
		signer:          "0xabcdefabcdefabcdefabcdefabcdefabcdefabcd",
		privateKey:      privateKey,
		client:          mockServer.Client(),
		baseURL:         mockServer.URL,
		symbolPrecision: make(map[string]SymbolPrecision),
	}

	if err := trader.CancelStopLossOrders("BTCUSDT"); err != nil {
		t.Fatalf("CancelStopLossOrders failed: %v", err)
	}
	if len(deletedOrderIDs) != 1 || deletedOrderIDs[0] != 1001 {
		t.Fatalf("expected only STOP_MARKET order 1001 to be deleted via /fapi/v3/order, got %v", deletedOrderIDs)
	}
}
