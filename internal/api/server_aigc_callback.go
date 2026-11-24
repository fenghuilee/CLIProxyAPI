package api

import (
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"
)

func (s *Server) aigcCallbackHandler(c *gin.Context) {
	coordinator := s.getAIGCCoordinator()
	if coordinator == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "aigc service unavailable"})
		return
	}

	provider := strings.TrimSpace(c.Param("provider"))
	genID := strings.TrimSpace(c.Query("gid"))
	if genID == "" {
		genID = strings.TrimSpace(c.Query("generation_id"))
	}
	token := strings.TrimSpace(c.Query("token"))

	bodyBytes, errRead := io.ReadAll(c.Request.Body)
	if errRead != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to read callback body"})
		return
	}

	errHandle := coordinator.HandleCallback(c.Request.Context(), provider, genID, token, bodyBytes, c.Request.Header)
	if errHandle != nil {
		log.Warnf("aigc: callback handling failed for provider=%s genID=%s: %v", provider, genID, errHandle)
		c.JSON(http.StatusBadRequest, gin.H{"error": errHandle.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}
