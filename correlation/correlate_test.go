package main

import (
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
