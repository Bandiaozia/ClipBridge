package ws

import (
	"encoding/json"
	"sync"
)

type Client struct {
	UserID   string
	DeviceID string
	Send     chan []byte
	Close    func(code int, reason string)
}

type Hub struct {
	mu      sync.RWMutex
	clients map[string]map[string]*Client
}

func NewHub() *Hub {
	return &Hub{clients: make(map[string]map[string]*Client)}
}

func (h *Hub) Register(client *Client) (replaced *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	byDevice := h.clients[client.UserID]
	if byDevice == nil {
		byDevice = make(map[string]*Client)
		h.clients[client.UserID] = byDevice
	}
	replaced = byDevice[client.DeviceID]
	byDevice[client.DeviceID] = client
	return replaced
}

// Unregister 仅在 client 仍是该设备的当前连接时删除并返回 true。
// 网络切换时新连接会先替换旧连接，旧连接随后退出不得把新连接标成离线。
func (h *Hub) Unregister(client *Client) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	byDevice := h.clients[client.UserID]
	if byDevice == nil || byDevice[client.DeviceID] != client {
		return false
	}
	delete(byDevice, client.DeviceID)
	if len(byDevice) == 0 {
		delete(h.clients, client.UserID)
	}
	return true
}

func (h *Hub) IsOnline(userID, deviceID string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.clients[userID] != nil && h.clients[userID][deviceID] != nil
}

func (h *Hub) Send(userID, deviceID string, value any) bool {
	payload, err := json.Marshal(value)
	if err != nil {
		return false
	}
	h.mu.RLock()
	client := h.clients[userID][deviceID]
	h.mu.RUnlock()
	if client == nil {
		return false
	}
	select {
	case client.Send <- payload:
		return true
	default:
		client.Close(4008, "发送队列已满")
		return false
	}
}

func (h *Hub) BroadcastExcept(userID, excludedDevice string, value any) {
	payload, err := json.Marshal(value)
	if err != nil {
		return
	}
	h.mu.RLock()
	targets := make([]*Client, 0, len(h.clients[userID]))
	for id, client := range h.clients[userID] {
		if id != excludedDevice {
			targets = append(targets, client)
		}
	}
	h.mu.RUnlock()
	for _, client := range targets {
		select {
		case client.Send <- payload:
		default:
			client.Close(4008, "发送队列已满")
		}
	}
}

func (h *Hub) Revoke(userID, deviceID string) {
	h.mu.RLock()
	client := h.clients[userID][deviceID]
	h.mu.RUnlock()
	if client != nil {
		_ = h.Send(userID, deviceID, map[string]any{"type": "device_revoked"})
		client.Close(4003, "设备已撤销")
	}
	h.BroadcastExcept(userID, deviceID, map[string]any{
		"type": "device_revoked", "device_id": deviceID,
	})
}

func (h *Hub) RevokeUser(userID string) {
	h.mu.RLock()
	clients := make([]*Client, 0, len(h.clients[userID]))
	for _, client := range h.clients[userID] {
		clients = append(clients, client)
	}
	h.mu.RUnlock()
	for _, client := range clients {
		_ = h.Send(userID, client.DeviceID, map[string]any{"type": "device_revoked"})
		client.Close(4003, "账户已删除")
	}
}

type Stats struct {
	Users   int
	Devices int
}

func (h *Hub) Stats() Stats {
	h.mu.RLock()
	defer h.mu.RUnlock()
	users := len(h.clients)
	devices := 0
	for _, byDevice := range h.clients {
		devices += len(byDevice)
	}
	return Stats{Users: users, Devices: devices}
}

func (h *Hub) MarkOnline(userID string, devices []string) map[string]bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	result := make(map[string]bool, len(devices))
	for _, id := range devices {
		result[id] = h.clients[userID] != nil && h.clients[userID][id] != nil
	}
	return result
}
