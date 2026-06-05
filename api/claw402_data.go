package api

import (
	"os"
	"strings"
)

func (s *Server) getClaw402WalletKey(userID string) string {
	if s.store != nil {
		model, err := s.store.AIModel().Get(userID, "claw402")
		if err == nil && model != nil && model.Enabled {
			walletKey := strings.TrimSpace(string(model.APIKey))
			if walletKey == "" || s.isEncryptedStorageValue(walletKey) {
				return ""
			}
			return walletKey
		}
	}
	return strings.TrimSpace(os.Getenv("CLAW402_WALLET_KEY"))
}

func (s *Server) isEncryptedStorageValue(value string) bool {
	return s.cryptoHandler != nil &&
		s.cryptoHandler.cryptoService != nil &&
		s.cryptoHandler.cryptoService.IsEncryptedStorageValue(value)
}
