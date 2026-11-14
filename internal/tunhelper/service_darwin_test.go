//go:build darwin

package tunhelper

import (
	"net"
	"os"
	"path/filepath"
	"testing"
)

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
