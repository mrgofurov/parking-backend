package response

import "github.com/gofiber/fiber/v2"

type Response struct {
	Success bool        `json:"success"`
	Message string      `json:"message,omitempty"`
	Data    interface{} `json:"data,omitempty"`
	Error   interface{} `json:"error,omitempty"`
}

func Success(c *fiber.Ctx, data interface{}, message ...string) error {
	msg := "Success"
	if len(message) > 0 {
		msg = message[0]
	}
	return c.Status(fiber.StatusOK).JSON(Response{
		Success: true,
		Message: msg,
		Data:    data,
	})
}

func Created(c *fiber.Ctx, data interface{}, message ...string) error {
	msg := "Created successfully"
	if len(message) > 0 {
		msg = message[0]
	}
	return c.Status(fiber.StatusCreated).JSON(Response{
		Success: true,
		Message: msg,
		Data:    data,
	})
}

func Error(c *fiber.Ctx, statusCode int, message string, err ...interface{}) error {
	var errData interface{}
	if len(err) > 0 {
		errData = err[0]
	}
	return c.Status(statusCode).JSON(Response{
		Success: false,
		Message: message,
		Error:   errData,
	})
}
