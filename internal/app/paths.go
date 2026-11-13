package app

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"local/loop/internal/profile"
)

func ProfileDirectory(executable, stateDir, override string) (string, error) {
	if override != "" {
		return readyProfileDirectory(override)
	}
	bundled := filepath.Join(filepath.Dir(executable), "..", "Resources", "profiles")
	if _, err := os.Stat(filepath.Join(bundled, profile.MainConfigName)); err == nil {
		if err := migrateInstalledState(stateDir); err != nil {
			return "", fmt.Errorf("迁移旧版数据：%w", err)
		}
		target := filepath.Join(stateDir, "profiles")
		// Migrate an installed user's own sources before considering bundle defaults.
		legacy := false
		for _, name := range []string{"base.json", "routing.json"} {
			if _, err := os.Stat(filepath.Join(target, name)); err == nil {
				legacy = true
			}
		}
		// An application update must never overwrite the user's routing, nodes,
		// subscription snapshots or current selection. Only seed missing files.
		err := filepath.WalkDir(bundled, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			relative, err := filepath.Rel(bundled, path)
			if err != nil {
				return err
			}
			if legacy && relative == profile.MainConfigName {
				return nil
			}
			destination := filepath.Join(target, relative)
			if entry.IsDir() {
				return os.MkdirAll(destination, 0700)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			file, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
			if os.IsExist(err) {
				return nil
			}
			if err != nil {
				return err
			}
			_, writeErr := file.Write(data)
			closeErr := file.Close()
			if writeErr != nil {
				os.Remove(destination)
				return writeErr
			}
			return closeErr
		})
		if err != nil {
			return "", err
		}
		return readyProfileDirectory(target)
	}
	return readyProfileDirectory("profiles")
}

func readyProfileDirectory(dir string) (string, error) {
	path, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	if err := (profile.Store{Dir: path}).EnsureMainConfig(); err != nil {
		return "", fmt.Errorf("准备主配置: %w", err)
	}
	// A malformed user document remains editable in the GUI.
	if _, err := (profile.Store{Dir: path}).Config(); err == nil {
		if err := (profile.Store{Dir: path}).UpgradePolicyGroups(); err != nil {
			return "", fmt.Errorf("准备策略组: %w", err)
		}
	}
	return path, nil
}

func PrepareProfile(store profile.Store, path string) (string, error) {
	if err := store.WriteRuntime(path); err != nil {
		return "", fmt.Errorf("生成运行配置: %w", err)
	}
	return path, nil
}
