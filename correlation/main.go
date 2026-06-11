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
	"crypto/subtle"
	"flag"
	"io"
	"log"
	"net/http"
	"time"
)

func main() {
	topoPath := flag.String("topology", "correlation/topology.json", "topology source-of-truth JSON file")
	listen := flag.String("listen", "127.0.0.1:9840", "metrics listen address (use a routable address only behind your own controls)")
	authToken := flag.String("auth-token", "", "if set, require 'Authorization: Bearer <token>' on /metrics")
	flag.Parse()

	if _, err := loadTopology(*topoPath); err != nil { // fail fast on a bad file
		log.Fatalf("topology: %v", err)
	}

	metrics := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t, err := loadTopology(*topoPath) // reload per scrape so edits apply without a restart
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		_, _ = io.WriteString(w, render(t))
	})

	mux := http.NewServeMux()
	mux.Handle("/metrics", requireToken(*authToken, metrics))
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("ok\n")) })

	srv := &http.Server{
		Addr:              *listen,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	log.Printf("densewatch-correlate  →  http://%s/metrics  (topology: %s)", *listen, *topoPath)
	log.Fatal(srv.ListenAndServe())
}

// requireToken wraps a handler so that, when token is non-empty, requests must
// carry "Authorization: Bearer <token>". Empty token means no auth (the default).
func requireToken(token string, next http.Handler) http.Handler {
	if token == "" {
		return next
	}
	want := "Bearer " + token
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte(want)) != 1 {
			w.Header().Set("WWW-Authenticate", "Bearer")
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}
