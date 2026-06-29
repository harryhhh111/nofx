// cmd/seedcoinsupply seeds the local coin_supply table from CoinGecko.
// It fetches one page per minute to stay within the free-tier rate limit.
package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	_ "github.com/lib/pq"
	"nofx/provider/coingecko"
)

const (
	defaultPageSize = 250
	defaultSleep    = 60 * time.Second
	defaultMaxPages = 100
)

func main() {
	if err := run(); err != nil {
		log.Fatalf("seed failed: %v", err)
	}
}

func run() error {
	ctx := context.Background()

	apiKey := os.Getenv("COINGECKO_API_KEY")
	var client *coingecko.Client
	if apiKey != "" {
		client = coingecko.NewProClient(apiKey)
		log.Printf("using CoinGecko API key (%s)", keyType(apiKey))
	} else {
		client = coingecko.NewClient()
		log.Println("using CoinGecko free API (1 request per minute)")
	}

	db, err := openDB()
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer db.Close()

	if err := ensureTable(db); err != nil {
		return fmt.Errorf("ensure table: %w", err)
	}

	maxPages := envInt("SEED_MAX_PAGES", defaultMaxPages)
	pageSize := envInt("SEED_PAGE_SIZE", defaultPageSize)
	sleep := envDuration("SEED_SLEEP", defaultSleep)

	log.Printf("starting seed: page_size=%d sleep=%s max_pages=%d", pageSize, sleep, maxPages)

	totalInserted := 0
	totalUpdated := 0
	for page := 1; page <= maxPages; page++ {
		log.Printf("fetching page %d...", page)
		data, err := fetchWithRetry(ctx, client, page, pageSize)
		if err != nil {
			log.Printf("page %d failed: %v", page, err)
			// If we got some data already, continue to next page; otherwise stop.
			if totalInserted == 0 && totalUpdated == 0 {
				return err
			}
			break
		}
		if len(data) == 0 {
			log.Printf("page %d empty, done", page)
			break
		}

		inserted, updated, err := upsertPage(db, data)
		if err != nil {
			return fmt.Errorf("upsert page %d: %w", page, err)
		}
		totalInserted += inserted
		totalUpdated += updated
		log.Printf("page %d: inserted=%d updated=%d total_rows=%d", page, inserted, updated, totalInserted+totalUpdated)

		if page < maxPages && len(data) == pageSize {
			log.Printf("sleeping %s before next page...", sleep)
			time.Sleep(sleep)
		}
	}

	log.Printf("seed complete: inserted=%d updated=%d", totalInserted, totalUpdated)
	return nil
}

func fetchWithRetry(ctx context.Context, client *coingecko.Client, page, pageSize int) ([]coingecko.MarketData, error) {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(attempt) * 60 * time.Second
			log.Printf("retry page %d attempt %d after %s", page, attempt, backoff)
			time.Sleep(backoff)
		}
		data, err := client.CoinsMarkets(ctx, coingecko.CoinsMarketsRequest{
			VSCCurrency: "usd",
			Order:       "market_cap_asc",
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

func upsertPage(db *sql.DB, data []coingecko.MarketData) (int, int, error) {
	inserted := 0
	updated := 0
	for _, d := range data {
		symbol := normalizeSymbol(d.Symbol)
		if symbol == "" {
			continue
		}
		res, err := db.Exec(`
			INSERT INTO coin_supply (symbol, coingecko_id, name, circulating_supply, total_supply, market_cap_usd, volume_24h_usd, last_updated_at, source)
			VALUES ($1, $2, $3, $4, $5, $6, $7, NOW(), 'coingecko')
			ON CONFLICT (symbol) DO UPDATE SET
				coingecko_id = EXCLUDED.coingecko_id,
				name = EXCLUDED.name,
				circulating_supply = EXCLUDED.circulating_supply,
				total_supply = EXCLUDED.total_supply,
				market_cap_usd = EXCLUDED.market_cap_usd,
				volume_24h_usd = EXCLUDED.volume_24h_usd,
				last_updated_at = NOW(),
				source = 'coingecko'
		`, symbol, d.ID, d.Name, nullIfZero(d.CirculatingSupply), nullIfZero(d.TotalSupply), nullIfZero(d.MarketCap), nullIfZero(d.TotalVolume))
		if err != nil {
			return 0, 0, err
		}
		rows, _ := res.RowsAffected()
		if rows == 1 {
			inserted++
		} else {
			updated++
		}
	}
	return inserted, updated, nil
}

func normalizeSymbol(symbol string) string {
	s := strings.ToUpper(strings.TrimSpace(symbol))
	if s == "" {
		return ""
	}
	if !strings.HasSuffix(s, "USDT") {
		s += "USDT"
	}
	return s
}

func nullIfZero(v float64) interface{} {
	if v == 0 {
		return nil
	}
	return v
}

func openDB() (*sql.DB, error) {
	connStr := os.Getenv("DATABASE_URL")
	if connStr == "" {
		host := os.Getenv("DB_HOST")
		port := os.Getenv("DB_PORT")
		user := os.Getenv("DB_USER")
		pass := os.Getenv("DB_PASSWORD")
		dbName := os.Getenv("DB_NAME")
		sslMode := os.Getenv("DB_SSLMODE")
		if host == "" {
			host = "172.17.0.1"
		}
		if port == "" {
			port = "5432"
		}
		if user == "" {
			user = "nfp"
		}
		if pass == "" {
			pass = "123456"
		}
		if dbName == "" {
			dbName = "nofx-v2"
		}
		if sslMode == "" {
			sslMode = "disable"
		}
		connStr = fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=%s", host, port, user, pass, dbName, sslMode)
	}
	return sql.Open("postgres", connStr)
}

func ensureTable(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS coin_supply (
			symbol TEXT PRIMARY KEY,
			coingecko_id TEXT,
			name TEXT,
			circulating_supply NUMERIC,
			total_supply NUMERIC,
			max_supply NUMERIC,
			market_cap_usd NUMERIC,
			volume_24h_usd NUMERIC,
			last_updated_at TIMESTAMPTZ DEFAULT NOW(),
			source TEXT DEFAULT 'coingecko'
		)
	`)
	return err
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
