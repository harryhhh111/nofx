package api

import (
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"nofx/logger"
	"nofx/provider/nofxos"
)

// handleAI500Coins returns AI500 top-rated coin list.
// Query params: limit (int, optional, default 20, max 100)
func (s *Server) handleAI500Coins(c *gin.Context) {
	limit := 20
	if limitStr := c.Query("limit"); limitStr != "" {
		if parsed, err := strconv.Atoi(limitStr); err == nil && parsed > 0 {
			limit = parsed
			if limit > 100 {
				limit = 100
			}
		}
	}

	if s.nofxosClient == nil {
		SafeInternalError(c, "AI500 data client not initialized", nil)
		return
	}
	if strings.TrimSpace(s.nofxosClient.GetAuthKey()) == "" && s.nofxosClient.GetClaw402() == nil {
		SafeErrorWithDetails(
			c,
			http.StatusBadRequest,
			"NofxOS API key is required. Configure NOFXOS_API_KEY or use a supported data gateway.",
			"nofxos.api_key_required",
			nil,
			nil,
		)
		return
	}

	coins, err := s.nofxosClient.GetAI500List()
	if err != nil {
		logger.Errorf("[AI500] Failed to fetch coin list: %v", err)
		SafeInternalError(c, "Failed to fetch AI500 data", err)
		return
	}

	if len(coins) > limit {
		coins = coins[:limit]
	}

	c.JSON(http.StatusOK, gin.H{
		"coins": coins,
		"count": len(coins),
	})
}

// initNofxosClient creates the nofxos client with claw402 backend payment.
func initNofxosClient() *nofxos.Client {
	client := nofxos.NewClient(nofxos.DefaultBaseURL, strings.TrimSpace(os.Getenv("NOFXOS_API_KEY")))

	walletKey := os.Getenv("CLAW402_WALLET_KEY")
	if walletKey != "" {
		claw402URL := os.Getenv("CLAW402_URL")
		if claw402URL == "" {
			claw402URL = "https://claw402.ai"
		}
		claw402Client, err := nofxos.NewClaw402DataClient(claw402URL, walletKey, &logger.MCPLogger{})
		if err == nil {
			client.SetClaw402(claw402Client)
			logger.Infof("🔗 AI500 API routed through claw402 (%s)", claw402URL)
		} else {
			logger.Warnf("⚠️ Failed to init claw402 data client: %v (using direct nofxos.ai)", err)
		}
	}

	return client
}
