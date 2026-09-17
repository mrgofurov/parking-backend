package service

import (
	"encoding/json"
	"log"
	"sync"

	"github.com/gofiber/websocket/v2"
)

type WSEventType string

const (
	WSEventEntry           WSEventType = "VEHICLE_ENTRY"
	WSEventExit            WSEventType = "VEHICLE_EXIT"
	WSEventBarrierChange   WSEventType = "BARRIER_STATUS"
	WSEventPayment         WSEventType = "PAYMENT_SUCCESS"
	WSEventOccupancyUpdate WSEventType = "OCCUPANCY_UPDATE"
)

type WSMessage struct {
	Type    WSEventType `json:"type"`
	Payload interface{} `json:"payload"`
}

type WebSocketHub struct {
	clients    map[*websocket.Conn]bool
	broadcast  chan WSMessage
	register   chan *websocket.Conn
	unregister chan *websocket.Conn
	mu         sync.RWMutex
}

func NewWebSocketHub() *WebSocketHub {
	hub := &WebSocketHub{
		clients:    make(map[*websocket.Conn]bool),
		broadcast:  make(chan WSMessage, 100),
		register:   make(chan *websocket.Conn),
		unregister: make(chan *websocket.Conn),
	}
	go hub.run()
	return hub
}

func (h *WebSocketHub) run() {
	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			h.clients[client] = true
			h.mu.Unlock()
			log.Println("[WebSocket] Client connected. Total clients:", len(h.clients))

		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				_ = client.Close()
			}
			h.mu.Unlock()
			log.Println("[WebSocket] Client disconnected. Total clients:", len(h.clients))

		case msg := <-h.broadcast:
			data, err := json.Marshal(msg)
			if err != nil {
				continue
			}

			h.mu.RLock()
			for client := range h.clients {
				err := client.WriteMessage(websocket.TextMessage, data)
				if err != nil {
					log.Println("[WebSocket] Error sending message:", err)
					_ = client.Close()
					delete(h.clients, client)
				}
			}
			h.mu.RUnlock()
		}
	}
}

func (h *WebSocketHub) Register(c *websocket.Conn) {
	h.register <- c
}

func (h *WebSocketHub) Unregister(c *websocket.Conn) {
	h.unregister <- c
}

func (h *WebSocketHub) Broadcast(eventType WSEventType, payload interface{}) {
	h.broadcast <- WSMessage{
		Type:    eventType,
		Payload: payload,
	}
}
