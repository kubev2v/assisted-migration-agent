package v2

import (
	"crypto/subtle"
	"net"
	"net/http"

	"github.com/gin-gonic/gin"

	v2 "github.com/kubev2v/assisted-migration-agent/agent/api/v2"
	"github.com/kubev2v/assisted-migration-agent/internal/upgrade"
)

type UpgradeHandler struct {
	v2.ServerInterface
	lifecycle *upgrade.Service
}

func NewUpgradeHandler(base v2.ServerInterface, lifecycle *upgrade.Service) *UpgradeHandler {
	return &UpgradeHandler{ServerInterface: base, lifecycle: lifecycle}
}

// Admission blocks all ordinary API handlers while draining and after baseline capture.
// Even GET handlers can initialize services and race the captured baseline.
func (h *UpgradeHandler) Admission(c *gin.Context) {
	if c.Request.URL.Path == "/api/v2/upgrade" || c.Request.URL.Path == "/api/v2/version" {
		c.Next()
		return
	}
	if !h.lifecycle.BeginRequest() {
		c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"error": "agent is in upgrade maintenance"})
		return
	}
	defer h.lifecycle.EndRequest()
	c.Next()
}

func (h *UpgradeHandler) GetUpgrade(c *gin.Context, _ v2.GetUpgradeParams) {
	if !h.authorized(c) {
		return
	}
	c.JSON(http.StatusOK, h.lifecycle.Status())
}
func (h *UpgradeHandler) PutUpgrade(c *gin.Context, _ v2.PutUpgradeParams) {
	if !h.authorized(c) {
		return
	}
	var request upgrade.Request
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid upgrade request"})
		return
	}
	if err := h.lifecycle.Transition(request); err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusAccepted, h.lifecycle.Status())
}
func (h *UpgradeHandler) authorized(c *gin.Context) bool {
	host, _, err := net.SplitHostPort(c.Request.RemoteAddr)
	ip := net.ParseIP(host)
	if err != nil || !ip.IsLoopback() || subtle.ConstantTimeCompare([]byte(c.GetHeader("X-Upgrade-Token")), []byte(h.lifecycle.Token())) != 1 {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "host upgrade control requires local authorization"})
		return false
	}
	return true
}

// Base handlers remain usable in compositions without host lifecycle control.
func (h *Handler) GetUpgrade(c *gin.Context, _ v2.GetUpgradeParams) {
	c.AbortWithStatus(http.StatusServiceUnavailable)
}
func (h *Handler) PutUpgrade(c *gin.Context, _ v2.PutUpgradeParams) {
	c.AbortWithStatus(http.StatusServiceUnavailable)
}
