package proxy

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func mockSystem(t *testing.T) (*systemProxy, *[][]string) {
	t.Helper()
	calls := [][]string{}
	p := &systemProxy{
		journal:  filepath.Join(t.TempDir(), "state.json"),
		services: func() ([]string, error) { return []string{"Wi-Fi"}, nil },
	}
	p.run = func(args ...string) (string, error) {
		calls = append(calls, append([]string(nil), args...))
		switch args[0] {
		case "-getautoproxyurl":
			return "URL: (null)\nEnabled: No", nil
		case "-getproxyautodiscovery":
			return "Auto Proxy Discovery: Off", nil
		case "-getwebproxy":
			return "Enabled: Yes\nServer: old.example\nPort: 8888\nAuthenticated Proxy Enabled: 0", nil
		case "-getsecurewebproxy", "-getsocksfirewallproxy":
			return "Enabled: No\nServer: 127.0.0.1\nPort: 7890\nAuthenticated Proxy Enabled: 0", nil
		}
		return "", nil
	}
	return p, &calls
}

func TestProxyRestoresEnabledAndDisabledSettings(t *testing.T) {
	p, calls := mockSystem(t)
	if err := p.enable("127.0.0.1:2080"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p.journal); err != nil {
		t.Fatal("missing recovery journal", err)
	}
	*calls = nil
	if err := p.restore(); err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		{"-setwebproxy", "Wi-Fi", "old.example", "8888", "off"},
		{"-setwebproxystate", "Wi-Fi", "on"},
		{"-setsecurewebproxy", "Wi-Fi", "127.0.0.1", "7890", "off"},
		{"-setsecurewebproxystate", "Wi-Fi", "off"},
		{"-setsocksfirewallproxy", "Wi-Fi", "127.0.0.1", "7890", "off"},
		{"-setsocksfirewallproxystate", "Wi-Fi", "off"},
	}
	if !reflect.DeepEqual(*calls, want) {
		t.Fatalf("restored settings mismatch:\n%v\nwant %v", *calls, want)
	}
	if _, err := os.Stat(p.journal); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("journal must be removed after successful restoration")
	}
}

func TestPartialEnableRollsBack(t *testing.T) {
	p, calls := mockSystem(t)
	run := p.run
	p.run = func(args ...string) (string, error) {
		if args[0] == "-setsecurewebproxy" && args[3] == "2080" {
			return "", errors.New("injected failure")
		}
		return run(args...)
	}
	if err := p.enable("127.0.0.1:2080"); err == nil {
		t.Fatal("expected failure")
	}
	if _, err := os.Stat(p.journal); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("rollback must finish and remove the journal")
	}
	found := false
	for _, call := range *calls {
		if strings.Join(call, " ") == "-setwebproxy Wi-Fi old.example 8888 off" {
			found = true
		}
	}
	if !found {
		t.Fatal("original HTTP proxy was not restored")
	}
}

func TestFailedRestoreRetainsJournalForRetry(t *testing.T) {
	p, _ := mockSystem(t)
	if err := p.enable("127.0.0.1:2080"); err != nil {
		t.Fatal(err)
	}
	run := p.run
	p.run = func(...string) (string, error) { return "", errors.New("denied") }
	if err := p.restore(); err == nil {
		t.Fatal("expected restoration error")
	}
	if _, err := os.Stat(p.journal); err != nil {
		t.Fatal("journal must survive a failed restore", err)
	}
	p.run = run
	if err := p.restore(); err != nil {
		t.Fatal("retry failed", err)
	}
}

func TestInstanceLockRejectsSecondOwnerAndReleases(t *testing.T) {
	path := filepath.Join(t.TempDir(), "client.lock")
	first, err := acquireLock(path)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	if second, err := acquireLock(path); err == nil {
		second.Close()
		t.Fatal("second client must not restore an active client's proxy settings")
	}
	first.Close()
	third, err := acquireLock(path)
	if err != nil {
		t.Fatal("lock must be available after close", err)
	}
	third.Close()
}
