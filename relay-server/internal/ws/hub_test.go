package ws

import "testing"

func TestOldConnectionCannotUnregisterReplacement(t *testing.T) {
	hub := NewHub()
	oldClient := &Client{
		UserID: "user", DeviceID: "device", Send: make(chan []byte, 1),
		Close: func(int, string) {},
	}
	newClient := &Client{
		UserID: "user", DeviceID: "device", Send: make(chan []byte, 1),
		Close: func(int, string) {},
	}

	if replaced := hub.Register(oldClient); replaced != nil {
		t.Fatal("first registration unexpectedly replaced a client")
	}
	if replaced := hub.Register(newClient); replaced != oldClient {
		t.Fatal("new connection did not replace the old connection")
	}
	if hub.Unregister(oldClient) {
		t.Fatal("stale connection was allowed to unregister current connection")
	}
	if !hub.IsOnline("user", "device") {
		t.Fatal("device became offline when stale connection exited")
	}
	if !hub.Unregister(newClient) {
		t.Fatal("current connection was not unregistered")
	}
	if hub.IsOnline("user", "device") {
		t.Fatal("device remained online after current connection exited")
	}
}
