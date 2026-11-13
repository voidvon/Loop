package app

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"local/loop/internal/profile"
	"local/loop/internal/testutil"
)

type modeTestBackend struct {
	store       profile.Store
	starts      []profile.Mode
	startErrors []error
	stops       int
	stopError   error
}

func (b *modeTestBackend) Start(ctx context.Context) error {
	mode, err := b.store.Mode()
	if err != nil {
		return err
	}
	b.starts = append(b.starts, mode)
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(b.startErrors) > 0 {
		err := b.startErrors[0]
		b.startErrors = b.startErrors[1:]
		return err
	}
	return nil
}
func (b *modeTestBackend) Stop() error { b.stops++; return b.stopError }
func TestModeSwitchReconnectsAndRollsBack(t *testing.T) {
	for _, test := range []struct {
		name        string
		failures    []error
		wantMode    profile.Mode
		wantStarts  []profile.Mode
		wantRunning bool
		wantError   bool
	}{
		{"success", nil, profile.ModeGlobal, []profile.Mode{profile.ModeGlobal}, true, false},
		{"rollback", []error{errors.New("unavailable"), nil}, profile.ModeRule, []profile.Mode{profile.ModeGlobal, profile.ModeRule}, true, true},
		{"rollback cannot reconnect", []error{errors.New("new failed"), errors.New("old failed")}, profile.ModeRule, []profile.Mode{profile.ModeGlobal, profile.ModeRule}, false, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			s := testutil.NewStore(t)
			backend := &modeTestBackend{store: s, startErrors: test.failures}
			running, err := SwitchMode(context.Background(), backend, s, profile.ModeGlobal, true)
			if running != test.wantRunning || (err != nil) != test.wantError {
				t.Fatal("unexpected result", running, err)
			}
			if got, _ := s.Mode(); got != test.wantMode {
				t.Fatal("wrong persisted mode", got)
			}
			if len(backend.starts) != len(test.wantStarts) {
				t.Fatal("wrong restarts", backend.starts)
			}
			for i, mode := range test.wantStarts {
				if backend.starts[i] != mode {
					t.Fatal("started with wrong mode", backend.starts)
				}
			}
		})
	}
}
func TestModeSwitchWhileStoppedAndInvalidSelection(t *testing.T) {
	s := testutil.NewStore(t)
	backend := &modeTestBackend{store: s}
	if running, err := SwitchMode(context.Background(), backend, s, profile.ModeDirect, false); running || err != nil {
		t.Fatal(running, err)
	}
	if backend.stops != 0 || len(backend.starts) != 0 {
		t.Fatal("offline switch touched backend")
	}
	if err := profile.WriteJSON(filepath.Join(s.Dir, "selection.json"), profile.Selection{}); err != nil {
		t.Fatal(err)
	}
	if running, err := SwitchMode(context.Background(), backend, s, profile.ModeGlobal, true); !running || err == nil {
		t.Fatal("invalid candidate should leave active direct mode untouched", running, err)
	}
	if backend.stops != 0 {
		t.Fatal("stopped before validating candidate")
	}
	if mode, _ := s.Mode(); mode != profile.ModeDirect {
		t.Fatal("failed preflight changed mode")
	}
}
func TestModeSwitchStopFailureAndCancellation(t *testing.T) {
	s := testutil.NewStore(t)
	backend := &modeTestBackend{store: s, stopError: errors.New("restore failed")}
	if running, err := SwitchMode(context.Background(), backend, s, profile.ModeDirect, true); running || err == nil {
		t.Fatal(running, err)
	}
	if mode, _ := s.Mode(); mode != profile.ModeRule {
		t.Fatal("stop failure changed mode")
	}
	if len(backend.starts) != 0 {
		t.Fatal("started despite failed cleanup")
	}
	backend.stopError = nil
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if running, err := SwitchMode(ctx, backend, s, profile.ModeDirect, true); running || err == nil {
		t.Fatal(running, err)
	}
	if mode, _ := s.Mode(); mode != profile.ModeRule {
		t.Fatal("cancellation failed to restore mode")
	}
}
