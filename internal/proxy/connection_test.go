package proxy

import (
	"context"
	"net"
	"testing"
)

func TestDirectConfigurationNeedsOnlyLocalListener(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	config := []byte(`{"outbounds":[{"type":"direct","tag":"direct"}]}`)
	if err := CheckConfiguredConnection(context.Background(), listener.Addr().String(), config); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := CheckConfiguredConnection(ctx, listener.Addr().String(), config); err == nil {
		t.Fatal("ignored cancellation")
	}
	if err := CheckConfiguredConnection(context.Background(), "127.0.0.1:0", config); err == nil {
		t.Fatal("missing local core accepted")
	}
	if err := CheckConfiguredConnection(context.Background(), listener.Addr().String(), []byte("invalid")); err == nil {
		t.Fatal("invalid configuration accepted")
	}
}
