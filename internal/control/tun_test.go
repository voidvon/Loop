package control

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"local/loop/internal/app"
)

func TestLiveTUNSwitchAcrossRPCSurvivesFrontendCancellation(t *testing.T) {
	for _, previous := range []bool{false, true} {
		for _, fail := range []bool{false, true} {
			t.Run(fmt.Sprintf("from-tun-%t/fail-%t", previous, fail), func(t *testing.T) {
				backend := &testBackend{running: true, failNext: fail, entered: make(chan struct{}, 1), release: make(chan struct{})}
				c := newTestController(t, backend)
				if err := c.Service.Store.SaveTUN(previous); err != nil {
					t.Fatal(err)
				}
				c.refresh(true, nil)
				client, _ := startTestServer(t, c)
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				finished := make(chan error, 1)
				go func() {
					_, err := client.Execute(ctx, app.Operation{Action: "set_tun", Enabled: boolValue(!previous)})
					finished <- err
				}()
				select {
				case <-backend.entered:
				case <-time.After(3 * time.Second):
					t.Fatal("live TUN switch never reached restart")
				}
				if !c.Snapshot().Busy {
					t.Fatal("switch did not publish busy state")
				}
				if _, err := c.Execute(context.Background(), app.Operation{Action: "set_tun", Enabled: boolValue(previous)}); !errors.Is(err, ErrBusy) {
					t.Fatal("concurrent TUN switch was accepted", err)
				}
				cancel()
				if err := <-finished; err == nil {
					t.Fatal("cancelled frontend did not return")
				}
				close(backend.release)
				waitFor(t, func() bool { return !c.Snapshot().Busy })
				second, err := NewClient(c.Service.StateDir)
				if err != nil {
					t.Fatal(err)
				}
				defer second.Close()
				state, err := second.Status(context.Background())
				wantTUN, wantStarts, wantStops := !previous, 1, 1
				if fail {
					wantTUN, wantStarts, wantStops = previous, 2, 2
				}
				if err != nil || !state.Running || state.TUN != wantTUN || (state.Error != "") != fail {
					t.Fatal("frontend exit lost switch or recovery state", state, err)
				}
				if starts, stops := backend.counts(); starts != wantStarts || stops != wantStops {
					t.Fatal("wrong switch/recovery lifecycle", starts, stops)
				}
			})
		}
	}
}
