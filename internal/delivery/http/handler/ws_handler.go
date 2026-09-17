package handler

import (
	"log"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/websocket/v2"
	"github.com/smart-parking/parking-backend/internal/service"
)

type WSHandler struct {
	hub *service.WebSocketHub
}

func NewWSHandler(hub *service.WebSocketHub) *WSHandler {
	return &WSHandler{hub: hub}
}

func (h *WSHandler) UpgradeMiddleware() fiber.Handler {
	return func(c *fiber.Ctx) error {
		if websocket.IsWebSocketUpgrade(c) {
			c.Locals("allowed", true)
			return c.Next()
		}
		return fiber.ErrUpgradeRequired
	}
}

func (h *WSHandler) HandleWS() fiber.Handler {
	return websocket.New(func(c *websocket.Conn) {
		h.hub.Register(c)
		defer func() {
			h.hub.Unregister(c)
		}()

		for {
			_, _, err := c.ReadMessage()
			if err != nil {
				if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
					log.Printf("[WebSocket] Error reading message: %v\n", err)
				}
				break
			}
		}
	})
}
