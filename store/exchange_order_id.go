package store

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// FormatExchangeOrderID normalizes an order id from JSON / API values for comparison.
// Empty / placeholder values like 0 should not participate in attribution matching.
func FormatExchangeOrderID(v interface{}) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		s := strings.TrimSpace(x)
		if s == "" || s == "0" || s == "0.0" {
			return ""
		}
		return s
	case int:
		if x == 0 {
			return ""
		}
		return strconv.Itoa(x)
	case int32:
		if x == 0 {
			return ""
		}
		return strconv.FormatInt(int64(x), 10)
	case int64:
		if x == 0 {
			return ""
		}
		return strconv.FormatInt(x, 10)
	case float64:
		if math.IsNaN(x) || math.IsInf(x, 0) || x == 0 {
			return ""
		}
		if x == math.Trunc(x) {
			return strconv.FormatInt(int64(x), 10)
		}
		return strings.TrimSpace(fmt.Sprintf("%v", x))
	case float32:
		xf := float64(x)
		if math.IsNaN(xf) || math.IsInf(xf, 0) || xf == 0 {
			return ""
		}
		if xf == math.Trunc(xf) {
			return strconv.FormatInt(int64(xf), 10)
		}
		return strings.TrimSpace(fmt.Sprintf("%v", x))
	default:
		s := strings.TrimSpace(fmt.Sprint(x))
		if s == "" || s == "0" || s == "0.0" {
			return ""
		}
		return s
	}
}

// FormatExchangeOrderIDFromMap reads common keys from exchange order / result maps.
func FormatExchangeOrderIDFromMap(order map[string]interface{}) string {
	if order == nil {
		return ""
	}
	for _, k := range []string{"orderId", "order_id", "ordId", "ord_id", "orderID"} {
		if v, ok := order[k]; ok {
			return FormatExchangeOrderID(v)
		}
	}
	return ""
}
