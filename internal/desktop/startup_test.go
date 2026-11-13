package desktop

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2/test"
	"local/miniproxy/internal/autostart"
)

type startupStub struct {
	state             autostart.Status
	readErr, writeErr error
	approval          bool
	reads, opened     int
	writes            []bool
}

func (s *startupStub) Status() (autostart.Status, error) {
	s.reads++
	return s.state, s.readErr
}

func (s *startupStub) SetEnabled(enabled bool) (autostart.Status, error) {
	s.writes = append(s.writes, enabled)
	if s.writeErr == nil {
		s.state = autostart.Disabled
		if enabled {
			s.state = autostart.Enabled
			if s.approval {
				s.state = autostart.RequiresApproval
			}
		}
	}
	return s.state, s.writeErr
}

func (s *startupStub) OpenSettings() { s.opened++ }

func finishStartup(t *testing.T, queue <-chan func()) {
	t.Helper()
	select {
	case complete := <-queue:
		complete()
	case <-time.After(5 * time.Second):
		t.Fatal("login-item operation did not complete")
	}
}

func TestStartupUsesSystemStateAndHandlesApprovalAndFailure(t *testing.T) {
	a := test.NewApp()
	a.Settings().SetTheme(newClientTheme())
	defer a.Quit()
	p := newMorePane(a.Preferences())
	defer p.startup.Close()
	queue := make(chan func(), 8)
	p.startup.post = func(fn func()) { queue <- fn }
	system := &startupStub{state: autostart.Disabled}
	p.startup.attach(system)
	finishStartup(t, queue)
	p.categories.Select(moreStartup)
	finishStartup(t, queue)
	if p.body.Objects[0] != p.startup.root || p.startup.toggle.Checked || len(system.writes) != 0 {
		t.Fatal("opening startup settings changed registration or showed the wrong page/state")
	}
	test.Tap(p.startup.toggle)
	if !p.startup.busy || !p.startup.toggle.Disabled() {
		t.Fatal("registration did not lock the control while work is pending")
	}
	test.Tap(p.startup.toggle)
	finishStartup(t, queue)
	if !p.startup.toggle.Checked || p.startup.message.Visible() || !reflect.DeepEqual(system.writes, []bool{true}) {
		t.Fatal("registration result did not reach the UI or caused a duplicate write")
	}
	// A setting changed outside the app is read back, never overwritten.
	system.state = autostart.Disabled
	p.Refresh()
	finishStartup(t, queue)
	if p.startup.toggle.Checked || len(system.writes) != 1 {
		t.Fatal("external system setting was overwritten by a local preference")
	}
	system.approval = true
	test.Tap(p.startup.toggle)
	finishStartup(t, queue)
	if p.startup.state != autostart.RequiresApproval || !p.startup.message.Visible() || !strings.Contains(p.startup.message.Text, "尚未生效") || !p.startup.settings.Visible() {
		t.Fatal("pending system approval was presented as enabled")
	}
	test.Tap(p.startup.settings)
	if system.opened != 1 {
		t.Fatal("login-item system settings button is not wired")
	}
	// A pending registration must remain cancellable.
	test.Tap(p.startup.toggle)
	finishStartup(t, queue)
	if p.startup.toggle.Checked || p.startup.settings.Visible() || system.state != autostart.Disabled {
		t.Fatal("pending registration could not be cancelled")
	}
	system.writeErr = errors.New("system refused the change")
	test.Tap(p.startup.toggle)
	finishStartup(t, queue)
	if p.startup.toggle.Checked || !p.startup.message.Visible() || !strings.Contains(p.startup.message.Text, system.writeErr.Error()) || p.startup.toggle.Disabled() {
		t.Fatal("failed registration did not restore the actual state and allow retry")
	}
}

func TestStartupUnavailableAndShutdownIgnoreQueuedCompletion(t *testing.T) {
	a := test.NewApp()
	a.Settings().SetTheme(newClientTheme())
	defer a.Quit()
	p := newStartupPane()
	queue := make(chan func(), 4)
	p.post = func(fn func()) { queue <- fn }
	system := &startupStub{state: autostart.Unavailable, readErr: errors.New("open the packaged app")}
	p.attach(system)
	finishStartup(t, queue)
	test.Tap(p.toggle)
	if !p.toggle.Disabled() || len(system.writes) != 0 || !p.message.Visible() {
		t.Fatal("unavailable runtime allowed registration")
	}
	system.state, system.readErr = autostart.Enabled, nil
	p.Refresh()
	select {
	case complete := <-queue:
		p.Close()
		complete()
	case <-time.After(5 * time.Second):
		t.Fatal("system state read did not complete")
	}
	if p.toggle.Checked || p.state != autostart.Unavailable {
		t.Fatal("a queued completion updated a closed pane")
	}
}
