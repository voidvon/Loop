// Package autostart controls the current application's macOS login item.
// The operating system is the source of truth; no proxy profile stores this state.
package autostart

type Status int

const (
	Unavailable Status = iota
	Disabled
	Enabled
	RequiresApproval
)

// Service only changes registration after an explicit SetEnabled call.
type Service struct{}
