package request

// Register -.
type Register struct {
	Username string `json:"username" form:"username" validate:"required,min=3,max=255" example:"johndoe"`
	Email    string `json:"email" form:"email" validate:"required,email"         example:"john@example.com"`
	Password string `json:"password" form:"password" validate:"required,min=6"         example:"secret123"`
} // @name v1.Register

// Login -.
type Login struct {
	Email    string `json:"email" form:"email" validate:"required,email" example:"john@example.com"`
	Password string `json:"password" form:"password" validate:"required"       example:"secret123"`
} // @name v1.Login
