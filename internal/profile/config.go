package profile

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const MainConfigName = "config.json"

// Config reads the user-owned main configuration, never the generated runtime
// configuration. Legacy files are read only when no main configuration exists.
func (s Store) Config() (map[string]any, error) {
	if data, ok := s.documents[MainConfigName]; ok {
		return parseConfig(data)
	}
	data, err := os.ReadFile(filepath.Join(s.Dir, MainConfigName))
	if errors.Is(err, os.ErrNotExist) {
		return s.legacyConfig()
	}
	if err != nil {
		return nil, err
	}
	return parseConfig(data)
}

func parseConfig(data []byte) (map[string]any, error) {
	config, err := ParseDocument(data)
	if err != nil {
		return nil, err
	}
	if err := onlyKeys(config, MainConfigName, "log", "inbounds", "experimental", "route", "dns", "http_clients", "policy_groups"); err != nil {
		return nil, err
	}
	return config, nil
}

func (s Store) legacyConfig() (map[string]any, error) {
	config := map[string]any{}
	for _, source := range []struct {
		name string
		keys []string
	}{
		{"base.json", []string{"log", "inbounds", "experimental"}},
		{"routing.json", []string{"route", "dns", "http_clients"}},
	} {
		data, err := os.ReadFile(filepath.Join(s.Dir, source.name))
		if err != nil {
			return nil, err
		}
		value, err := ParseDocument(data)
		if err != nil {
			return nil, fmt.Errorf("迁移 %s: %w", source.name, err)
		}
		if err := onlyKeys(value, source.name, source.keys...); err != nil {
			return nil, err
		}
		for key, item := range value {
			config[key] = item
		}
	}
	return config, nil
}

// EnsureMainConfig migrates the two former sources once. It retains the legacy
// files as backups and never overwrites an existing main file, even if invalid.
func (s Store) EnsureMainConfig() error {
	path := filepath.Join(s.Dir, MainConfigName)
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	config, err := s.legacyConfig()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	// Publish a complete private file without replacing one another process
	// may have created during migration.
	f, err := os.CreateTemp(s.Dir, ".loop-migrate-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(append(data, '\n')); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Link(f.Name(), path); err != nil && !errors.Is(err, os.ErrExist) {
		return err
	}
	return nil
}
