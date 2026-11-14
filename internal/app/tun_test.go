package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"local/loop/internal/profile"
	"local/loop/internal/proxy"
	"local/loop/internal/testutil"
)

type tunTestBackend struct {
	store       profile.Store
	starts      []bool
	stops       int
	startErrors []error
	stopErrors  []error
	onStart     func()
}

func (b *tunTestBackend) Start(ctx context.Context) error {
	config, err := b.store.Compile()
	if err != nil {
		return err
	}
	data, _ := json.Marshal(config)
	b.starts = append(b.starts, proxy.HasTUN(data))
	if b.onStart != nil {
		b.onStart()
	}
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

func (b *tunTestBackend) Stop() error {
	b.stops++
	if len(b.stopErrors) > 0 {
		err := b.stopErrors[0]
		b.stopErrors = b.stopErrors[1:]
		return err
	}
	return nil
}

func TestLiveTUNSwitchReconnectsAndRecoversInBothDirections(t *testing.T) {
	startupErr, recoveryErr := errors.New("startup or authorization failed"), errors.New("recovery failed")
	for _, mode := range []profile.Mode{profile.ModeRule, profile.ModeGlobal, profile.ModeDirect} {
		for _, previous := range []bool{false, true} {
			for _, tc := range []struct {
				name        string
				failures    []error
				wantRunning bool
				wantStops   int
			}{
				{"success", nil, true, 1},
				{"restore connection", []error{startupErr, nil}, true, 2},
				{"recovery fails", []error{startupErr, recoveryErr}, false, 3},
			} {
				t.Run(fmt.Sprintf("%s/from-tun-%t/%s", mode, previous, tc.name), func(t *testing.T) {
					s := testutil.NewStore(t)
					if err := s.SaveMode(mode); err != nil {
						t.Fatal(err)
					}
					if err := s.SaveTUN(previous); err != nil {
						t.Fatal(err)
					}
					before := map[string][]byte{}
					for _, name := range []string{profile.MainConfigName, "nodes.json", "subscriptions.json", "selection.json"} {
						before[name], _ = os.ReadFile(filepath.Join(s.Dir, name))
					}
					backend := &tunTestBackend{store: s, startErrors: append([]error(nil), tc.failures...)}
					running, err := SwitchTUN(context.Background(), backend, s, !previous, true)
					failed := len(tc.failures) > 0
					if running != tc.wantRunning || (err != nil) != failed || (failed && !errors.Is(err, startupErr)) {
						t.Fatal("wrong switch/recovery result", running, err)
					}
					wantTUN, wantStarts := !previous, []bool{!previous}
					if failed {
						wantTUN, wantStarts = previous, append(wantStarts, previous)
					}
					if got, err := s.TUN(); err != nil || got != wantTUN {
						t.Fatal("wrong persisted capture setting", got, err)
					}
					if !reflect.DeepEqual(backend.starts, wantStarts) || backend.stops != tc.wantStops {
						t.Fatal("wrong capture lifecycle", backend.starts, backend.stops)
					}
					if got, err := s.Mode(); err != nil || got != mode {
						t.Fatal("TUN switch changed routing mode", got, err)
					}
					for name, want := range before {
						got, _ := os.ReadFile(filepath.Join(s.Dir, name))
						if !bytes.Equal(got, want) {
							t.Fatal("TUN switch changed profile", name)
						}
					}
				})
			}
		}
	}
}

func TestTUNSwitchStoppedAndUnchangedDoNotRestart(t *testing.T) {
	s := testutil.NewStore(t)
	backend := &tunTestBackend{store: s}
	if running, err := SwitchTUN(context.Background(), backend, s, true, false); running || err != nil {
		t.Fatal(running, err)
	}
	if enabled, err := s.TUN(); !enabled || err != nil {
		t.Fatal("stopped switch was not saved", enabled, err)
	}
	if running, err := SwitchTUN(context.Background(), backend, s, true, true); !running || err != nil {
		t.Fatal(running, err)
	}
	if backend.stops != 0 || len(backend.starts) != 0 {
		t.Fatal("unchanged or stopped switch touched backend")
	}
}

func TestTUNSwitchRejectsInvalidConfigurationBeforeStopping(t *testing.T) {
	s := testutil.NewStore(t)
	if err := os.WriteFile(filepath.Join(s.Dir, profile.MainConfigName), []byte("invalid"), 0600); err != nil {
		t.Fatal(err)
	}
	backend := &tunTestBackend{store: s}
	if running, err := SwitchTUN(context.Background(), backend, s, true, true); !running || err == nil {
		t.Fatal("invalid preflight interrupted connection", running, err)
	}
	if enabled, err := s.TUN(); enabled || err != nil || backend.stops != 0 || len(backend.starts) != 0 {
		t.Fatal("invalid preflight changed capture", enabled, err)
	}
}

func TestTUNSwitchStopsRecoveryWhenCleanupFailsOrDaemonExits(t *testing.T) {
	for _, name := range []string{"stop failure", "cleanup failure", "daemon exits", "already cancelled"} {
		t.Run(name, func(t *testing.T) {
			s := testutil.NewStore(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			backend := &tunTestBackend{store: s}
			wantStarts, wantStops, wantRunning := []bool(nil), 1, false
			sentinel := errors.New(name)
			switch name {
			case "stop failure":
				backend.stopErrors = []error{sentinel}
			case "cleanup failure":
				backend.startErrors = []error{errors.New("startup failed")}
				backend.stopErrors = []error{nil, sentinel}
				wantStarts, wantStops = []bool{true}, 2
			case "daemon exits":
				backend.onStart = cancel
				sentinel = context.Canceled
				wantStarts, wantStops = []bool{true}, 2
			case "already cancelled":
				cancel()
				sentinel = context.Canceled
				wantStops, wantRunning = 0, true
			}
			if running, err := SwitchTUN(ctx, backend, s, true, true); running != wantRunning || !errors.Is(err, sentinel) {
				t.Fatal("wrong cleanup/cancellation result", running, err)
			}
			if enabled, err := s.TUN(); enabled || err != nil {
				t.Fatal("original capture setting was not retained", enabled, err)
			}
			if !reflect.DeepEqual(backend.starts, wantStarts) || backend.stops != wantStops {
				t.Fatal("restarted after cleanup failure or cancellation", backend.starts, backend.stops)
			}
		})
	}
}
