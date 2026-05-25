package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"nofx/provider/nofxos"
)

// handleNofxosStatus returns recent nofxos API call records for monitoring.
func (s *Server) handleNofxosStatus(c *gin.Context) {
	records := nofxos.GetCallRecords()
	// Reverse so newest is first
	for i, j := 0, len(records)-1; i < j; i, j = i+1, j-1 {
		records[i], records[j] = records[j], records[i]
	}
	c.JSON(http.StatusOK, gin.H{
		"records": records,
		"count":   len(records),
	})
}
