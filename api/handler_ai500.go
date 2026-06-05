package api

import (
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"nofx/auth"
	"nofx/kernel"
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

	client := s.nofxosClient
	if userID := s.getOptionalAuthenticatedUserID(c); userID != "" {
		if userWalletKey := s.getClaw402WalletKey(userID); userWalletKey != "" {
			userClient := nofxos.NewClient(nofxos.DefaultBaseURL, "")
			if claw402Client := newClaw402DataClient(userWalletKey); claw402Client != nil {
				userClient.SetClaw402(claw402Client)
				client = userClient
			}
		}
	}

	if client == nil {
		SafeInternalError(c, "AI500 data client not initialized", nil)
		return
	}
	if client.GetClaw402() == nil {
		SafeErrorWithDetails(
			c,
			http.StatusBadRequest,
			"Claw402 wallet is required for AI500 data. Configure a Claw402 wallet in Settings > Model Config or set CLAW402_WALLET_KEY for public data preview.",
			"nofxos.claw402_wallet_required",
			nil,
			nil,
		)
		return
	}

	coins, err := client.GetAI500List()
	if err != nil {
		logger.Errorf("[AI500] Failed to fetch coin list: %v", err)
		SafeErrorWithDetails(
			c,
			http.StatusBadGateway,
			kernel.DescribeExternalDataError(err),
			"nofxos.claw402_data_failed",
			nil,
			err,
		)
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
	client := nofxos.NewClient(nofxos.DefaultBaseURL, "")

	walletKey := os.Getenv("CLAW402_WALLET_KEY")
	if walletKey != "" {
		if claw402Client := newClaw402DataClient(walletKey); claw402Client != nil {
			client.SetClaw402(claw402Client)
		}
	}

	return client
}

func (s *Server) getOptionalAuthenticatedUserID(c *gin.Context) string {
	authHeader := c.GetHeader("Authorization")
	if authHeader == "" {
		return ""
	}
	tokenParts := strings.Split(authHeader, " ")
	if len(tokenParts) != 2 || tokenParts[0] != "Bearer" || auth.IsTokenBlacklisted(tokenParts[1]) {
		return ""
	}
	claims, err := auth.ValidateJWT(tokenParts[1])
	if err != nil || claims == nil {
		return ""
	}
	if s.store == nil {
		return claims.UserID
	}
	if _, err := s.store.User().GetByID(claims.UserID); err != nil {
		return ""
	}
	return claims.UserID
}

func newClaw402DataClient(walletKey string) *nofxos.Claw402DataClient {
	claw402URL := os.Getenv("CLAW402_URL")
	if claw402URL == "" {
		claw402URL = "https://claw402.ai"
	}
	claw402Client, err := nofxos.NewClaw402DataClient(claw402URL, walletKey, &logger.MCPLogger{})
	if err != nil {
		logger.Warnf("Failed to init claw402 data client: %v", err)
		return nil
	}
	logger.Infof("NofxOS data routed through claw402 (%s)", claw402URL)
	return claw402Client
}
