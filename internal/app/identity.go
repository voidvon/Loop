package app

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
)

const (
	Name        = "loop"
	DisplayName = "Loop"
	ID          = "local.loop.desktop"
	// Kept only to import an existing installation after the rename.
	legacyName = "MiniProxy"
	legacyID   = "local.miniproxy.desktop"
)

func DefaultStateDirectory() (string, error) {
	root, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, Name), nil
}

// Import legacy data only for the default installed profile. Custom state and
// profile directories remain independent. The old installation stays intact.
func migrateInstalledState(stateDir string) error {
	defaultDir, err := DefaultStateDirectory()
	if err != nil {
		return err
	}
	absolute, err := filepath.Abs(stateDir)
	if err != nil {
		return err
	}
	if absolute != defaultDir {
		return nil
	}
	return migrateLegacyState(filepath.Dir(defaultDir))
}

func migrateLegacyState(configRoot string) error {
	source, target := filepath.Join(configRoot, legacyName), filepath.Join(configRoot, Name)
	marker := filepath.Join(target, ".legacy-imported")
	if _, err := os.Stat(marker); err == nil {
		return nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if _, err := os.Stat(source); errors.Is(err, fs.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	if err := os.MkdirAll(target, 0700); err != nil {
		return err
	}
	oldProfiles, newProfiles := filepath.Join(source, "profiles"), filepath.Join(target, "profiles")
	if _, err := os.Lstat(newProfiles); errors.Is(err, fs.ErrNotExist) {
		if info, err := os.Stat(oldProfiles); err == nil {
			if !info.IsDir() {
				return fmt.Errorf("旧配置路径不是目录")
			}
			staging, err := os.MkdirTemp(target, ".profiles-import-*")
			if err != nil {
				return err
			}
			defer os.RemoveAll(staging)
			if err := copyPrivateTree(oldProfiles, staging); err != nil {
				return fmt.Errorf("迁移旧配置：%w", err)
			}
			if err := os.Rename(staging, newProfiles); err != nil {
				// A concurrent launcher may have completed the same import.
				if _, existsErr := os.Stat(newProfiles); existsErr != nil {
					return err
				}
			}
		} else if !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	} else if err != nil {
		return err
	}
	if err := copyPrivateFileIfMissing(filepath.Join(source, "desktop.json"), filepath.Join(target, "desktop.json")); err != nil {
		return err
	}
	// Runtime files, locks and proxy restoration journals belong to the old
	// process and must never be inherited by the renamed daemon.
	return os.WriteFile(marker, []byte("Imported legacy profiles and desktop preferences.\n"), 0600)
}

func copyPrivateTree(source, target string) error {
	return filepath.WalkDir(source, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		destination := filepath.Join(target, relative)
		if entry.IsDir() {
			return os.MkdirAll(destination, 0700)
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("无法迁移非普通配置文件：%s", relative)
		}
		return copyPrivateFileIfMissing(path, destination)
	})
}

// Publish a complete private file without replacing a preference already saved
// by loop, including when two launchers perform the import simultaneously.
func copyPrivateFileIfMissing(source, target string) error {
	if _, err := os.Lstat(target); err == nil {
		return nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	data, err := os.ReadFile(source)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(target), ".loop-import-*")
	if err != nil {
		return err
	}
	defer os.Remove(temporary.Name())
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Link(temporary.Name(), target); err != nil && !errors.Is(err, fs.ErrExist) {
		return err
	}
	return nil
}

// MigrateDesktopPreferences runs before Fyne loads the new application ID.
func MigrateDesktopPreferences() error {
	root, err := os.UserConfigDir()
	if err != nil {
		return err
	}
	if runtime.GOOS == "darwin" {
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		root = filepath.Join(home, "Library", "Preferences")
	}
	return migrateDesktopPreferences(filepath.Join(root, "fyne"))
}

func migrateDesktopPreferences(root string) error {
	return copyPrivateFileIfMissing(filepath.Join(root, legacyID, "preferences.json"), filepath.Join(root, ID, "preferences.json"))
}
