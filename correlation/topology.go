package main

import (
	"encoding/json"
	"fmt"
	"io"
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

// maxTopologyBytes bounds the topology file read: it is small (racks + nodes), and
// a local read with no timeout must not slurp a huge or runaway file into memory.
const maxTopologyBytes = 1 << 20 // 1 MiB

func loadTopology(path string) (*Topology, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	// Read at most the cap + 1 byte, so we can tell "at the limit" from "over it".
	data, err := io.ReadAll(io.LimitReader(f, maxTopologyBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxTopologyBytes {
		return nil, fmt.Errorf("topology %s: file too large (> %d bytes)", path, maxTopologyBytes)
	}
	var t Topology
	if err := json.Unmarshal(data, &t); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if len(t.Nodes) == 0 || len(t.Racks) == 0 {
		return nil, fmt.Errorf("topology %s: need at least one rack and one node", path)
	}
	// Enforce the join invariant at load time: every node must map to a known rack,
	// and every rack to a CDU. Otherwise render() silently emits cdu="" and the
	// PromQL attribution drops that node's series with no error surfaced anywhere.
	rackCDU := make(map[string]string, len(t.Racks))
	for _, r := range t.Racks {
		rackCDU[r.Name] = r.CDU
	}
	for _, n := range t.Nodes {
		switch cdu, ok := rackCDU[n.Rack]; {
		case !ok:
			return nil, fmt.Errorf("topology %s: node %q references unknown rack %q", path, n.Host, n.Rack)
		case cdu == "":
			return nil, fmt.Errorf("topology %s: rack %q (node %q) has no cdu mapping", path, n.Rack, n.Host)
		}
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
		fmt.Fprintf(&b, "densewatch_topology_info{Hostname=%s,rack=%s,cdu=%s} 1\n", escapeLabel(n.Host), escapeLabel(n.Rack), escapeLabel(rackCDU[n.Rack]))
	}
	b.WriteString("# HELP densewatch_rack_power_capacity_kw Provisioned power budget per rack (kW).\n")
	b.WriteString("# TYPE densewatch_rack_power_capacity_kw gauge\n")
	for _, r := range t.Racks {
		fmt.Fprintf(&b, "densewatch_rack_power_capacity_kw{rack=%s} %g\n", escapeLabel(r.Name), r.PowerCapacityKW)
	}
	return b.String()
}

// escapeLabel renders s as a Prometheus exposition label value: double-quoted, with
// only \\, \" and \n escaped and other control characters dropped. Node/rack/cdu
// names come from operator-authored JSON; this keeps an exotic value (e.g. a tab)
// from producing invalid exposition that Prometheus would reject.
func escapeLabel(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 2)
	b.WriteByte('"')
	for _, r := range s {
		switch {
		case r == '\\':
			b.WriteString(`\\`)
		case r == '"':
			b.WriteString(`\"`)
		case r == '\n':
			b.WriteString(`\n`)
		case r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f):
			// drop other C0/C1 control characters
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}
