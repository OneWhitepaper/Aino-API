package routes

import (
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/gin-gonic/gin"
)

// registerTeamFundingRoutes inherits the user panel JWT, audit and rate limits.
func registerTeamFundingRoutes(user *gin.RouterGroup, h *handler.TeamFundingHandler) {
	if h == nil {
		return
	}
	user.GET("/funding", h.GetFunding)
	user.PUT("/funding", h.UpdateFunding)
	user.GET("/teams", h.ListTeams)
	user.POST("/teams", h.CreateTeam)
	user.GET("/teams/:id", h.GetTeam)
	user.PATCH("/teams/:id", h.UpdateTeam)
	user.POST("/teams/:id/invitations", h.CreateInvitation)
	user.GET("/team-invitations", h.ListInvitations)
	user.POST("/team-invitations/:id/accept", h.AcceptInvitation)
	user.POST("/team-invitations/:id/decline", h.DeclineInvitation)
	user.DELETE("/teams/:id/invitations/:invitation_id", h.RevokeInvitation)
	user.PATCH("/teams/:id/members/:user_id", h.UpdateMember)
	user.DELETE("/teams/:id/members/:user_id", h.RemoveMember)
	user.GET("/teams/:id/ledger", h.ListLedger)
}
