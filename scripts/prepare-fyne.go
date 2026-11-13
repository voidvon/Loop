//go:build ignore

// Reconstruct Loop's patched Fyne from the verified Go module cache.
// Run from the repository root, normally through make prepare.
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const patchDir = "third_party/fyne-patches"
const targetDir = "third_party/fyne"
const stampName = ".loop-fyne.json"

type upstream struct{ Module, Version, Sum string }
type stamp struct{ Inputs, Tree string }

func main() {
	refresh := flag.Bool("refresh-patch", false, "save edits in generated Fyne back to loop.patch")
	flag.Parse()
	if err := prepare(*refresh); err != nil {
		fmt.Fprintln(os.Stderr, "prepare-fyne:", err)
		os.Exit(1)
	}
}

func prepare(refresh bool) error {
	data, err := os.ReadFile(filepath.Join(patchDir, "upstream.json"))
	if err != nil {
		return err
	}
	var spec upstream
	if err = json.Unmarshal(data, &spec); err != nil {
		return err
	}
	if spec.Module != "fyne.io/fyne/v2" || spec.Version == "" || spec.Sum == "" {
		return errors.New("invalid pinned Fyne module")
	}
	mod, err := os.ReadFile("go.mod")
	if err != nil {
		return err
	}
	require := `(?m)^\s*` + regexp.QuoteMeta(spec.Module) + `\s+` + regexp.QuoteMeta(spec.Version) + `(?:\s|$)`
	if !regexp.MustCompile(require).Match(mod) {
		return errors.New("go.mod and upstream.json must pin the same Fyne version")
	}
	lock := "third_party/.fyne-prepare.lock"
	if err = os.Mkdir(lock, 0700); err != nil {
		return fmt.Errorf("cannot acquire %s (another prepare may be running): %w", lock, err)
	}
	defer os.Remove(lock)
	inputs, err := inputHash()
	if err != nil {
		return err
	}
	old, readErr := os.ReadFile(filepath.Join(targetDir, stampName))
	var previous stamp
	if readErr == nil {
		if err = json.Unmarshal(old, &previous); err != nil {
			return err
		}
		tree, err := treeHash(targetDir)
		if err != nil {
			return err
		}
		if !refresh && tree != previous.Tree {
			return errors.New("generated Fyne has local edits; run make update-fyne-patch to preserve them before preparing again")
		}
		if !refresh && previous.Inputs == inputs {
			return nil
		}
	} else if !os.IsNotExist(readErr) {
		return readErr
	}
	if !refresh {
		if _, err = os.Stat(targetDir); err == nil && readErr != nil {
			return errors.New("existing third_party/fyne has no preparation stamp; move it aside before preparing, preserving any local changes")
		}
	}
	command := exec.Command("go", "mod", "download", "-json", spec.Module+"@"+spec.Version)
	output, err := command.Output()
	if err != nil {
		return commandError(command, err)
	}
	var module struct{ Dir, Sum, Error string }
	if err = json.Unmarshal(output, &module); err != nil {
		return err
	}
	if module.Error != "" {
		return errors.New(module.Error)
	}
	if module.Sum != spec.Sum || module.Dir == "" {
		return errors.New("downloaded Fyne does not match the pinned module checksum")
	}
	if refresh {
		if _, err = os.Stat(targetDir); err != nil {
			return err
		}
		if err = refreshPatch(module.Dir); err != nil {
			return err
		}
		if err = copyFile(filepath.Join(patchDir, "README.md"), filepath.Join(targetDir, "LOOP_PATCHES.md"), 0644); err != nil {
			return err
		}
		return writeStamp(targetDir)
	}
	staging, err := os.MkdirTemp("third_party", ".fyne-stage-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(staging)
	if err = copyTree(module.Dir, staging); err != nil {
		return err
	}
	command = exec.Command("git", "apply", "--no-index", "--whitespace=error-all", "--directory="+filepath.ToSlash(staging), filepath.Join(patchDir, "loop.patch"))
	if out, err := command.CombinedOutput(); err != nil {
		return fmt.Errorf("apply Fyne patch: %w\n%s", err, out)
	}
	if err = copyFile(filepath.Join(patchDir, "README.md"), filepath.Join(staging, "LOOP_PATCHES.md"), 0644); err != nil {
		return err
	}
	if err = writeStamp(staging); err != nil {
		return err
	}
	backup := staging + "-old"
	if _, err = os.Stat(targetDir); err == nil {
		if err = os.Rename(targetDir, backup); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	if err = os.Rename(staging, targetDir); err != nil {
		if restoreErr := os.Rename(backup, targetDir); restoreErr != nil && !os.IsNotExist(restoreErr) {
			return fmt.Errorf("publish Fyne: %v; restore backup %s: %w", err, backup, restoreErr)
		}
		return err
	}
	if err = os.RemoveAll(backup); err != nil {
		return err
	}
	fmt.Println("Prepared", spec.Module, spec.Version, "with Loop patches")
	return nil
}

func commandError(cmd *exec.Cmd, err error) error {
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return fmt.Errorf("%s: %w\n%s", strings.Join(cmd.Args, " "), err, exit.Stderr)
	}
	return err
}

func inputHash() (string, error) {
	hash := sha256.New()
	for _, name := range []string{"upstream.json", "loop.patch", "README.md"} {
		data, err := os.ReadFile(filepath.Join(patchDir, name))
		if err != nil {
			return "", err
		}
		fmt.Fprintf(hash, "%s\x00%d\x00", name, len(data))
		hash.Write(data)
	}
	data, err := os.ReadFile("scripts/prepare-fyne.go")
	if err != nil {
		return "", err
	}
	hash.Write(data)
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func treeHash(root string) (string, error) {
	hash := sha256.New()
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if rel == stampName {
			return nil
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("unsupported dependency file: %s", path)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		fmt.Fprintf(hash, "%s\x00%d\x00", filepath.ToSlash(rel), info.Size())
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		_, err = io.Copy(hash, f)
		closeErr := f.Close()
		if err != nil {
			return err
		}
		return closeErr
	})
	return hex.EncodeToString(hash.Sum(nil)), err
}

func writeStamp(root string) error {
	inputs, err := inputHash()
	if err != nil {
		return err
	}
	tree, err := treeHash(root)
	if err != nil {
		return err
	}
	data, err := json.Marshal(stamp{Inputs: inputs, Tree: tree})
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(root, stampName), data, 0644)
}

func copyTree(source, target string) error {
	return filepath.WalkDir(source, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		dst := filepath.Join(target, rel)
		if entry.IsDir() {
			return os.MkdirAll(dst, 0755)
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("unsupported module file: %s", path)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		return copyFile(path, dst, info.Mode().Perm()|0200)
	})
}

func copyFile(source, target string, mode fs.FileMode) error {
	data, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	return os.WriteFile(target, data, mode)
}

func refreshPatch(source string) error {
	files := map[string]bool{}
	for _, root := range []string{source, targetDir} {
		if err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				return nil
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			if rel != stampName && rel != "LOOP_PATCHES.md" {
				files[rel] = true
			}
			return nil
		}); err != nil {
			return err
		}
	}
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	var patch bytes.Buffer
	for _, name := range names {
		before, after := filepath.Join(source, name), filepath.Join(targetDir, name)
		a, aerr := os.ReadFile(before)
		b, berr := os.ReadFile(after)
		if aerr != nil && !os.IsNotExist(aerr) {
			return aerr
		}
		if berr != nil && !os.IsNotExist(berr) {
			return berr
		}
		if bytes.Equal(a, b) && (aerr == nil) == (berr == nil) {
			continue
		}
		if aerr != nil {
			before = os.DevNull
		}
		if berr != nil {
			after = os.DevNull
		}
		command := exec.Command("git", "diff", "--no-index", "--binary", "--no-color", "--no-ext-diff", "--no-textconv", "--", before, after)
		data, err := command.Output()
		var exit *exec.ExitError
		if err != nil && (!errors.As(err, &exit) || exit.ExitCode() != 1) {
			return commandError(command, err)
		}
		rel := filepath.ToSlash(name)
		for _, line := range strings.SplitAfter(string(data), "\n") {
			switch {
			case strings.HasPrefix(line, "diff --git "):
				line = "diff --git a/" + rel + " b/" + rel + "\n"
			case strings.HasPrefix(line, "--- "):
				line = "--- a/" + rel + "\n"
				if aerr != nil {
					line = "--- /dev/null\n"
				}
			case strings.HasPrefix(line, "+++ "):
				line = "+++ b/" + rel + "\n"
				if berr != nil {
					line = "+++ /dev/null\n"
				}
			}
			patch.WriteString(line)
		}
	}
	tmp, err := os.CreateTemp(patchDir, ".loop.patch-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err = tmp.Write(patch.Bytes()); err != nil {
		tmp.Close()
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	if err = os.Rename(tmp.Name(), filepath.Join(patchDir, "loop.patch")); err != nil {
		return err
	}
	fmt.Println("Updated Loop Fyne patch:", patch.Len(), "bytes")
	return nil
}
