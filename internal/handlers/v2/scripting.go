package v2

import (
	"encoding/base64"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/kubev2v/assisted-migration-agent/internal/services"
)

type ScriptForm struct {
	Data string `json:"data" binding:"required"`
}

type scriptOutput struct {
	Type string `json:"type"`
	Name string `json:"name,omitempty"`
	Data string `json:"data"`
}

type scriptResponse struct {
	Outputs []scriptOutput `json:"outputs"`
	Stdout  string         `json:"stdout"`
	Error   string         `json:"error,omitempty"`
}

type ScriptingHandler struct {
	svc *services.ScriptingService
}

func NewScriptingHandler(svc *services.ScriptingService) *ScriptingHandler {
	return &ScriptingHandler{svc: svc}
}

func (h *ScriptingHandler) RunScript(c *gin.Context) {
	var sf ScriptForm
	if err := c.ShouldBindBodyWithJSON(&sf); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	result, err := h.svc.Execute(c.Request.Context(), sf.Data)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	resp := scriptResponse{
		Outputs: make([]scriptOutput, len(result.Outputs)),
		Stdout:  base64.StdEncoding.EncodeToString([]byte(result.Stdout)),
	}
	for i, o := range result.Outputs {
		resp.Outputs[i] = scriptOutput{
			Type: string(o.Type),
			Name: o.Name,
			Data: string(o.Data),
		}
	}
	if result.Err != nil {
		resp.Error = result.Err.Error()
	}

	c.JSON(http.StatusOK, resp)
}
