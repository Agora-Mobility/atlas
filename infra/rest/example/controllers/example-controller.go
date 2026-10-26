// Package controllers binds HTTP requests of the example module to its
// usecase.
package controllers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/Agora-Mobility/atlas/infra/observability/businessmetrics"
	"github.com/Agora-Mobility/atlas/infra/rest/binding"
	"github.com/Agora-Mobility/atlas/shared/customerrors"
	"github.com/Agora-Mobility/atlas/usecases/example"
	"github.com/Agora-Mobility/atlas/usecases/example/dtos"
)

type ExampleController struct {
	useCase *example.ExampleUseCase
}

func NewExampleController(useCase *example.ExampleUseCase) *ExampleController {
	return &ExampleController{useCase: useCase}
}

// Create godoc
//
//	@Summary		Create an example
//	@Description	Creates an example and emits example.created.v1 through the outbox.
//	@Tags			examples
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			body	body		dtos.CreateExampleInput	true	"Example"
//	@Success		201		{object}	dtos.ExampleOutput
//	@Failure		400		{object}	customerrors.Response
//	@Failure		401		{object}	customerrors.Response
//	@Failure		403		{object}	customerrors.Response
//	@Failure		422		{object}	customerrors.Response
//	@Router			/api/v1/examples [post]
func (ctl *ExampleController) Create(c *gin.Context) {
	var input dtos.CreateExampleInput
	if err := binding.JSON(c, &input); err != nil {
		_ = c.Error(err)
		return
	}

	out, err := ctl.useCase.Create(c.Request.Context(), input)
	if err != nil {
		_ = c.Error(err)
		return
	}
	businessmetrics.ExamplesCreated.Inc()
	c.JSON(http.StatusCreated, out)
}

// GetByID godoc
//
//	@Summary	Get an example
//	@Tags		examples
//	@Produce	json
//	@Param		id	path		string	true	"Example ID (UUID)"
//	@Success	200	{object}	dtos.ExampleOutput
//	@Failure	400	{object}	customerrors.Response
//	@Failure	404	{object}	customerrors.Response
//	@Router		/api/v1/examples/{id} [get]
func (ctl *ExampleController) GetByID(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		_ = c.Error(customerrors.BadRequest("id must be a valid UUID"))
		return
	}

	out, err := ctl.useCase.GetByID(c.Request.Context(), dtos.GetExampleInput{ID: id})
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, out)
}

// List godoc
//
//	@Summary	List examples
//	@Tags		examples
//	@Produce	json
//	@Param		page	query		int	false	"Page (default 1)"
//	@Param		size	query		int	false	"Page size (default 20, max 100)"
//	@Success	200		{object}	dtos.ExamplePageOutput
//	@Failure	400		{object}	customerrors.Response
//	@Router		/api/v1/examples [get]
func (ctl *ExampleController) List(c *gin.Context) {
	var input dtos.ListExamplesInput
	if err := binding.Query(c, &input); err != nil {
		_ = c.Error(err)
		return
	}

	out, err := ctl.useCase.List(c.Request.Context(), input)
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, out)
}
