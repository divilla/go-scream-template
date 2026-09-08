package request

import "github.com/divilla/go-scream-template/internal/entity"

// CreateTask -.
type CreateTask struct {
	Title       string `json:"title" form:"title" validate:"required,max=255" example:"My task"`
	Description string `json:"description" form:"description" validate:"max=1000"         example:"Task description"`
} // @name v1.CreateTask

// UpdateTask -.
type UpdateTask struct {
	Title       string `json:"title" form:"title" validate:"required,max=255" example:"Updated task"`
	Description string `json:"description" form:"description" validate:"max=1000"         example:"Updated description"`
} // @name v1.UpdateTask

// TransitionTask -.
type TransitionTask struct {
	Status entity.TaskStatus `json:"status" form:"status" validate:"required,oneof=todo in_progress done" example:"in_progress"`
} // @name v1.TransitionTask
