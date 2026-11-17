package controllers

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/Agora-Mobility/atlas/infra/rest/binding"
	"github.com/Agora-Mobility/atlas/shared"
	usecases "github.com/Agora-Mobility/atlas/usecases/auth"
	"github.com/Agora-Mobility/atlas/usecases/auth/dtos"
)

type AuditController struct {
	audit *usecases.AuditUsecase
}

func NewAuditController(audit *usecases.AuditUsecase) *AuditController {
	return &AuditController{audit: audit}
}

// List godoc
//
//	@Summary		Read the audit log
//	@Description	Requires audit:read. Newest first. Every change of tariffs, policies, accounts, roles and the rest of the backoffice, with who made it and the values before and after. Passwords, hashes, tokens and secrets come as ***; IP and user agent are not shown. An entry without actor is a system action.
//	@Tags			Backoffice audit
//	@Produce		json
//	@Param			actor_id	query		string	false	"Who made the change"
//	@Param			entity		query		string	false	"Type of entity, such as service_tariff or user"
//	@Param			entity_id	query		string	false	"Id of the entity"
//	@Param			action		query		string	false	"Action, such as service_tariff.publish"
//	@Param			from		query		string	false	"At or after, YYYY-MM-DD or RFC 3339"
//	@Param			to			query		string	false	"Before, YYYY-MM-DD or RFC 3339"
//	@Param			page		query		int		false	"Page (from 1)"
//	@Param			size		query		int		false	"Page size (max 100)"
//	@Success		200			{object}	dtos.AuditPage
//	@Failure		401			{object}	shared.ErrorResponse
//	@Failure		403			{object}	shared.ErrorResponse	"Missing audit:read"
//	@Failure		422			{object}	shared.ErrorResponse	"Invalid filters"
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/admin/audit [get]
func (c *AuditController) List(ctx *gin.Context) {
	var query dtos.AuditListQuery
	if err := binding.Query(ctx, &query); err != nil {
		shared.InvalidRequest(ctx, err)
		return
	}
	page, err := c.audit.List(ctx.Request.Context(), query)
	if errors.Is(err, usecases.ErrAuditFilter) {
		shared.RespondError(ctx, http.StatusUnprocessableEntity, shared.CodeInvalidRequest, "actor_id must be a UUID and from and to YYYY-MM-DD or RFC 3339.")
		return
	}
	if err != nil {
		shared.InternalServerError(ctx, shared.CodeInternalError, "An internal error occurred.", err)
		return
	}
	ctx.JSON(http.StatusOK, page)
}
