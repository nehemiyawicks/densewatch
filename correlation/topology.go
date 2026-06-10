package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// Topology is the data-center source of truth: which node sits in which rack,
// and which CDU cools that rack. A JSON file today; a NetBox-backed loader slots
// in behind the same struct (NetBox is the data source, not the correlation logic).
type Topology struct {
	Racks []Rack `json:"racks"`
	Nodes []Node `json:"nodes"`
}

type Rack struct {
	Name            string  `json:"name"`
	PowerCapacityKW float64 `json:"power_capacity_kw"`
	CDU             string  `json:"cdu"` // must match the `cdu` label on densewatch_cdu_* metrics
}

type Node struct {
	Host string `json:"host"`
	Rack string `json:"rack"`
}

func loadTopology(path string) (*Topology, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var t Topology
	if err := json.Unmarshal(data, &t); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if len(t.Nodes) == 0 || len(t.Racks) == 0 {
		return nil, fmt.Errorf("topology %s: need at least one rack and one node", path)
	}
	return &t, nil
}

// render emits the topology as Prometheus join-key metrics. densewatch_topology_info
// joins GPU/job metrics (by Hostname) to a rack and CDU; densewatch_rack_power_capacity_kw
// gives the denominator for power-density and headroom.
func render(t *Topology) string {
	rackCDU := make(map[string]string, len(t.Racks))
	for _, r := range t.Racks {
		rackCDU[r.Name] = r.CDU
	}
	var b strings.Builder
	b.WriteString("# HELP densewatch_topology_info node -> rack -> cdu join key (value always 1).\n")
	b.WriteString("# TYPE densewatch_topology_info gauge\n")
	for _, n := range t.Nodes {
		fmt.Fprintf(&b, "densewatch_topology_info{Hostname=%q,rack=%q,cdu=%q} 1\n", n.Host, n.Rack, rackCDU[n.Rack])
	}
	b.WriteString("# HELP densewatch_rack_power_capacity_kw Provisioned power budget per rack (kW).\n")
	b.WriteString("# TYPE densewatch_rack_power_capacity_kw gauge\n")
	for _, r := range t.Racks {
		fmt.Fprintf(&b, "densewatch_rack_power_capacity_kw{rack=%q} %g\n", r.Name, r.PowerCapacityKW)
	}
	return b.String()
}
