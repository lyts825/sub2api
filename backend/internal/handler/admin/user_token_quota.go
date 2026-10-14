package admin

import (
	"context"
	"net/http"
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type userTokenQuotaAdminService interface {
	GetUserTokenQuota(ctx context.Context, userID int64) (*service.UserTokenQuota, error)
	UpdateUserTokenQuota(ctx context.Context, userID int64, quota *int64, resetUsed bool) (*service.UserTokenQuota, error)
}

// GetUserTokenQuota returns the shared allowance across all of a user's keys.
// GET /api/v1/admin/users/:id/token-quota
func (h *UserHandler) GetUserTokenQuota(c *gin.Context) {
	userID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || userID <= 0 {
		response.BadRequest(c, "Invalid user ID")
		return
	}
	svc, ok := h.adminService.(userTokenQuotaAdminService)
	if !ok {
		response.Error(c, http.StatusServiceUnavailable, "token quota service not available")
		return
	}
	quota, err := svc.GetUserTokenQuota(c.Request.Context(), userID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, quota)
}

// UpdateUserTokenQuotaRequest leaves the current allowance unchanged when quota
// is omitted; reset_used resets only the user's aggregate token counter.
type UpdateUserTokenQuotaRequest struct {
	Quota     *int64 `json:"quota" binding:"omitempty,gte=0"`
	ResetUsed bool   `json:"reset_used"`
}

// UpdateUserTokenQuota is registered only under the administrator-authenticated
// route group. User-owned API Key updates cannot change this shared allowance.
// PUT /api/v1/admin/users/:id/token-quota
func (h *UserHandler) UpdateUserTokenQuota(c *gin.Context) {
	userID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || userID <= 0 {
		response.BadRequest(c, "Invalid user ID")
		return
	}
	var req UpdateUserTokenQuotaRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	svc, ok := h.adminService.(userTokenQuotaAdminService)
	if !ok {
		response.Error(c, http.StatusServiceUnavailable, "token quota service not available")
		return
	}
	quota, err := svc.UpdateUserTokenQuota(c.Request.Context(), userID, req.Quota, req.ResetUsed)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, quota)
}
