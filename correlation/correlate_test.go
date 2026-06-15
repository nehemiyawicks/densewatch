package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadAndRenderTopology(t *testing.T) {
	topo, err := loadTopology("topology.json")
	if err != nil {
		t.Fatal(err)
	}
	if len(topo.Nodes) != 8 {
		t.Fatalf("expected 8 nodes, got %d", len(topo.Nodes))
	}
	out := render(topo)
	for _, want := range []string{
		`densewatch_topology_info{Hostname="node01",rack="rack-A1",cdu="1"} 1`,
		`densewatch_topology_info{Hostname="node05",rack="rack-A2",cdu="1"} 1`,
		`densewatch_rack_power_capacity_kw{rack="rack-A1"} 132`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

// TestTopologyIntegrity guards the join: every node must point at a known rack,
// and every rack must map to a CDU - otherwise the PromQL attribution silently drops series.
func TestTopologyIntegrity(t *testing.T) {
	topo, err := loadTopology("topology.json")
	if err != nil {
		t.Fatal(err)
	}
	cdu := make(map[string]string)
	for _, r := range topo.Racks {
		cdu[r.Name] = r.CDU
	}
	for _, n := range topo.Nodes {
		c, ok := cdu[n.Rack]
		if !ok {
			t.Errorf("node %s references unknown rack %q", n.Host, n.Rack)
		}
		if c == "" {
			t.Errorf("rack %q (node %s) has no CDU mapping", n.Rack, n.Host)
		}
	}
}

// TestTopologyFileSizeCap: an oversized topology file is rejected before parse,
// so a runaway or hostile file cannot be slurped into memory.
func TestTopologyFileSizeCap(t *testing.T) {
	path := filepath.Join(t.TempDir(), "big.json")
	big := append([]byte(`{"racks":[],"nodes":[],"pad":"`), make([]byte, maxTopologyBytes)...)
	big = append(big, '"', '}')
	if err := os.WriteFile(path, big, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadTopology(path); err == nil || !strings.Contains(err.Error(), "too large") {
		t.Fatalf("expected a 'too large' error, got %v", err)
	}
}

// TestTopologyRejectsOrphanNode: loadTopology fails fast when a node points at a
// rack that doesn't exist, rather than silently emitting cdu="" and breaking the join.
func TestTopologyRejectsOrphanNode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "orphan.json")
	js := `{"racks":[{"name":"rack-A1","power_capacity_kw":132,"cdu":"1"}],
	        "nodes":[{"host":"node01","rack":"rack-A1"},{"host":"node02","rack":"rack-ZZ"}]}`
	if err := os.WriteFile(path, []byte(js), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadTopology(path); err == nil || !strings.Contains(err.Error(), "unknown rack") {
		t.Fatalf("expected an 'unknown rack' error, got %v", err)
	}
}
