package smallcap

import (
	"os"
	"sync"

	"nofx/provider/coingecko"
)

var (
	sharedProvider     SmallMarketValueProvider
	sharedProviderOnce sync.Once
)

// SharedProvider returns the process-wide SmallMarketValueProvider singleton.
//
// Using a singleton ensures that all traders and the preview API share the same
// cache, so the number of upstream requests does not scale with the number of
// active traders. CoinAnk is used when COINANK_API_KEY is configured; otherwise
// the free CoinGecko + Binance OI provider is used.
func SharedProvider() SmallMarketValueProvider {
	sharedProviderOnce.Do(func() {
		if sharedProvider == nil {
			sharedProvider = initSharedProvider()
		}
	})
	return sharedProvider
}

func initSharedProvider() SmallMarketValueProvider {
	if apiKey := os.Getenv("COINANK_API_KEY"); apiKey != "" {
		return NewCoinAnkProvider(apiKey)
	}
	if apiKey := os.Getenv("COINGECKO_API_KEY"); apiKey != "" {
		return NewCoinGeckoProviderWithClient(coingecko.NewProClient(apiKey))
	}
	return NewCoinGeckoProvider()
}

// SetSharedProvider overrides the shared provider. Useful for tests.
func SetSharedProvider(p SmallMarketValueProvider) {
	sharedProvider = p
	sharedProviderOnce = sync.Once{}
}
