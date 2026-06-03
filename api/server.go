package api

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"nofx/auth"
	"nofx/crypto"
	"nofx/logger"
	"nofx/manager"
	"nofx/provider/nofxos"
	"nofx/store"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// Server HTTP API server
type Server struct {
	router                    *gin.Engine
	traderManager             *manager.TraderManager
	store                     *store.Store
	cryptoHandler             *CryptoHandler
	exchangeAccountStateCache *ExchangeAccountStateCache
	nofxosClient              *nofxos.Client
	httpServer                *http.Server
	port                      int
	telegramReloadCh          chan<- struct{} // signal Telegram bot to reload
}

// NewServer Creates API server
func NewServer(traderManager *manager.TraderManager, st *store.Store, cryptoService *crypto.CryptoService, port int) *Server {
	// Set to Release mode (reduce log output)
	gin.SetMode(gin.ReleaseMode)

	router := gin.Default()

	// Enable CORS
	router.Use(corsMiddleware())

	// Create crypto handler
	cryptoHandler := NewCryptoHandler(cryptoService)

	s := &Server{
		router:                    router,
		traderManager:             traderManager,
		store:                     st,
		cryptoHandler:             cryptoHandler,
		exchangeAccountStateCache: NewExchangeAccountStateCache(),
		nofxosClient:              initNofxosClient(),
		port:                      port,
	}

	// Setup routes
	s.setupRoutes()

	return s
}

// corsMiddleware CORS middleware
func corsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(http.StatusOK)
			return
		}

		c.Next()
	}
}

// setupRoutes Setup routes
func (s *Server) setupRoutes() {
	// API route group
	api := s.router.Group("/api")
	{
		// Health check
		api.Any("/health", s.handleHealth)

		// Admin login (used in admin mode, public)

		// System supported models and exchanges (no authentication required)
		s.route(api, "GET", "/supported-models", s.handleGetSupportedModels)
		s.route(api, "GET", "/supported-exchanges", s.handleGetSupportedExchanges)

		// System config (no authentication required, for frontend to determine admin mode/registration status)
		s.route(api, "GET", "/config", s.handleGetSystemConfig)

		// Wallet validation (no authentication required; used by frontend config form)
		api.POST("/wallet/validate", s.handleWalletValidate)
		api.POST("/wallet/generate", s.handleWalletGenerate)

		// Crypto related endpoints (no authentication required, not exposed to bot)
		api.GET("/crypto/config", s.cryptoHandler.HandleGetCryptoConfig)
		api.GET("/crypto/public-key", s.cryptoHandler.HandleGetPublicKey)
		api.POST("/crypto/decrypt", s.cryptoHandler.HandleDecryptSensitiveData)

		// Public competition data (no authentication required)
		s.route(api, "GET", "/traders", s.handlePublicTraderList)
		s.route(api, "GET", "/competition", s.handlePublicCompetition)
		s.route(api, "GET", "/top-traders", s.handleTopTraders)
		s.route(api, "GET", "/equity-history", s.handleEquityHistory)
		s.route(api, "POST", "/equity-history-batch", s.handleEquityHistoryBatch)
		s.route(api, "GET", "/traders/:id/public-config", s.handleGetPublicTraderConfig)

		// Market data (no authentication required)
		s.route(api, "GET", "/klines", s.handleKlines)
		s.route(api, "GET", "/symbols", s.handleSymbols)
		s.route(api, "GET", "/ai500/coins", s.handleAI500Coins)
		s.route(api, "GET", "/nofxos/status", s.handleNofxosStatus)

		// Public strategy market (no authentication required)
		s.route(api, "GET", "/strategies/public", s.handlePublicStrategies)
		s.route(api, "POST", "/strategies/estimate-tokens", s.handleEstimateTokens)

		// Decision digest (lightweight, no authentication required)
		decisions := api.Group("/decisions")
		{
			decisions.GET("/digest", s.handleGetDecisionDigest)
			decisions.GET("/digest/list", s.handleGetDecisionDigestList)
		}

		// Authentication related routes (no authentication required)
		s.route(api, "POST", "/register", s.handleRegister)
		s.route(api, "POST", "/login", s.handleLogin)
		s.route(api, "POST", "/reset-password", s.handleResetPassword)

		// Routes requiring authentication
		protected := api.Group("/", s.authMiddleware())
		{
			// Logout (add to blacklist)
			s.route(protected, "POST", "/logout", s.handleLogout)
			s.route(protected, "POST", "/onboarding/beginner", s.handleBeginnerOnboarding)
			s.route(protected, "GET", "/onboarding/beginner/current", s.handleCurrentBeginnerWallet)

			// User account management
			s.route(protected, "PUT", "/user/password",
				s.handleChangePassword)

			// Server IP query (requires authentication, for whitelist configuration)
			s.route(protected, "GET", "/server-ip", s.handleGetServerIP)

			// AI trader management
			s.route(protected, "GET", "/my-traders",
				s.handleTraderList)
			s.route(protected, "GET", "/traders/:id/config",
				s.handleGetTraderConfig)
			s.route(protected, "POST", "/traders",
				s.handleCreateTrader)
			s.route(protected, "PUT", "/traders/:id",
				s.handleUpdateTrader)
			s.route(protected, "DELETE", "/traders/:id",
				s.handleDeleteTrader)
			s.route(protected, "POST", "/traders/:id/start",
				s.handleStartTrader)
			s.route(protected, "POST", "/traders/:id/stop",
				s.handleStopTrader)
			s.route(protected, "POST", "/traders/:id/sync-balance",
				s.handleSyncBalance)
			s.route(protected, "POST", "/traders/:id/close-position",
				s.handleClosePosition)
			s.route(protected, "PUT", "/traders/:id/competition",
				s.handleToggleCompetition)
			s.route(protected, "GET", "/traders/:id/grid-risk",
				s.handleGetGridRiskInfo)

			// AI cost tracking
			s.route(protected, "GET", "/ai-costs", s.handleGetAICosts)
			s.route(protected, "GET", "/ai-costs/summary", s.handleGetAICostsSummary)

			// AI model configuration
			s.route(protected, "GET", "/models",
				s.handleGetModelConfigs)
			s.route(protected, "PUT", "/models",
				s.handleUpdateModelConfigs)

			// Exchange configuration
			s.route(protected, "GET", "/exchanges",
				s.handleGetExchangeConfigs)
			s.route(protected, "GET", "/exchanges/account-state",
				s.handleGetExchangeAccountStates)
			s.route(protected, "POST", "/exchanges",
				s.handleCreateExchange)
			s.route(protected, "PUT", "/exchanges",
				s.handleUpdateExchangeConfigs)
			s.route(protected, "DELETE", "/exchanges/:id",
				s.handleDeleteExchange)

			// Telegram bot configuration
			s.route(protected, "GET", "/telegram",
				s.handleGetTelegramConfig)
			s.route(protected, "POST", "/telegram",
				s.handleUpdateTelegramConfig)
			s.route(protected, "POST", "/telegram/model",
				s.handleUpdateTelegramModel)
			s.route(protected, "DELETE", "/telegram/binding",
				s.handleUnbindTelegram)

			// Strategy management
			s.route(protected, "GET", "/strategies",
				s.handleGetStrategies)
			s.route(protected, "GET", "/strategies/active",
				s.handleGetActiveStrategy)
			s.route(protected, "GET", "/strategies/default-config",
				s.handleGetDefaultStrategyConfig)
			s.route(protected, "POST", "/strategies/preview-prompt",
				s.handlePreviewPrompt)
			s.route(protected, "POST", "/strategies/preview-flow", s.handlePreviewPrompt)
			s.route(protected, "POST", "/strategies/compile",
				s.handleCompileStrategyPrompt)
			s.route(protected, "POST", "/strategies/test-run", s.handleStrategyTestRun)
			s.route(protected, "POST", "/strategies/:id/evolve",
				s.handleEvolveStrategy)
			s.route(protected, "GET", "/strategies/:id", s.handleGetStrategy)
			s.route(protected, "POST", "/strategies",
				s.handleCreateStrategy)
			s.route(protected, "PUT", "/strategies/:id",
				s.handleUpdateStrategy)
			s.route(protected, "DELETE", "/strategies/:id",
				s.handleDeleteStrategy)
			s.route(protected, "POST", "/strategies/:id/activate",
				s.handleActivateStrategy)
			s.route(protected, "POST", "/strategies/:id/duplicate",
				s.handleDuplicateStrategy)

			// Data for specified trader (using query parameter ?trader_id=xxx)
			s.route(protected, "GET", "/status",
				s.handleStatus)
			s.route(protected, "GET", "/account",
				s.handleAccount)
			s.route(protected, "GET", "/positions",
				s.handlePositions)
			s.route(protected, "GET", "/positions/history",
				s.handlePositionHistory)
			s.route(protected, "GET", "/trades",
				s.handleTrades)
			s.route(protected, "GET", "/orders",
				s.handleOrders)
			s.route(protected, "GET", "/orders/:id/fills",
				s.handleOrderFills)
			s.route(protected, "GET", "/open-orders",
				s.handleOpenOrders)
			s.route(protected, "GET", "/decisions",
				s.handleDecisions)
			s.route(protected, "GET", "/decisions/latest",
				s.handleLatestDecisions)
			s.route(protected, "GET", "/trade-memories",
				s.handleTradeMemories)
			s.route(protected, "GET", "/execution-analytics",
				s.handleExecutionAnalytics)
			s.route(protected, "GET", "/statistics",
				s.handleStatistics)
			s.route(protected, "GET", "/bbmacd/stats",
				s.handleBBMACDStats)
			s.route(protected, "GET", "/bbmacd/config",
				s.handleGetBBMACDConfig)
			s.route(protected, "PUT", "/bbmacd/config",
				s.handleUpdateBBMACDConfig)

		}
	}
}

// handleHealth Health check
func (s *Server) handleHealth(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status": "ok",
		"time":   c.Request.Context().Value("time"),
	})
}

// handleGetSystemConfig Get system configuration (configuration that client needs to know)
func (s *Server) handleGetSystemConfig(c *gin.Context) {
	userCount, _ := s.store.User().Count()
	c.JSON(http.StatusOK, gin.H{
		"initialized":      userCount > 0,
		"btc_eth_leverage": 10,
		"altcoin_leverage": 5,
	})
}

// handleGetServerIP Get server IP address (for whitelist configuration)
func (s *Server) handleGetServerIP(c *gin.Context) {
	// Try to get public IP via third-party API
	publicIP := getPublicIPFromAPI()

	// If third-party API fails, get first public IP from network interface
	if publicIP == "" {
		publicIP = getPublicIPFromInterface()
	}

	// If still cannot get it, return error
	if publicIP == "" {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Unable to get public IP address"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"public_ip": publicIP,
		"message":   "Please add this IP address to the whitelist",
	})
}

// getPublicIPFromAPI Get public IP via third-party API (IPv4 only)
func getPublicIPFromAPI() string {
	// Try multiple public IP query services (IPv4-only endpoints)
	services := []string{
		"https://api4.ipify.org?format=text", // IPv4 only
		"https://ipv4.icanhazip.com",         // IPv4 only
		"https://v4.ident.me",                // IPv4 only
		"https://api.ipify.org?format=text",  // May return IPv4 or IPv6
	}

	client := &http.Client{
		Timeout: 5 * time.Second,
	}

	for _, service := range services {
		resp, err := client.Get(service)
		if err != nil {
			continue
		}
		defer resp.Body.Close()

		if resp.StatusCode == http.StatusOK {
			body := make([]byte, 128)
			n, err := resp.Body.Read(body)
			if err != nil && err.Error() != "EOF" {
				continue
			}

			ip := strings.TrimSpace(string(body[:n]))
			parsedIP := net.ParseIP(ip)
			// Verify if it's a valid IPv4 address (not containing ":")
			if parsedIP != nil && parsedIP.To4() != nil {
				return ip
			}
		}
	}

	return ""
}

// getPublicIPFromInterface Get first public IP from network interface
func getPublicIPFromInterface() string {
	interfaces, err := net.Interfaces()
	if err != nil {
		return ""
	}

	for _, iface := range interfaces {
		// Skip disabled interfaces and loopback interfaces
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}

		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}

		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}

			if ip == nil || ip.IsLoopback() {
				continue
			}

			// Only consider IPv4 addresses
			if ip.To4() != nil {
				ipStr := ip.String()
				// Exclude private IP address ranges
				if !isPrivateIP(ip) {
					return ipStr
				}
			}
		}
	}

	return ""
}

// isPrivateIP Determine if it's a private IP address
func isPrivateIP(ip net.IP) bool {
	// Private IP address ranges:
	// 10.0.0.0/8
	// 172.16.0.0/12
	// 192.168.0.0/16
	privateRanges := []string{
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
	}

	for _, cidr := range privateRanges {
		_, subnet, _ := net.ParseCIDR(cidr)
		if subnet.Contains(ip) {
			return true
		}
	}

	return false
}

// getTraderFromQuery Get trader from query parameter
func (s *Server) getTraderFromQuery(c *gin.Context) (*manager.TraderManager, string, error) {
	userID := c.GetString("user_id")
	traderID := c.Query("trader_id")

	// Ensure user's traders are loaded into memory
	err := s.traderManager.LoadUserTradersFromStore(s.store, userID)
	if err != nil {
		logger.Infof("Failed to load traders for user %s: %v", userID, err)
	}

	if traderID == "" {
		// If no trader_id specified, return first trader for this user
		ids := s.traderManager.GetTraderIDs()
		if len(ids) == 0 {
			return nil, "", fmt.Errorf("No available traders")
		}

		// Get user's trader list, prioritize returning user's own traders
		userTraders, err := s.store.Trader().List(userID)
		if err == nil && len(userTraders) > 0 {
			traderID = userTraders[0].ID
		} else {
			traderID = ids[0]
		}
	}

	return s.traderManager, traderID, nil
}

// authMiddleware JWT authentication middleware
func (s *Server) authMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Missing Authorization header"})
			c.Abort()
			return
		}

		// Check Bearer token format
		tokenParts := strings.Split(authHeader, " ")
		if len(tokenParts) != 2 || tokenParts[0] != "Bearer" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid Authorization format"})
			c.Abort()
			return
		}

		tokenString := tokenParts[1]

		// Blacklist check
		if auth.IsTokenBlacklisted(tokenString) {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Token expired, please login again"})
			c.Abort()
			return
		}

		// Validate JWT token
		claims, err := auth.ValidateJWT(tokenString)
		if err != nil {
			logger.Errorf("[Auth] Invalid token: %v", err)
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid or expired token"})
			c.Abort()
			return
		}

		// Store user information in context
		c.Set("user_id", claims.UserID)
		c.Set("email", claims.Email)
		c.Next()
	}
}

// Start Start server
func (s *Server) Start() error {
	addr := fmt.Sprintf(":%d", s.port)
	logger.Infof("🌐 API server starting at http://localhost%s", addr)
	logger.Infof("📊 API Documentation:")
	logger.Infof("  • GET  /api/health           - Health check")
	logger.Infof("  • GET  /api/traders          - Public AI trader leaderboard top 50 (no auth required)")
	logger.Infof("  • GET  /api/competition      - Public competition data (no auth required)")
	logger.Infof("  • GET  /api/top-traders      - Top 5 trader data (no auth required, for performance comparison)")
	logger.Infof("  • GET  /api/equity-history?trader_id=xxx - Public return rate historical data (no auth required, for competition)")
	logger.Infof("  • GET  /api/equity-history-batch?trader_ids=a,b,c - Batch get historical data (no auth required, performance comparison optimization)")
	logger.Infof("  • GET  /api/traders/:id/public-config - Public trader config (no auth required, no sensitive info)")
	logger.Infof("  • POST /api/traders          - Create new AI trader")
	logger.Infof("  • DELETE /api/traders/:id    - Delete AI trader")
	logger.Infof("  • POST /api/traders/:id/start - Start AI trader")
	logger.Infof("  • POST /api/traders/:id/stop  - Stop AI trader")
	logger.Infof("  • GET  /api/models           - Get AI model config")
	logger.Infof("  • PUT  /api/models           - Update AI model config")
	logger.Infof("  • GET  /api/exchanges        - Get exchange config")
	logger.Infof("  • PUT  /api/exchanges        - Update exchange config")
	logger.Infof("  • GET  /api/status?trader_id=xxx     - Specified trader's system status")
	logger.Infof("  • GET  /api/account?trader_id=xxx    - Specified trader's account info")
	logger.Infof("  • GET  /api/positions?trader_id=xxx  - Specified trader's position list")
	logger.Infof("  • GET  /api/decisions?trader_id=xxx  - Specified trader's decision log")
	logger.Infof("  • GET  /api/decisions/latest?trader_id=xxx - Specified trader's latest decisions")
	logger.Infof("  • GET  /api/statistics?trader_id=xxx - Specified trader's statistics")
	logger.Infof("  • GET  /api/performance?trader_id=xxx - Specified trader's AI learning performance analysis")
	logger.Info()

	s.httpServer = &http.Server{
		Addr:    addr,
		Handler: s.router,
	}
	return s.httpServer.ListenAndServe()
}

// Shutdown Gracefully shutdown server
func (s *Server) Shutdown() error {
	if s.httpServer == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return s.httpServer.Shutdown(ctx)
}

// SetTelegramReloadCh sets the channel used to signal the Telegram bot to reload
func (s *Server) SetTelegramReloadCh(ch chan<- struct{}) {
	s.telegramReloadCh = ch
}

// handleGetDecisionDigest gets the latest decision digest for a specific trader.
func (s *Server) handleGetDecisionDigest(c *gin.Context) {
	traderID := c.Query("trader_id")
	if traderID == "" {
		SafeBadRequest(c, "trader_id is required")
		return
	}

	_, err := s.store.Trader().GetByID(traderID)
	if err != nil {
		SafeNotFound(c, "Trader")
		return
	}

	digest, err := s.store.Decision().GetLatestDigest(traderID)
	if err != nil {
		SafeNotFound(c, "Decision record")
		return
	}

	c.JSON(http.StatusOK, digest)
}

// handleGetDecisionDigestList gets a sorted list of latest decision digests for all traders.
func (s *Server) handleGetDecisionDigestList(c *gin.Context) {
	sortBy := c.DefaultQuery("sort_by", "timestamp")
	order := c.DefaultQuery("order", "desc")
	limit := 50
	if limitStr := c.Query("limit"); limitStr != "" {
		if parsedLimit, err := strconv.Atoi(limitStr); err == nil && parsedLimit > 0 {
			limit = parsedLimit
			if limit > 200 {
				limit = 200
			}
		}
	}

	validSortFields := map[string]bool{"timestamp": true, "trader_id": true, "success": true}
	if !validSortFields[sortBy] {
		sortBy = "timestamp"
	}
	if order != "asc" && order != "desc" {
		order = "desc"
	}

	traders, err := s.store.Trader().ListAll()
	if err != nil {
		SafeInternalError(c, "Get trader list", err)
		return
	}

	traderIDs := make([]string, len(traders))
	for i, t := range traders {
		traderIDs[i] = t.ID
	}

	digests, err := s.store.Decision().GetTradersLatestDigests(traderIDs, sortBy, order, limit)
	if err != nil {
		SafeInternalError(c, "Get decision digest list", err)
		return
	}

	c.JSON(http.StatusOK, digests)
}
