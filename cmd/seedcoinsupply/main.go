// cmd/seedcoinsupply seeds the local coin_supply table from CoinGecko.
// It fetches one page per minute to stay within the free-tier rate limit.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"

	"nofx/provider/coingecko"
	"nofx/store"
)

const (
	defaultPageSize = 250
	defaultSleep    = 60 * time.Second
	defaultMaxPages = 10
)

func main() {
	if err := run(); err != nil {
		log.Fatalf("seed failed: %v", err)
	}
}

func run() error {
	ctx := context.Background()

	db, err := openGORM()
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}

	supplyStore := store.NewCoinSupplyStore(db)
	if err := supplyStore.InitTables(); err != nil {
		return fmt.Errorf("ensure table: %w", err)
	}

	apiKey := os.Getenv("COINGECKO_API_KEY")
	var client *coingecko.Client
	if apiKey != "" {
		client = coingecko.NewProClient(apiKey)
		log.Printf("using CoinGecko API key (%s)", keyType(apiKey))
	} else {
		client = coingecko.NewClient()
		log.Println("using CoinGecko free API (1 request per minute)")
	}

	maxPages := envInt("SEED_MAX_PAGES", defaultMaxPages)
	pageSize := envInt("SEED_PAGE_SIZE", defaultPageSize)
	sleep := envDuration("SEED_SLEEP", defaultSleep)
	order := envString("SEED_ORDER", "market_cap_asc")

	log.Printf("starting seed: page_size=%d sleep=%s max_pages=%d order=%s", pageSize, sleep, maxPages, order)

	totalRows := 0
	for page := 1; page <= maxPages; page++ {
		log.Printf("fetching page %d...", page)
		data, err := fetchWithRetry(ctx, client, page, pageSize, order)
		if err != nil {
			log.Printf("page %d failed: %v", page, err)
			if totalRows == 0 {
				return err
			}
			break
		}
		if len(data) == 0 {
			log.Printf("page %d empty, done", page)
			break
		}

		records := toRecords(data)
		if len(records) > 0 {
			if err := supplyStore.Upsert(records); err != nil {
				return fmt.Errorf("upsert page %d: %w", page, err)
			}
		}
		totalRows += len(records)
		log.Printf("page %d: rows=%d total_rows=%d", page, len(records), totalRows)

		if page < maxPages {
			log.Printf("sleeping %s before next page...", sleep)
			time.Sleep(sleep)
		}
	}

	count, err := supplyStore.Count()
	if err != nil {
		log.Printf("failed to count: %v", err)
	} else {
		log.Printf("coin_supply table now has %d rows", count)
	}

	log.Printf("seed complete: total_rows=%d", totalRows)
	return nil
}

func fetchWithRetry(ctx context.Context, client *coingecko.Client, page, pageSize int, order string) ([]coingecko.MarketData, error) {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(attempt) * 60 * time.Second
			log.Printf("retry page %d attempt %d after %s", page, attempt, backoff)
			time.Sleep(backoff)
		}
		data, err := client.CoinsMarkets(ctx, coingecko.CoinsMarketsRequest{
			VSCCurrency: "usd",
			Order:       order,
			PerPage:     pageSize,
			Page:        page,
		})
		if err == nil {
			return data, nil
		}
		lastErr = err
		log.Printf("page %d attempt %d error: %v", page, attempt, err)
	}
	return nil, lastErr
}

func toRecords(data []coingecko.MarketData) []store.CoinSupply {
	records := make([]store.CoinSupply, 0, len(data))
	seen := make(map[string]struct{}, len(data))
	for _, d := range data {
		symbol := normalizeSymbol(d.Symbol)
		if symbol == "" {
			continue
		}
		if _, ok := seen[symbol]; ok {
			continue
		}
		seen[symbol] = struct{}{}
		records = append(records, store.CoinSupply{
			Symbol:            symbol,
			CoingeckoID:       d.ID,
			Name:              d.Name,
			CirculatingSupply: d.CirculatingSupply,
			TotalSupply:       d.TotalSupply,
			MarketCapUSD:      d.MarketCap,
			Volume24hUSD:      d.TotalVolume,
			LastUpdatedAt:     time.Now().UTC(),
			Source:            "coingecko",
		})
	}
	return records
}

func normalizeSymbol(symbol string) string {
	s := strings.ToUpper(strings.TrimSpace(symbol))
	if s == "" {
		return ""
	}
	s = strings.ReplaceAll(s, "-", "")
	s = strings.ReplaceAll(s, " ", "")
	if !strings.HasSuffix(s, "USDT") && !strings.HasSuffix(s, "USDC") && !strings.HasSuffix(s, "USD") {
		s += "USDT"
	}
	return s
}

func openGORM() (*gorm.DB, error) {
	host := envString("DB_HOST", "172.17.0.1")
	port := envInt("DB_PORT", 5432)
	user := envString("DB_USER", "nfp")
	pass := envString("DB_PASSWORD", "123456")
	dbName := envString("DB_NAME", "nofx-v2")
	sslMode := envString("DB_SSLMODE", "disable")
	return store.InitGormPostgres(host, port, user, pass, dbName, sslMode)
}

func keyType(key string) string {
	if strings.HasPrefix(key, "CG-") {
		return "demo"
	}
	return "pro"
}

func envInt(name string, def int) int {
	v := os.Getenv(name)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

func envDuration(name string, def time.Duration) time.Duration {
	v := os.Getenv(name)
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return def
	}
	return d
}

func envString(name, def string) string {
	v := os.Getenv(name)
	if v == "" {
		return def
	}
	return v
}
