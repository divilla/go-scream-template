package v1

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/divilla/go-scream-template/internal/controller/restapi/v1/request"
	"github.com/divilla/go-scream-template/internal/controller/restapi/v1/response"
	"github.com/divilla/go-scream-template/internal/entity"
	"github.com/labstack/echo/v5"
)

// @Summary     Create task
// @Description Create a new task for the current user
// @ID          create-task
// @Tags        tasks
// @Accept      json,xml,x-www-form-urlencoded,mpfd
// @Produce     json
// @Param       request body     request.CreateTask true "Task data"
// @Success     201     {object} entity.Task
// @Failure     400     {object} response.Error
// @Failure     401     {object} response.Error
// @Failure     500     {object} response.Error
// @Security    BearerAuth
// @Failure     413     {object} map[string]string "Request body exceeds 4 MiB"
// @Router      /tasks [post]
func (r *V1) createTask(ctx *echo.Context) error {
	userID, ok := ctx.Get("userID").(string)
	if !ok {
		return errorResponse(ctx, http.StatusUnauthorized, "unauthorized")
	}

	var body request.CreateTask

	if err := bindBody(ctx, &body); err != nil {
		r.l.Error(err, "restapi - v1 - createTask")

		return bodyErrorResponse(ctx, err)
	}

	if err := r.v.Struct(body); err != nil {
		r.l.Error(err, "restapi - v1 - createTask")

		return errorResponse(ctx, http.StatusBadRequest, "invalid request body")
	}

	task, err := r.tk.Create(ctx.Request().Context(), userID, body.Title, body.Description)
	if err != nil {
		r.l.Error(err, "restapi - v1 - createTask")

		return errorResponse(ctx, http.StatusInternalServerError, "internal server error")
	}

	return ctx.JSON(http.StatusCreated, task)
}

// @Summary     List tasks
// @Description List tasks for the current user with optional filtering
// @ID          list-tasks
// @Tags        tasks
// @Produce     json
// @Param       status query    string false "Filter by status" Enums(todo, in_progress, done)
// @Param       limit  query    int    false "Limit"  default(10)
// @Param       offset query    int    false "Offset" default(0)
// @Success     200    {object} response.TaskList
// @Failure     401    {object} response.Error
// @Failure     500    {object} response.Error
// @Security    BearerAuth
// @Router      /tasks [get]
func (r *V1) listTasks(ctx *echo.Context) error {
	userID, ok := ctx.Get("userID").(string)
	if !ok {
		return errorResponse(ctx, http.StatusUnauthorized, "unauthorized")
	}

	var status *entity.TaskStatus

	if s := ctx.QueryParam("status"); s != "" {
		ts := entity.TaskStatus(s)
		if !ts.Valid() {
			return errorResponse(ctx, http.StatusBadRequest, "invalid task status")
		}

		status = &ts
	}

	limit, err := strconv.Atoi(ctx.QueryParam("limit"))
	if err != nil {
		limit = 10
	}

	offset, err := strconv.Atoi(ctx.QueryParam("offset"))
	if err != nil {
		offset = 0
	}

	tasks, total, err := r.tk.List(ctx.Request().Context(), userID, status, limit, offset)
	if err != nil {
		r.l.Error(err, "restapi - v1 - listTasks")

		return errorResponse(ctx, http.StatusInternalServerError, "internal server error")
	}

	return ctx.JSON(http.StatusOK, response.TaskList{
		Tasks: tasks,
		Total: total,
	})
}

// @Summary     Get task
// @Description Get a task by ID
// @ID          get-task
// @Tags        tasks
// @Produce     json
// @Param       id  path     string true "Task ID"
// @Success     200 {object} entity.Task
// @Failure     401 {object} response.Error
// @Failure     403 {object} response.Error
// @Failure     404 {object} response.Error
// @Failure     500 {object} response.Error
// @Security    BearerAuth
// @Router      /tasks/{id} [get]
func (r *V1) getTask(ctx *echo.Context) error {
	userID, ok := ctx.Get("userID").(string)
	if !ok {
		return errorResponse(ctx, http.StatusUnauthorized, "unauthorized")
	}

	taskID := ctx.Param("id")

	task, err := r.tk.Get(ctx.Request().Context(), userID, taskID)
	if err != nil {
		r.l.Error(err, "restapi - v1 - getTask")

		if errors.Is(err, entity.ErrTaskNotFound) {
			return errorResponse(ctx, http.StatusNotFound, "task not found")
		}

		if errors.Is(err, entity.ErrTaskForbidden) {
			return errorResponse(ctx, http.StatusForbidden, "forbidden")
		}

		return errorResponse(ctx, http.StatusInternalServerError, "internal server error")
	}

	return ctx.JSON(http.StatusOK, task)
}

// @Summary     Update task
// @Description Update task title and description
// @ID          update-task
// @Tags        tasks
// @Accept      json,xml,x-www-form-urlencoded,mpfd
// @Produce     json
// @Param       id      path     string            true "Task ID"
// @Param       request body     request.UpdateTask  true "Updated task data"
// @Success     200     {object} entity.Task
// @Failure     400     {object} response.Error
// @Failure     401     {object} response.Error
// @Failure     403     {object} response.Error
// @Failure     404     {object} response.Error
// @Failure     500     {object} response.Error
// @Security    BearerAuth
// @Failure     413     {object} map[string]string "Request body exceeds 4 MiB"
// @Router      /tasks/{id} [put]
func (r *V1) updateTask(ctx *echo.Context) error {
	userID, ok := ctx.Get("userID").(string)
	if !ok {
		return errorResponse(ctx, http.StatusUnauthorized, "unauthorized")
	}

	taskID := ctx.Param("id")

	var body request.UpdateTask

	if err := bindBody(ctx, &body); err != nil {
		r.l.Error(err, "restapi - v1 - updateTask")

		return bodyErrorResponse(ctx, err)
	}

	if err := r.v.Struct(body); err != nil {
		r.l.Error(err, "restapi - v1 - updateTask")

		return errorResponse(ctx, http.StatusBadRequest, "invalid request body")
	}

	task, err := r.tk.Update(ctx.Request().Context(), userID, taskID, body.Title, body.Description)
	if err != nil {
		r.l.Error(err, "restapi - v1 - updateTask")

		if errors.Is(err, entity.ErrTaskNotFound) {
			return errorResponse(ctx, http.StatusNotFound, "task not found")
		}

		if errors.Is(err, entity.ErrTaskForbidden) {
			return errorResponse(ctx, http.StatusForbidden, "forbidden")
		}

		return errorResponse(ctx, http.StatusInternalServerError, "internal server error")
	}

	return ctx.JSON(http.StatusOK, task)
}

// @Summary     Transition task status
// @Description Change task status (todo -> in_progress -> done, or in_progress -> todo)
// @ID          transition-task
// @Tags        tasks
// @Accept      json,xml,x-www-form-urlencoded,mpfd
// @Produce     json
// @Param       id      path     string                true "Task ID"
// @Param       request body     request.TransitionTask  true "New status"
// @Success     200     {object} entity.Task
// @Failure     400     {object} response.Error
// @Failure     401     {object} response.Error
// @Failure     403     {object} response.Error
// @Failure     404     {object} response.Error
// @Failure     500     {object} response.Error
// @Security    BearerAuth
// @Failure     413     {object} map[string]string "Request body exceeds 4 MiB"
// @Router      /tasks/{id}/status [patch]
func (r *V1) transitionTask(ctx *echo.Context) error {
	userID, ok := ctx.Get("userID").(string)
	if !ok {
		return errorResponse(ctx, http.StatusUnauthorized, "unauthorized")
	}

	taskID := ctx.Param("id")

	var body request.TransitionTask

	if err := bindBody(ctx, &body); err != nil {
		r.l.Error(err, "restapi - v1 - transitionTask")

		return bodyErrorResponse(ctx, err)
	}

	if err := r.v.Struct(body); err != nil {
		r.l.Error(err, "restapi - v1 - transitionTask")

		return errorResponse(ctx, http.StatusBadRequest, "invalid request body")
	}

	task, err := r.tk.Transition(ctx.Request().Context(), userID, taskID, body.Status)
	if err != nil {
		r.l.Error(err, "restapi - v1 - transitionTask")

		if errors.Is(err, entity.ErrTaskNotFound) {
			return errorResponse(ctx, http.StatusNotFound, "task not found")
		}

		if errors.Is(err, entity.ErrTaskForbidden) {
			return errorResponse(ctx, http.StatusForbidden, "forbidden")
		}

		if errors.Is(err, entity.ErrInvalidTransition) {
			return errorResponse(ctx, http.StatusBadRequest, "invalid status transition")
		}

		return errorResponse(ctx, http.StatusInternalServerError, "internal server error")
	}

	return ctx.JSON(http.StatusOK, task)
}

// @Summary     Delete task
// @Description Delete a task by ID
// @ID          delete-task
// @Tags        tasks
// @Param       id  path     string true "Task ID"
// @Success     204 "No Content"
// @Failure     401 {object} response.Error
// @Failure     404 {object} response.Error
// @Failure     500 {object} response.Error
// @Security    BearerAuth
// @Router      /tasks/{id} [delete]
func (r *V1) deleteTask(ctx *echo.Context) error {
	userID, ok := ctx.Get("userID").(string)
	if !ok {
		return errorResponse(ctx, http.StatusUnauthorized, "unauthorized")
	}

	taskID := ctx.Param("id")

	err := r.tk.Delete(ctx.Request().Context(), userID, taskID)
	if err != nil {
		r.l.Error(err, "restapi - v1 - deleteTask")

		if errors.Is(err, entity.ErrTaskNotFound) {
			return errorResponse(ctx, http.StatusNotFound, "task not found")
		}

		return errorResponse(ctx, http.StatusInternalServerError, "internal server error")
	}

	return ctx.NoContent(http.StatusNoContent)
}
