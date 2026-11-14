package app

import (
	"context"
	"time"

	"local/loop/internal/profile"
	"local/loop/internal/proxy"
)

type TrayPreferences struct {
	ShowModeLetter  bool `json:"show_mode_letter"`
	DimDisconnected bool `json:"dim_disconnected"`
	ShowPolicies    bool `json:"show_policies"`
}

func DefaultTrayPreferences() TrayPreferences {
	return TrayPreferences{true, true, true}
}

// Operation is shared by the desktop, CLI, and local daemon protocol.
// Paths and executable names are never accepted from a running client's RPC.
type Operation struct {
	Action       string                `json:"action"`
	Enabled      *bool                 `json:"enabled,omitempty"`
	Mode         profile.Mode          `json:"mode,omitempty"`
	Group        string                `json:"group,omitempty"`
	Key          string                `json:"key,omitempty"`
	Name         string                `json:"name,omitempty"`
	ID           string                `json:"id,omitempty"`
	URL          string                `json:"url,omitempty"`
	Data         []byte                `json:"data,omitempty"`
	Expected     []byte                `json:"expected,omitempty"`
	Node         *profile.Node         `json:"node,omitempty"`
	Subscription *profile.Subscription `json:"subscription,omitempty"`
	Targets      []proxy.ProbeTarget   `json:"targets,omitempty"`
	Tray         *TrayPreferences      `json:"tray,omitempty"`
	Benchmark    *BenchmarkRequest     `json:"benchmark,omitempty"`
}

type LatencyValue struct {
	Key       string        `json:"key"`
	Duration  time.Duration `json:"duration"`
	Error     string        `json:"error,omitempty"`
	Timeout   bool          `json:"timeout,omitempty"`
	Cancelled bool          `json:"cancelled,omitempty"`
}

type OperationResult struct {
	Document  []byte                `json:"document,omitempty"`
	Rules     RuleSaveResult        `json:"rules"`
	Ports     DocumentSaveResult    `json:"ports"`
	Policies  DocumentSaveResult    `json:"policies"`
	Latencies []LatencyValue        `json:"latencies,omitempty"`
	Routing   proxy.RoutingResult   `json:"routing,omitempty"`
	Benchmark proxy.BenchmarkReport `json:"benchmark,omitempty"`
}

type RemoteOperations interface {
	Execute(context.Context, Operation) (OperationResult, error)
}

func (s *Service) DeleteManual(id string) error {
	if s.Remote != nil {
		_, err := s.Remote.Execute(context.Background(), Operation{Action: "delete_manual", ID: id})
		return err
	}
	return s.Store.DeleteManual(id)
}

func (s *Service) DeleteSubscription(id string) error {
	if s.Remote != nil {
		_, err := s.Remote.Execute(context.Background(), Operation{Action: "delete_subscription", ID: id})
		return err
	}
	return s.Store.DeleteSubscription(id)
}
