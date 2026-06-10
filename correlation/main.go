// Command densewatch-correlate reads the data-center topology (the source of
// truth - a JSON file today, NetBox-pluggable next) and emits it as Prometheus
// join-key metrics, so PromQL can attribute GPU jobs to racks, power, and CDUs:
//
//	sum by (rack) (DCGM_FI_DEV_POWER_USAGE * on(Hostname) group_left(rack) densewatch_topology_info)
//
// This is the correlation layer - it turns "GPU power and CDU heat side by side"
// into "this job is loading this rack, whose CDU is at X% of capacity".
package main

import (
	"flag"
	"io"
	"log"
	"net/http"
)

func main() {
	topoPath := flag.String("topology", "correlation/topology.json", "topology source-of-truth JSON file")
	listen := flag.String("listen", ":9840", "metrics listen address")
	flag.Parse()

	if _, err := loadTopology(*topoPath); err != nil { // fail fast on a bad file
		log.Fatalf("topology: %v", err)
	}

	http.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		t, err := loadTopology(*topoPath) // reload per scrape so edits apply without a restart
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		_, _ = io.WriteString(w, render(t))
	})
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("ok\n")) })

	log.Printf("densewatch-correlate  →  http://localhost%s/metrics  (topology: %s)", *listen, *topoPath)
	log.Fatal(http.ListenAndServe(*listen, nil))
}
