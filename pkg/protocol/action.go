// Package protocol defines the wire format between qdesk components.
//
// All types serialise to JSON and are shared across the in-sandbox daemon,
// the control plane, and any clients (test runner, AI agent SDKs).
package protocol

// ActionType is the discriminator for the Action union.
type ActionType string

const (
	ActionClick  ActionType = "click"
	ActionType_  ActionType = "type"
	ActionKey    ActionType = "key"
	ActionScroll ActionType = "scroll"
	ActionDrag   ActionType = "drag"
	ActionWait   ActionType = "wait"
)

// MouseButton identifies which mouse button a click uses.
// Default ("") is treated as "left" by the daemon.
type MouseButton string

const (
	MouseLeft   MouseButton = "left"
	MouseMiddle MouseButton = "middle"
	MouseRight  MouseButton = "right"
)

// Point is a 2D coordinate used by Drag.
type Point struct {
	X int `json:"x"`
	Y int `json:"y"`
}

// Action is the request body for POST /actions.
//
// Fields are tagged with omitempty so each action variant only carries the
// fields it needs over the wire. Validation per Type happens in the daemon.
//
// Click:  Type, X, Y, [Button]
// Type:   Type, Text
// Key:    Type, Keys
// Scroll: Type, X, Y, DX, DY
// Drag:   Type, From, To
// Wait:   Type, MS
type Action struct {
	Type ActionType `json:"type"`

	// Click / Scroll cursor position.
	X int `json:"x,omitempty"`
	Y int `json:"y,omitempty"`

	// Click button.
	Button MouseButton `json:"button,omitempty"`

	// Type text payload.
	Text string `json:"text,omitempty"`

	// Key combo (e.g. ["ctrl","s"]).
	Keys []string `json:"keys,omitempty"`

	// Scroll deltas (vertical primary).
	DX int `json:"dx,omitempty"`
	DY int `json:"dy,omitempty"`

	// Drag endpoints.
	From *Point `json:"from,omitempty"`
	To   *Point `json:"to,omitempty"`

	// Wait duration in milliseconds.
	MS uint64 `json:"ms,omitempty"`
}

// ActionResult is the response body for POST /actions.
type ActionResult struct {
	OK            bool `json:"ok"`
	ScreenChanged bool `json:"screen_changed"`
}

// HealthResponse is the response body for GET /health.
type HealthResponse struct {
	Status  string `json:"status"`
	Version string `json:"version"`
}
