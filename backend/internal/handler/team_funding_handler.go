package handler

import (
	"encoding/json"
	"io"
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type TeamFundingHandler struct{ funding *service.TeamFundingService }

func NewTeamFundingHandler(funding *service.TeamFundingService) *TeamFundingHandler {
	return &TeamFundingHandler{funding: funding}
}
func teamFundingActor(c *gin.Context) (int64, bool) {
	s, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok || s.UserID <= 0 {
		response.Unauthorized(c, "User not authenticated")
		return 0, false
	}
	return s.UserID, true
}
func teamFundingID(c *gin.Context, key string) (int64, bool) {
	id, e := strconv.ParseInt(c.Param(key), 10, 64)
	if e != nil || id <= 0 {
		response.BadRequest(c, "Invalid "+key)
		return 0, false
	}
	return id, true
}
func teamFundingJSON(c *gin.Context, v any) bool {
	dec := json.NewDecoder(io.LimitReader(c.Request.Body, 65537))
	dec.DisallowUnknownFields()
	if e := dec.Decode(v); e != nil {
		response.BadRequest(c, "Invalid request: "+e.Error())
		return false
	}
	var extra any
	if e := dec.Decode(&extra); e != io.EOF {
		response.BadRequest(c, "Invalid request body")
		return false
	}
	return true
}
func teamFundingResult(c *gin.Context, v any, e error) {
	if response.ErrorFrom(c, e) {
		return
	}
	if v == nil {
		v = gin.H{}
	}
	response.Success(c, v)
}
func (h *TeamFundingHandler) GetFunding(c *gin.Context) {
	u, ok := teamFundingActor(c)
	if !ok {
		return
	}
	v, e := h.funding.GetFunding(c.Request.Context(), u)
	teamFundingResult(c, v, e)
}
func (h *TeamFundingHandler) UpdateFunding(c *gin.Context) {
	u, ok := teamFundingActor(c)
	if !ok {
		return
	}
	var req service.FundingUpdateCommand
	if !teamFundingJSON(c, &req) {
		return
	}
	v, e := h.funding.UpdateFunding(c.Request.Context(), u, &req)
	teamFundingResult(c, v, e)
}
func (h *TeamFundingHandler) ListTeams(c *gin.Context) {
	u, ok := teamFundingActor(c)
	if !ok {
		return
	}
	v, e := h.funding.ListTeams(c.Request.Context(), u)
	teamFundingResult(c, v, e)
}
func (h *TeamFundingHandler) CreateTeam(c *gin.Context) {
	u, ok := teamFundingActor(c)
	if !ok {
		return
	}
	var req service.TeamCreateCommand
	if !teamFundingJSON(c, &req) {
		return
	}
	v, e := h.funding.CreateTeam(c.Request.Context(), u, &req)
	teamFundingResult(c, v, e)
}
func (h *TeamFundingHandler) GetTeam(c *gin.Context) {
	u, ok := teamFundingActor(c)
	if !ok {
		return
	}
	id, ok := teamFundingID(c, "id")
	if !ok {
		return
	}
	v, e := h.funding.GetTeam(c.Request.Context(), u, id)
	teamFundingResult(c, v, e)
}
func (h *TeamFundingHandler) UpdateTeam(c *gin.Context) {
	u, ok := teamFundingActor(c)
	if !ok {
		return
	}
	id, ok := teamFundingID(c, "id")
	if !ok {
		return
	}
	var req service.TeamUpdateCommand
	if !teamFundingJSON(c, &req) {
		return
	}
	v, e := h.funding.UpdateTeam(c.Request.Context(), u, id, &req)
	teamFundingResult(c, v, e)
}
func (h *TeamFundingHandler) CreateInvitation(c *gin.Context) {
	u, ok := teamFundingActor(c)
	if !ok {
		return
	}
	id, ok := teamFundingID(c, "id")
	if !ok {
		return
	}
	var req service.InvitationCreateCommand
	if !teamFundingJSON(c, &req) {
		return
	}
	v, e := h.funding.CreateInvitation(c.Request.Context(), u, id, &req)
	teamFundingResult(c, v, e)
}
func (h *TeamFundingHandler) ListInvitations(c *gin.Context) {
	u, ok := teamFundingActor(c)
	if !ok {
		return
	}
	v, e := h.funding.ListInvitations(c.Request.Context(), u)
	teamFundingResult(c, gin.H{"items": v}, e)
}
func (h *TeamFundingHandler) AcceptInvitation(c *gin.Context) {
	u, ok := teamFundingActor(c)
	if !ok {
		return
	}
	id, ok := teamFundingID(c, "id")
	if !ok {
		return
	}
	teamFundingResult(c, nil, h.funding.AcceptInvitation(c.Request.Context(), u, id))
}
func (h *TeamFundingHandler) DeclineInvitation(c *gin.Context) {
	u, ok := teamFundingActor(c)
	if !ok {
		return
	}
	id, ok := teamFundingID(c, "id")
	if !ok {
		return
	}
	teamFundingResult(c, nil, h.funding.DeclineInvitation(c.Request.Context(), u, id))
}
func (h *TeamFundingHandler) RevokeInvitation(c *gin.Context) {
	u, ok := teamFundingActor(c)
	if !ok {
		return
	}
	id, ok := teamFundingID(c, "id")
	if !ok {
		return
	}
	invite, ok := teamFundingID(c, "invitation_id")
	if !ok {
		return
	}
	teamFundingResult(c, nil, h.funding.RevokeInvitation(c.Request.Context(), u, id, invite))
}
func (h *TeamFundingHandler) UpdateMember(c *gin.Context) {
	u, ok := teamFundingActor(c)
	if !ok {
		return
	}
	id, ok := teamFundingID(c, "id")
	if !ok {
		return
	}
	member, ok := teamFundingID(c, "user_id")
	if !ok {
		return
	}
	var req service.TeamMemberPatch
	if !teamFundingJSON(c, &req) {
		return
	}
	v, e := h.funding.UpdateMember(c.Request.Context(), u, id, member, &req)
	teamFundingResult(c, v, e)
}
func (h *TeamFundingHandler) RemoveMember(c *gin.Context) {
	u, ok := teamFundingActor(c)
	if !ok {
		return
	}
	id, ok := teamFundingID(c, "id")
	if !ok {
		return
	}
	member, ok := teamFundingID(c, "user_id")
	if !ok {
		return
	}
	teamFundingResult(c, nil, h.funding.RemoveMember(c.Request.Context(), u, id, member))
}
func (h *TeamFundingHandler) ListLedger(c *gin.Context) {
	u, ok := teamFundingActor(c)
	if !ok {
		return
	}
	id, ok := teamFundingID(c, "id")
	if !ok {
		return
	}
	page, size := response.ParsePagination(c)
	v, e := h.funding.ListLedger(c.Request.Context(), u, id, page, size)
	teamFundingResult(c, v, e)
}
