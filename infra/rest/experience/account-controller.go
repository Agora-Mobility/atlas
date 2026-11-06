package experience

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/Agora-Mobility/atlas/infra/middleware"
	"github.com/Agora-Mobility/atlas/infra/rest/binding"
	"github.com/Agora-Mobility/atlas/shared"
	"github.com/Agora-Mobility/atlas/usecases/experience/dtos"
)

func currentUser(ctx *gin.Context) (uuid.UUID, bool) {
	userID, ok := middleware.UserIDFromContext(ctx.Request.Context())
	if !ok {
		shared.Unauthorized(ctx, shared.CodeUnauthorized, "Authentication required.")
		return uuid.Nil, false
	}
	return userID, true
}

// GetPreferences godoc
//
//	@Summary	Read my preferences
//	@Tags		Account
//	@Produce	json
//	@Success	200	{object}	dtos.PreferencesView
//	@Security	BearerAuth
//	@Security	ClientId
//	@Router		/api/v1/users/me/preferences [get]
func (c *Controller) GetPreferences(ctx *gin.Context) {
	userID, ok := currentUser(ctx)
	if !ok {
		return
	}
	view, err := c.uc.Preferences(ctx.Request.Context(), userID)
	respond(ctx, http.StatusOK, view, err)
}

// SavePreferences godoc
//
//	@Summary		Save my preferences
//	@Description	Status pushes and SMS respect these values when they are sent. The code sent to confirm an identity is not affected.
//	@Tags			Account
//	@Accept			json
//	@Produce		json
//	@Param			body	body		dtos.PreferencesView	true	"Preferences"
//	@Success		200		{object}	dtos.PreferencesView
//	@Failure		422		{object}	shared.ErrorResponse
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/users/me/preferences [put]
func (c *Controller) SavePreferences(ctx *gin.Context) {
	userID, ok := currentUser(ctx)
	if !ok {
		return
	}
	var in dtos.PreferencesView
	if err := binding.JSON(ctx, &in); err != nil {
		shared.InvalidRequest(ctx, err)
		return
	}
	view, err := c.uc.SavePreferences(ctx.Request.Context(), userID, in)
	respond(ctx, http.StatusOK, view, err)
}

// GetAvatar godoc
//
//	@Summary	Read my profile photo
//	@Tags		Account
//	@Produce	json
//	@Success	200	{object}	dtos.AvatarView
//	@Security	BearerAuth
//	@Security	ClientId
//	@Router		/api/v1/users/me/avatar [get]
func (c *Controller) GetAvatar(ctx *gin.Context) {
	userID, ok := currentUser(ctx)
	if !ok {
		return
	}
	view, err := c.uc.Avatar(ctx.Request.Context(), userID)
	respond(ctx, http.StatusOK, view, err)
}

// SetAvatar godoc
//
//	@Summary		Set my profile photo
//	@Description	The photo is uploaded first through POST /media/photos and must belong to the user.
//	@Tags			Account
//	@Accept			json
//	@Produce		json
//	@Param			body	body		dtos.AvatarInput	true	"Photo"
//	@Success		200		{object}	dtos.AvatarView
//	@Failure		404		{object}	shared.ErrorResponse
//	@Failure		422		{object}	shared.ErrorResponse
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/users/me/avatar [put]
func (c *Controller) SetAvatar(ctx *gin.Context) {
	userID, ok := currentUser(ctx)
	if !ok {
		return
	}
	var in dtos.AvatarInput
	if err := binding.JSON(ctx, &in); err != nil {
		shared.InvalidRequest(ctx, err)
		return
	}
	view, err := c.uc.SetAvatar(ctx.Request.Context(), userID, in)
	respond(ctx, http.StatusOK, view, err)
}

// GetReferral godoc
//
//	@Summary	My invitation code and what it earned
//	@Tags		Account
//	@Produce	json
//	@Success	200	{object}	dtos.ReferralView
//	@Security	BearerAuth
//	@Security	ClientId
//	@Router		/api/v1/users/me/referral [get]
func (c *Controller) GetReferral(ctx *gin.Context) {
	userID, ok := currentUser(ctx)
	if !ok {
		return
	}
	view, err := c.uc.Referral(ctx.Request.Context(), userID)
	respond(ctx, http.StatusOK, view, err)
}

// RedeemReferral godoc
//
//	@Summary		Use the code of a friend
//	@Description	Once per account and before the first paid request. The reward goes to the wallet of the friend when this account completes and pays its first request.
//	@Tags			Account
//	@Accept			json
//	@Param			body	body	dtos.RedeemReferralInput	true	"Code"
//	@Success		204		"Done"
//	@Failure		404		{object}	shared.ErrorResponse
//	@Failure		409		{object}	shared.ErrorResponse
//	@Failure		422		{object}	shared.ErrorResponse
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/users/me/referral/redeem [post]
func (c *Controller) RedeemReferral(ctx *gin.Context) {
	userID, ok := currentUser(ctx)
	if !ok {
		return
	}
	var in dtos.RedeemReferralInput
	if err := binding.JSON(ctx, &in); err != nil {
		shared.InvalidRequest(ctx, err)
		return
	}
	respond(ctx, http.StatusNoContent, nil, c.uc.RedeemReferral(ctx.Request.Context(), userID, in))
}

// ListNotifications godoc
//
//	@Summary		My notifications
//	@Description	Read and unread items, each with a link_type and link_id to open the order, offer, booking or ticket.
//	@Tags			Account
//	@Produce		json
//	@Param			unread	query		bool	false	"Only unread"
//	@Param			page	query		int		false	"Page"
//	@Param			size	query		int		false	"Page size"
//	@Success		200		{object}	dtos.InboxList
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/notifications [get]
func (c *Controller) ListNotifications(ctx *gin.Context) {
	userID, ok := currentUser(ctx)
	if !ok {
		return
	}
	page, ok := pageParams(ctx)
	if !ok {
		return
	}
	list, err := c.uc.ListInbox(ctx.Request.Context(), userID, ctx.Query("unread") == "true", page)
	respond(ctx, http.StatusOK, list, err)
}

// ReadNotification godoc
//
//	@Summary	Mark a notification as read
//	@Tags		Account
//	@Param		id	path	string	true	"Notification id"
//	@Success	204	"Done"
//	@Failure	404	{object}	shared.ErrorResponse
//	@Security	BearerAuth
//	@Security	ClientId
//	@Router		/api/v1/notifications/{id}/read [post]
func (c *Controller) ReadNotification(ctx *gin.Context) {
	userID, ok := currentUser(ctx)
	if !ok {
		return
	}
	id, ok := idParam(ctx, "id", "NOTIFICATION_NOT_FOUND", "Notification not found.")
	if !ok {
		return
	}
	respond(ctx, http.StatusNoContent, nil, c.uc.MarkInboxRead(ctx.Request.Context(), userID, id))
}

// ReadAllNotifications godoc
//
//	@Summary	Mark all notifications as read
//	@Tags		Account
//	@Produce	json
//	@Success	200	{object}	dtos.MarkedView
//	@Security	BearerAuth
//	@Security	ClientId
//	@Router		/api/v1/notifications/read-all [post]
func (c *Controller) ReadAllNotifications(ctx *gin.Context) {
	userID, ok := currentUser(ctx)
	if !ok {
		return
	}
	view, err := c.uc.MarkAllInboxRead(ctx.Request.Context(), userID)
	respond(ctx, http.StatusOK, view, err)
}

// Help godoc
//
//	@Summary		Help center
//	@Description	Topics with search, the support line and the most recent request, to open a ticket about it.
//	@Tags			Account
//	@Produce		json
//	@Param			search	query		string	false	"Text"
//	@Success		200		{object}	dtos.HelpView
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/help [get]
func (c *Controller) Help(ctx *gin.Context) {
	userID, ok := currentUser(ctx)
	if !ok {
		return
	}
	view, err := c.uc.Help(ctx.Request.Context(), userID, ctx.Query("search"))
	respond(ctx, http.StatusOK, view, err)
}
