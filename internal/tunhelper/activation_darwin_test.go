//go:build darwin && cgo

package tunhelper

import (
	"context"
	"encoding/xml"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Opt in because this test temporarily registers a launchd job in the current
// login session. It never installs a privileged service or changes networking.
func TestLaunchdSocketActivation(t *testing.T) {
	if os.Getenv("LOOP_TEST_LAUNCHD") != "1" {
		t.Skip("set LOOP_TEST_LAUNCHD=1 to test against launchd")
	}
	dir, err := os.MkdirTemp("/tmp", "loop-activation-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	escape := func(value string) string {
		var escaped strings.Builder
		if err := xml.EscapeText(&escaped, []byte(value)); err != nil {
			t.Fatal(err)
		}
		return escaped.String()
	}
	label := fmt.Sprintf("local.loop.tun.test.%d", os.Getpid())
	socket := filepath.Join(dir, "control.sock")
	marker := filepath.Join(dir, "runs")
	plist := serviceLaunchPlist(os.Getuid())
	plist = strings.Replace(plist, "<key>Label</key><string>"+ServiceLabel+"</string>", "<key>Label</key><string>"+label+"</string>", 1)
	plist = strings.ReplaceAll(plist, ServiceSocket, escape(socket))
	plist = strings.Replace(plist, "<array><string>"+ServiceExecutable+"</string><string>-service</string></array>",
		"<array><string>"+escape(executable)+"</string><string>-test.run=^TestLaunchdSocketActivationHelper$</string></array>", 1)
	plist = strings.Replace(plist, "</dict></plist>", "<key>EnvironmentVariables</key><dict><key>LOOP_TUN_ACTIVATION_PROBE</key><string>"+escape(marker)+"</string></dict></dict></plist>", 1)
	plist = strings.ReplaceAll(plist, ServiceDirectory+"/service.log", escape(filepath.Join(dir, "service.log")))
	filename := filepath.Join(dir, label+".plist")
	if err := os.WriteFile(filename, []byte(plist), 0600); err != nil {
		t.Fatal(err)
	}
	domain := fmt.Sprintf("gui/%d", os.Getuid())
	target := domain + "/" + label
	if output, err := exec.Command("/bin/launchctl", "bootstrap", domain, filename).CombinedOutput(); err != nil {
		t.Fatalf("bootstrap: %s (%v)", output, err)
	}
	defer func() {
		if output, err := exec.Command("/bin/launchctl", "bootout", target).CombinedOutput(); err != nil {
			t.Errorf("bootout: %s (%v)", output, err)
		}
	}()
	for i := 1; i <= 2; i++ {
		status, err := serviceStatusAt(context.Background(), socket, func(net.Conn) error { return nil })
		if err != nil || status.Protocol != ServiceProtocol {
			log, _ := os.ReadFile(filepath.Join(dir, "service.log"))
			t.Fatalf("activation %d: %+v (%v), log: %s", i, status, err, log)
		}
		deadline := time.Now().Add(8 * time.Second)
		for {
			output, err := exec.Command("/bin/launchctl", "print", target).CombinedOutput()
			if err != nil {
				t.Fatalf("print: %s (%v)", output, err)
			}
			if strings.Contains(string(output), "state = not running") {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("launchd service stayed resident")
			}
			time.Sleep(50 * time.Millisecond)
		}
		info, err := os.Stat(socket)
		if err != nil || info.Mode()&os.ModeSocket == 0 || info.Mode().Perm() != 0600 {
			t.Fatal("private launchd socket did not survive idle exit", info, err)
		}
		runs, err := os.ReadFile(marker)
		if err != nil || len(strings.Fields(string(runs))) != i {
			t.Fatalf("expected %d separate activations, got %q (%v)", i, runs, err)
		}
	}
}

func TestLaunchdSocketActivationHelper(t *testing.T) {
	marker := os.Getenv("LOOP_TUN_ACTIVATION_PROBE")
	if marker == "" {
		t.Skip("subprocess used by TestLaunchdSocketActivation")
	}
	file, err := os.OpenFile(marker, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, err = fmt.Fprintln(file, os.Getpid())
	file.Close()
	if err != nil {
		t.Fatal(err)
	}
	listener, err := activatedServiceListener()
	if err != nil {
		t.Fatal(err)
	}
	err = serveService(context.Background(), listener, serviceInstallation{UID: os.Getuid()}, "unused", socketUID,
		func() error { return nil }, nil, 100*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
}
