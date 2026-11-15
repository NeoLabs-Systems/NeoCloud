package notify

// Show sends one desktop notification now. The caller dedupes (the engine's
// notices go through Watch and its gate; a send to this device is told once
// per offer by internal/handoff).
func Show(title, body string) error { return send(Message{Title: title, Body: body}) }
