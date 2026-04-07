package aster

import (
	"context"
	"crypto/ecdsa"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"math/big"
	"net/http"
	"net/url"
	"nofx/hook"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// AsterTrader Aster trading platform implementation
type AsterTrader struct {
	ctx        context.Context
	user       string            // Main wallet address (ERC20)
	signer     string            // API wallet address
	privateKey *ecdsa.PrivateKey // API wallet private key
	client     *http.Client
	baseURL    string

	// Cache symbol precision information
	symbolPrecision map[string]SymbolPrecision
	mu              sync.RWMutex
}

// SymbolPrecision Symbol precision information
type SymbolPrecision struct {
	PricePrecision    int
	QuantityPrecision int
	TickSize          float64 // Price tick size
	StepSize          float64 // Quantity step size
}

// NewAsterTrader Create Aster trader
// user: Main wallet address (login address)
// signer: API wallet address (obtained from https://www.asterdex.com/en/api-wallet)
// privateKey: API wallet private key (obtained from https://www.asterdex.com/en/api-wallet)
func NewAsterTrader(user, signer, privateKeyHex string) (*AsterTrader, error) {
	// Parse private key
	privKey, err := crypto.HexToECDSA(strings.TrimPrefix(privateKeyHex, "0x"))
	if err != nil {
		return nil, fmt.Errorf("failed to parse private key: %w", err)
	}
	client := &http.Client{
		Timeout: 30 * time.Second, // Increased to 30 seconds
		Transport: &http.Transport{
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: 10 * time.Second,
			IdleConnTimeout:       90 * time.Second,
		},
	}
	res := hook.HookExec[hook.NewAsterTraderResult](hook.NEW_ASTER_TRADER, user, client)
	if res != nil && res.Error() == nil {
		client = res.GetResult()
	}

	return &AsterTrader{
		ctx:             context.Background(),
		user:            user,
		signer:          signer,
		privateKey:      privKey,
		symbolPrecision: make(map[string]SymbolPrecision),
		client:          client,
		baseURL:         "https://fapi.asterdex.com",
	}, nil
}

// genNonce Generate microsecond timestamp
func (t *AsterTrader) genNonce() uint64 {
	return uint64(time.Now().UnixMicro())
}

// getPrecision Get symbol precision information
func (t *AsterTrader) getPrecision(symbol string) (SymbolPrecision, error) {
	t.mu.RLock()
	if prec, ok := t.symbolPrecision[symbol]; ok {
		t.mu.RUnlock()
		return prec, nil
	}
	t.mu.RUnlock()

	// Get exchange information
	resp, err := t.client.Get(t.baseURL + "/fapi/v3/exchangeInfo")
	if err != nil {
		return SymbolPrecision{}, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var info struct {
		Symbols []struct {
			Symbol            string                   `json:"symbol"`
			PricePrecision    int                      `json:"pricePrecision"`
			QuantityPrecision int                      `json:"quantityPrecision"`
			Filters           []map[string]interface{} `json:"filters"`
		} `json:"symbols"`
	}

	if err := json.Unmarshal(body, &info); err != nil {
		return SymbolPrecision{}, err
	}

	// Cache precision for all symbols
	t.mu.Lock()
	for _, s := range info.Symbols {
		prec := SymbolPrecision{
			PricePrecision:    s.PricePrecision,
			QuantityPrecision: s.QuantityPrecision,
		}

		// Parse filters to get tickSize and stepSize
		for _, filter := range s.Filters {
			filterType, _ := filter["filterType"].(string)
			switch filterType {
			case "PRICE_FILTER":
				if tickSizeStr, ok := filter["tickSize"].(string); ok {
					prec.TickSize, _ = strconv.ParseFloat(tickSizeStr, 64)
				}
			case "LOT_SIZE":
				if stepSizeStr, ok := filter["stepSize"].(string); ok {
					prec.StepSize, _ = strconv.ParseFloat(stepSizeStr, 64)
				}
			}
		}

		t.symbolPrecision[s.Symbol] = prec
	}
	t.mu.Unlock()

	if prec, ok := t.symbolPrecision[symbol]; ok {
		return prec, nil
	}

	return SymbolPrecision{}, fmt.Errorf("precision information not found for symbol %s", symbol)
}

// roundToTickSize Round price/quantity to the nearest multiple of tick size/step size
func roundToTickSize(value float64, tickSize float64) float64 {
	if tickSize <= 0 {
		return value
	}
	// Calculate how many tick sizes
	steps := value / tickSize
	// Round to the nearest integer
	roundedSteps := math.Round(steps)
	// Multiply back by tick size
	return roundedSteps * tickSize
}

// formatPrice Format price to correct precision and tick size
func (t *AsterTrader) formatPrice(symbol string, price float64) (float64, error) {
	prec, err := t.getPrecision(symbol)
	if err != nil {
		return 0, err
	}

	// Prioritize tick size to ensure price is a multiple of tick size
	if prec.TickSize > 0 {
		return roundToTickSize(price, prec.TickSize), nil
	}

	// If no tick size, round by precision
	multiplier := math.Pow10(prec.PricePrecision)
	return math.Round(price*multiplier) / multiplier, nil
}

// formatQuantity Format quantity to correct precision and step size
func (t *AsterTrader) formatQuantity(symbol string, quantity float64) (float64, error) {
	prec, err := t.getPrecision(symbol)
	if err != nil {
		return 0, err
	}

	// Prioritize step size to ensure quantity is a multiple of step size
	if prec.StepSize > 0 {
		return roundToTickSize(quantity, prec.StepSize), nil
	}

	// If no step size, round by precision
	multiplier := math.Pow10(prec.QuantityPrecision)
	return math.Round(quantity*multiplier) / multiplier, nil
}

// formatFloatWithPrecision Format float to string with specified precision (remove trailing zeros)
func (t *AsterTrader) formatFloatWithPrecision(value float64, precision int) string {
	// Format with specified precision
	formatted := strconv.FormatFloat(value, 'f', precision, 64)

	// Remove trailing zeros and decimal point (if any)
	formatted = strings.TrimRight(formatted, "0")
	formatted = strings.TrimRight(formatted, ".")

	return formatted
}

// normalizeAndStringify Normalize parameters and serialize to JSON string (sorted by key)
func (t *AsterTrader) normalizeAndStringify(params map[string]interface{}) (string, error) {
	normalized, err := t.normalize(params)
	if err != nil {
		return "", err
	}
	bs, err := json.Marshal(normalized)
	if err != nil {
		return "", err
	}
	return string(bs), nil
}

// normalize Recursively normalize parameters (sorted by key, all values converted to strings)
func (t *AsterTrader) normalize(v interface{}) (interface{}, error) {
	switch val := v.(type) {
	case map[string]interface{}:
		keys := make([]string, 0, len(val))
		for k := range val {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		newMap := make(map[string]interface{}, len(keys))
		for _, k := range keys {
			nv, err := t.normalize(val[k])
			if err != nil {
				return nil, err
			}
			newMap[k] = nv
		}
		return newMap, nil
	case []interface{}:
		out := make([]interface{}, 0, len(val))
		for _, it := range val {
			nv, err := t.normalize(it)
			if err != nil {
				return nil, err
			}
			out = append(out, nv)
		}
		return out, nil
	case string:
		return val, nil
	case int:
		return fmt.Sprintf("%d", val), nil
	case int64:
		return fmt.Sprintf("%d", val), nil
	case float64:
		return fmt.Sprintf("%v", val), nil
	case bool:
		return fmt.Sprintf("%v", val), nil
	default:
		// Convert other types to string
		return fmt.Sprintf("%v", val), nil
	}
}

// EIP-712 domain separator for Aster v3 API
// Domain: { name: "AsterSignTransaction", version: "1", chainId: 1666, verifyingContract: 0x0 }
var asterDomainSeparator []byte

func init() {
	domainTypeHash := crypto.Keccak256([]byte("EIP712Domain(string name,string version,uint256 chainId,address verifyingContract)"))
	nameHash := crypto.Keccak256([]byte("AsterSignTransaction"))
	versionHash := crypto.Keccak256([]byte("1"))
	chainID := common.LeftPadBytes(big.NewInt(1666).Bytes(), 32)
	contract := common.LeftPadBytes(common.Address{}.Bytes(), 32) // 0x0000...0000

	domainData := make([]byte, 0, 5*32)
	domainData = append(domainData, domainTypeHash...)   // 32 bytes (keccak256 output)
	domainData = append(domainData, nameHash...)          // 32 bytes
	domainData = append(domainData, versionHash...)       // 32 bytes
	domainData = append(domainData, chainID...)           // 32 bytes
	domainData = append(domainData, contract...)          // 32 bytes
	asterDomainSeparator = crypto.Keccak256(domainData)
}

// getServerTime fetches Aster server time to avoid clock skew issues.
// Falls back to local time if the request fails.
func (t *AsterTrader) getServerTime() int64 {
	resp, err := t.client.Get(t.baseURL + "/fapi/v3/time")
	if err != nil {
		return time.Now().UnixMilli()
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return time.Now().UnixMilli()
	}
	var result struct {
		ServerTime int64 `json:"serverTime"`
	}
	if err := json.Unmarshal(body, &result); err != nil || result.ServerTime == 0 {
		return time.Now().UnixMilli()
	}
	return result.ServerTime
}

// sign signs request parameters using EIP-712 TypedData (Aster v3 API)
// Reference: https://github.com/AsterDEX/api-docs/blob/master/aster-finance-futures-api-v3.md
func (t *AsterTrader) sign(params map[string]interface{}, nonce uint64) error {
	// Use server time to avoid clock skew (Aster rejects nonce >5s from server time)
	serverTimeMs := t.getServerTime()
	serverTimeMicro := serverTimeMs * 1000

	// Add standard fields
	params["recvWindow"] = "50000"
	params["timestamp"] = strconv.FormatInt(serverTimeMs, 10)
	params["user"] = t.user
	params["signer"] = t.signer
	params["nonce"] = fmt.Sprintf("%d", serverTimeMicro)

	// Build URL-encoded parameter string (sorted by key for deterministic output)
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	q := url.Values{}
	for _, k := range keys {
		q.Set(k, fmt.Sprintf("%v", params[k]))
	}
	msg := q.Encode()

	// EIP-712 struct hash: keccak256(typeHash || keccak256(msg))
	messageTypeHash := crypto.Keccak256([]byte("Message(string msg)"))
	msgHash := crypto.Keccak256([]byte(msg))

	structData := make([]byte, 0, 2*32)
	structData = append(structData, messageTypeHash...)
	structData = append(structData, msgHash...)
	structHash := crypto.Keccak256(structData)

	// EIP-712 digest: \x19\x01 || domainSeparator || structHash
	digest := make([]byte, 0, 2+32+32)
	digest = append(digest, 0x19, 0x01)
	digest = append(digest, asterDomainSeparator...)
	digest = append(digest, structHash...)
	hash := crypto.Keccak256(digest)

	// ECDSA signature
	sig, err := crypto.Sign(hash, t.privateKey)
	if err != nil {
		return fmt.Errorf("signature failed: %w", err)
	}

	if len(sig) != 65 {
		return fmt.Errorf("signature length abnormal: %d", len(sig))
	}
	sig[64] += 27

	params["signature"] = "0x" + hex.EncodeToString(sig)

	return nil
}

// request Send HTTP request (with retry mechanism)
func (t *AsterTrader) request(method, endpoint string, params map[string]interface{}) ([]byte, error) {
	const maxRetries = 3
	var lastErr error

	for attempt := 1; attempt <= maxRetries; attempt++ {
		// Generate new nonce and signature for each retry
		nonce := t.genNonce()
		paramsCopy := make(map[string]interface{})
		for k, v := range params {
			paramsCopy[k] = v
		}

		// Sign
		if err := t.sign(paramsCopy, nonce); err != nil {
			return nil, err
		}

		body, err := t.doRequest(method, endpoint, paramsCopy)
		if err == nil {
			return body, nil
		}

		lastErr = err

		// Retry if network timeout or temporary error
		if strings.Contains(err.Error(), "timeout") ||
			strings.Contains(err.Error(), "connection reset") ||
			strings.Contains(err.Error(), "EOF") {
			if attempt < maxRetries {
				waitTime := time.Duration(attempt) * time.Second
				time.Sleep(waitTime)
				continue
			}
		}

		// Don't retry other errors (like 400/401)
		return nil, err
	}

	return nil, fmt.Errorf("request failed (retried %d times): %w", maxRetries, lastErr)
}

// doRequest Execute actual HTTP request
func (t *AsterTrader) doRequest(method, endpoint string, params map[string]interface{}) ([]byte, error) {
	fullURL := t.baseURL + endpoint
	method = strings.ToUpper(method)

	switch method {
	case "POST":
		// POST request: parameters in form body
		form := url.Values{}
		for k, v := range params {
			form.Set(k, fmt.Sprintf("%v", v))
		}
		req, err := http.NewRequest("POST", fullURL, strings.NewReader(form.Encode()))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

		resp, err := t.client.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()

		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
		}
		return body, nil

	case "GET", "DELETE":
		// GET/DELETE request: parameters in querystring
		q := url.Values{}
		for k, v := range params {
			q.Set(k, fmt.Sprintf("%v", v))
		}
		u, _ := url.Parse(fullURL)
		u.RawQuery = q.Encode()

		req, err := http.NewRequest(method, u.String(), nil)
		if err != nil {
			return nil, err
		}

		resp, err := t.client.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()

		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
		}
		return body, nil

	default:
		return nil, fmt.Errorf("unsupported HTTP method: %s", method)
	}
}
