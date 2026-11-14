package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"local/loop/internal/profile"
	"local/loop/internal/proxy"
	"local/loop/internal/testutil"
)

func TestBenchmarkResolvesNodeKeysAndPreservesSavedChoices(t *testing.T) {
	core, err := exec.LookPath("sing-box")
	if err != nil {
		t.Skip("sing-box is not installed")
	}
	store := testutil.NewStore(t)
	if err := profile.WriteJSON(filepath.Join(store.Dir, "nodes.json"), profile.NodeList{Nodes: []profile.Node{{ID: "a", Name: "Unavailable Test Node", Outbound: map[string]any{"type": "http", "server": "127.0.0.1", "server_port": 1}}}}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveMode(profile.ModeDirect); err != nil {
		t.Fatal(err)
	}
	service := NewService(Options{Store: store, CorePath: core})
	nodes, err := service.BenchmarkNodes()
	if err != nil || len(nodes) != 1 || nodes[0].Name != "Unavailable Test Node" {
		t.Fatal(nodes, err)
	}
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	defer origin.Close()
	options := proxy.BenchmarkOptions{URL: origin.URL, Requests: 5, Concurrency: 2, Timeout: time.Second}
	before := routingStoreFiles(t, store.Dir)
	for _, keys := range [][]string{nil, {nodes[0].Key}} {
		report, err := service.RunBenchmark(context.Background(), BenchmarkRequest{Options: options, NodeKeys: keys}, nil)
		if err != nil || len(report.Results) != 1 {
			t.Fatal(report, err)
		}
		r := report.Results[0]
		if len(keys) == 0 {
			if r.Success != 5 || !strings.Contains(r.Name, "全局直连") {
				t.Fatal(r)
			}
		} else if r.NetworkFailures != 5 || r.Name != nodes[0].Name || r.Source != "手动节点" {
			t.Fatal("explicit target ignored", r)
		}
	}
	if !reflect.DeepEqual(before, routingStoreFiles(t, store.Dir)) {
		t.Fatal("benchmark changed configuration or choices")
	}
	for _, keys := range [][]string{{"missing"}, {nodes[0].Key, nodes[0].Key}} {
		if _, err := service.RunBenchmark(context.Background(), BenchmarkRequest{Options: options, NodeKeys: keys}, nil); err == nil {
			t.Fatal("accepted invalid target selection", keys)
		}
	}
}
