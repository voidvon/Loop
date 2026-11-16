//go:build darwin

package tunhelper

import (
	"encoding/xml"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestServiceLaunchPlistUsesPrivateOnDemandSocket(t *testing.T) {
	plist := serviceLaunchPlist(1001)
	if strings.Contains(plist, "KeepAlive") || strings.Contains(plist, "RunAtLoad") {
		t.Fatal("service is configured to stay resident")
	}
	var document any
	if err := xml.Unmarshal([]byte(plist), &document); err != nil {
		t.Fatal(err)
	}
	filename := filepath.Join(t.TempDir(), "service.plist")
	if err := os.WriteFile(filename, []byte(plist), 0600); err != nil {
		t.Fatal(err)
	}
	for key, expected := range map[string]string{
		"Sockets.Control.SockPathName":  ServiceSocket,
		"Sockets.Control.SockPathOwner": "1001",
		"Sockets.Control.SockPathMode":  "384",
	} {
		output, err := exec.Command("/usr/bin/plutil", "-extract", key, "raw", "-o", "-", filename).CombinedOutput()
		if err != nil || strings.TrimSpace(string(output)) != expected {
			t.Fatalf("%s: %s (%v)", key, output, err)
		}
	}
}

func TestServiceUsesKernelPeerCredentials(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "loop-peer-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	listener, err := net.Listen("unix", filepath.Join(dir, "socket"))
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	client, err := net.Dial("unix", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	server, err := listener.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	uid, err := socketUID(server)
	if err != nil || uid != os.Geteuid() {
		t.Fatal("peer identity did not come from the kernel", uid, err)
	}
	if os.Geteuid() != 0 && verifyRootPeer(client) == nil {
		t.Fatal("ordinary-user process impersonated root service")
	}
}
