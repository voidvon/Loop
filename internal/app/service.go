// Package app coordinates client use cases without depending on a GUI toolkit.
package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"local/loop/internal/profile"
	"local/loop/internal/proxy"
)

// Options is shared by the command bootstrap and desktop composition root.
type Options struct {
	Store                                                  profile.Store
	Executable, StateDir, CorePath, ConfigPath, HelperPath string
}

// Service provides validated configuration, node and subscription operations.
// Callers serialize mutations; it has no UI state or long-lived goroutines.
type Service struct {
	Options
	coreErr error
	Remote  RemoteOperations
}

func NewService(options Options) *Service {
	s := &Service{Options: options}
	if s.CorePath == "" {
		s.CorePath, s.coreErr = proxy.FindCore(s.Executable)
	}
	return s
}

func (s *Service) CoreError() error { return s.coreErr }

func (s *Service) Document() (name, path string) {
	if s.ConfigPath != "" {
		return "", s.ConfigPath
	}
	return profile.MainConfigName, filepath.Join(s.Store.Dir, profile.MainConfigName)
}

func (s *Service) ValidateDocument(ctx context.Context, name string, data []byte) error {
	if s.Remote != nil {
		_, err := s.Remote.Execute(ctx, Operation{Action: "validate_document", Name: name, Data: data})
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.coreErr != nil {
		return s.coreErr
	}
	var config map[string]any
	var err error
	if name != "" {
		config, err = s.Store.CompileDocument(name, data)
	} else {
		config, err = profile.ParseDocument(data)
	}
	if err != nil {
		return err
	}
	encoded, err := json.Marshal(config)
	if err != nil {
		return err
	}
	if _, err := proxy.LocalAddress(encoded); err != nil {
		return err
	}
	if err := proxy.CheckConfiguration(ctx, s.CorePath, config); err != nil {
		return err
	}
	return ctx.Err()
}

func (s *Service) SaveDocument(ctx context.Context, data, expected []byte) ([]byte, error) {
	if s.Remote != nil {
		result, err := s.Remote.Execute(ctx, Operation{Action: "save_document", Data: data, Expected: expected})
		return result.Document, err
	}
	name, path := s.Document()
	return profile.SaveDocument(path, data, expected, func(data []byte) error {
		return s.ValidateDocument(ctx, name, data)
	})
}

func (s *Service) ValidateNodes(ctx context.Context, nodes []profile.Node) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.coreErr != nil {
		return s.coreErr
	}
	if len(nodes) == 0 {
		return errors.New("没有可用节点")
	}
	config, err := s.Store.CompileNode(nodes[0])
	if err != nil {
		return err
	}
	outbounds := append([]any(nil), config["outbounds"].([]any)...)
	for i, node := range nodes {
		if err := profile.ValidateNode(node); err != nil {
			return err
		}
		data, err := json.Marshal(node.Outbound)
		if err != nil {
			return err
		}
		var out map[string]any
		if err := json.Unmarshal(data, &out); err != nil {
			return err
		}
		out["tag"] = fmt.Sprintf("node-validate-%d", i)

		delete(out, "domain_resolver")
		outbounds = append(outbounds, out)
	}
	config["outbounds"] = outbounds
	return proxy.CheckConfiguration(ctx, s.CorePath, config)
}

func (s *Service) SaveManual(ctx context.Context, node profile.Node) error {
	if s.Remote != nil {
		_, err := s.Remote.Execute(ctx, Operation{Action: "save_manual", Node: &node})
		return err
	}
	if err := s.ValidateNodes(ctx, []profile.Node{node}); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return s.Store.SaveManual(node)
}

func (s *Service) UpdateSubscription(ctx context.Context, sub profile.Subscription) error {
	if s.Remote != nil {
		_, err := s.Remote.Execute(ctx, Operation{Action: "update_subscription", Subscription: &sub})
		return err
	}
	nodes, err := profile.FetchClash(ctx, sub.URL)
	if err != nil {
		return err
	}
	if err := s.ValidateNodes(ctx, nodes); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	sub.Nodes, sub.Format, sub.UpdatedAt = nodes, "clash", time.Now().UTC().Format(time.RFC3339)
	return s.Store.SaveSubscription(sub)
}
