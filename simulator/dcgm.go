package main

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// dcgmServer emits dcgm-exporter-style Prometheus metrics, including the
// hpc_job label that is densewatch's canonical correlation key (see spec §5-B).
type dcgmServer struct{ model *loadModel }

func (s *dcgmServer) metrics(w http.ResponseWriter, r *http.Request) {
	gs := s.model.gpuSamples(time.Now())
	var b strings.Builder

	metric := func(name, help string, val func(gpuSample) float64) {
		fmt.Fprintf(&b, "# HELP %s %s\n# TYPE %s gauge\n", name, help, name)
		for _, g := range gs {
			fmt.Fprintf(&b,
				"%s{gpu=\"%d\",UUID=\"%s\",Hostname=\"%s\",modelName=\"NVIDIA B200\",hpc_job=\"%s\"} %g\n",
				name, g.GPU, g.UUID, g.Node.Host, g.Node.Job, val(g))
		}
	}
	metric("DCGM_FI_DEV_POWER_USAGE", "GPU power draw (W).", func(g gpuSample) float64 { return g.PowerW })
	metric("DCGM_FI_DEV_GPU_TEMP", "GPU temperature (C).", func(g gpuSample) float64 { return g.TempC })
	metric("DCGM_FI_DEV_GPU_UTIL", "GPU utilization (%).", func(g gpuSample) float64 { return g.UtilPct })
	metric("DCGM_FI_DEV_SM_CLOCK", "SM clock (MHz).", func(g gpuSample) float64 { return g.ClockM })

	// node→rack topology: stands in for the NetBox source-of-truth until the
	// M3 correlation layer joins it properly. Real dcgm-exporter has no rack label.
	b.WriteString("# HELP densewatch_node_rack_info node→rack topology (sim stand-in for NetBox).\n")
	b.WriteString("# TYPE densewatch_node_rack_info gauge\n")
	seen := map[string]bool{}
	for _, g := range gs {
		if seen[g.Node.Host] {
			continue
		}
		seen[g.Node.Host] = true
		fmt.Fprintf(&b, "densewatch_node_rack_info{Hostname=\"%s\",rack=\"%s\"} 1\n", g.Node.Host, g.Node.Rack)
	}

	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	_, _ = io.WriteString(w, b.String())
}
