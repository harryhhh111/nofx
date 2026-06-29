// Package supplyrefresher keeps the local coin_supply table up to date by
// fetching one page of CoinGecko market data per minute. This avoids hitting
// CoinGecko's strict free-tier rate limits during trading.
package supplyrefresher

import (
	"context"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"nofx/logger"
	"nofx/provider/coingecko"
	"nofx/store"
)

// Config controls the refresher behavior.
type Config struct {
	PageSize       int
	SleepInterval  time.Duration
	MaxPagesPerRun int
	Order          string // "market_cap_asc" or "market_cap_desc"
}

// DefaultConfig returns the recommended refresher config.
func DefaultConfig() Config {
	return Config{
		PageSize:       envInt("SUPPLY_REFRESH_PAGE_SIZE", 250),
		SleepInterval:  envDuration("SUPPLY_REFRESH_INTERVAL", 60*time.Second),
		MaxPagesPerRun: envInt("SUPPLY_REFRESH_MAX_PAGES_PER_RUN", 10),
		Order:          envString("SUPPLY_REFRESH_ORDER", "market_cap_asc"),
	}
}

// Refresher runs a background goroutine that periodically fetches CoinGecko
// market data and upserts it into the local coin_supply table.
type Refresher struct {
	store  *store.CoinSupplyStore
	client *coingecko.Client
	config Config

	stop   chan struct{}
	done   chan struct{}
	mu     sync.Mutex
	page   int
	status Status
}

// Status exposes basic runtime state.
type Status struct {
	Running   bool
	LastPage  int
	LastRows  int
	LastError string
	LastRunAt time.Time
}

// New creates a new Refresher. If apiKey is empty it uses the free CoinGecko client.
func New(s *store.CoinSupplyStore, apiKey string, cfg Config) *Refresher {
	var client *coingecko.Client
	if apiKey != "" {
		client = coingecko.NewProClient(apiKey)
	} else {
		client = coingecko.NewClient()
	}
	return &Refresher{
		store:  s,
		client: client,
		config: cfg,
		stop:   make(chan struct{}),
		done:   make(chan struct{}),
		page:   1,
	}
}

// Start begins the background refresh loop.
func (r *Refresher) Start() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.status.Running {
		return nil
	}
	r.status.Running = true

	go r.loop()
	logger.Infof("🔄 coin supply refresher started (page_size=%d interval=%s max_pages=%d order=%s)",
		r.config.PageSize, r.config.SleepInterval, r.config.MaxPagesPerRun, r.config.Order)
	return nil
}

// Stop signals the background loop to exit and waits for it to finish.
func (r *Refresher) Stop() {
	close(r.stop)
	<-r.done
	r.mu.Lock()
	r.status.Running = false
	r.mu.Unlock()
	logger.Infof("🛑 coin supply refresher stopped")
}

// Status returns a snapshot of the current runtime state.
func (r *Refresher) Status() Status {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.status
}

func (r *Refresher) loop() {
	defer close(r.done)

	// Run an initial refresh immediately on startup.
	r.tick()

	ticker := time.NewTicker(r.config.SleepInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			r.tick()
		case <-r.stop:
			return
		}
	}
}

func (r *Refresher) tick() {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	r.mu.Lock()
	page := r.page
	r.mu.Unlock()

	data, err := r.client.CoinsMarkets(ctx, coingecko.CoinsMarketsRequest{
		VSCCurrency: "usd",
		Order:       r.config.Order,
		PerPage:     r.config.PageSize,
		Page:        page,
	})
	if err != nil {
		r.setStatus(page, 0, err.Error())
		logger.Warnf("supply refresher page %d failed: %v", page, err)
		return
	}

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

	if len(records) > 0 {
		if err := r.store.Upsert(records); err != nil {
			r.setStatus(page, 0, err.Error())
			logger.Warnf("supply refresher upsert page %d failed: %v", page, err)
			return
		}
	}

	// Advance page, resetting after MaxPagesPerRun.
	nextPage := page + 1
	if nextPage > r.config.MaxPagesPerRun || len(data) == 0 {
		nextPage = 1
	}

	r.setPage(nextPage)
	r.setStatus(page, len(records), "")
	logger.Infof("supply refresher page %d: rows=%d next_page=%d", page, len(records), nextPage)
}

func (r *Refresher) setPage(page int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.page = page
}

func (r *Refresher) setStatus(page, rows int, errStr string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.status.LastPage = page
	r.status.LastRows = rows
	r.status.LastError = errStr
	r.status.LastRunAt = time.Now().UTC()
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
