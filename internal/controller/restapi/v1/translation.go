package v1

import (
	"net/http"

	"github.com/divilla/go-scream-template/internal/controller/restapi/v1/request"
	_ "github.com/divilla/go-scream-template/internal/controller/restapi/v1/response" // for swaggo
	"github.com/divilla/go-scream-template/internal/entity"
	"github.com/labstack/echo/v5"
)

// @Summary     Show history
// @Description Show all translation history for current user
// @ID          history
// @Tags        translation
// @Produce     json
// @Success     200 {object} entity.TranslationHistory
// @Failure     401 {object} response.Error
// @Failure     500 {object} response.Error
// @Security    BearerAuth
// @Router      /translation/history [get]
func (r *V1) history(ctx *echo.Context) error {
	userID, ok := ctx.Get("userID").(string)
	if !ok {
		return errorResponse(ctx, http.StatusUnauthorized, "unauthorized")
	}

	translationHistory, err := r.t.History(ctx.Request().Context(), userID)
	if err != nil {
		r.l.Error(err, "restapi - v1 - history")

		return errorResponse(ctx, http.StatusInternalServerError, "database problems")
	}

	return ctx.JSON(http.StatusOK, translationHistory)
}

// @Summary     Translate
// @Description Translate a text
// @ID          do-translate
// @Tags        translation
// @Accept      json,xml,x-www-form-urlencoded,mpfd
// @Produce     json
// @Param       request body     request.Translate true "Set up translation"
// @Success     200     {object} entity.Translation
// @Failure     400     {object} response.Error
// @Failure     401     {object} response.Error
// @Failure     500     {object} response.Error
// @Security    BearerAuth
// @Failure     413     {object} map[string]string "Request body exceeds 4 MiB"
// @Router      /translation/do-translate [post]
func (r *V1) doTranslate(ctx *echo.Context) error {
	userID, ok := ctx.Get("userID").(string)
	if !ok {
		return errorResponse(ctx, http.StatusUnauthorized, "unauthorized")
	}

	var body request.Translate

	if err := bindBody(ctx, &body); err != nil {
		r.l.Error(err, "restapi - v1 - doTranslate")

		return bodyErrorResponse(ctx, err)
	}

	if err := r.v.Struct(body); err != nil {
		r.l.Error(err, "restapi - v1 - doTranslate")

		return errorResponse(ctx, http.StatusBadRequest, "invalid request body")
	}

	translation, err := r.t.Translate(
		ctx.Request().Context(),
		userID,
		entity.Translation{
			Source:      body.Source,
			Destination: body.Destination,
			Original:    body.Original,
		},
	)
	if err != nil {
		r.l.Error(err, "restapi - v1 - doTranslate")

		return errorResponse(ctx, http.StatusInternalServerError, "translation service problems")
	}

	return ctx.JSON(http.StatusOK, translation)
}
