// Command densewatch-cdu is a Prometheus exporter for CDU / liquid-cooling
// telemetry. It scrapes CDUs over Redfish (DSP2064 CoolingUnit) AND Modbus-TCP
// and normalizes both into one unified densewatch_cdu_* metric schema. Read-only.
package main

import (
	"flag"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

type stringSlice []string

func (s *stringSlice) String() string { return strings.Join(*s, ",") }
func (s *stringSlice) Set(v string) error {
	*s = append(*s, v)
	return nil
}

func main() {
	// Subcommand: `densewatch-cdu probe <redfish-url>` runs the conformance
	// probe. With no subcommand it runs the exporter (serve), the default.
	if len(os.Args) >= 2 && os.Args[1] == "probe" {
		if len(os.Args) < 3 {
			log.Fatal("usage: densewatch-cdu probe <redfish-url>")
		}
		os.Exit(runProbe(os.Args[2]))
	}

	var redfish, modbus stringSlice
	flag.Var(&redfish, "redfish", "Redfish CoolingUnit URL to scrape (repeatable)")
	flag.Var(&modbus, "modbus", "Modbus-TCP CDU address host:port to scrape (repeatable)")
	listen := flag.String("listen", ":9839", "metrics listen address")
	timeout := flag.Duration("timeout", 5*time.Second, "per-CDU scrape timeout")
	flag.Parse()

	if len(redfish) == 0 && len(modbus) == 0 {
		log.Fatal("no targets: pass at least one -redfish <url> or -modbus <host:port>")
	}
	client := &http.Client{Timeout: *timeout}

	http.HandleFunc("/metrics", func(w http.ResponseWriter, req *http.Request) {
		rs := make([]Reading, 0, len(redfish)+len(modbus))
		for _, u := range redfish {
			start := time.Now()
			rd := collectRedfish(u, client)
			rd.ScrapeSeconds = time.Since(start).Seconds()
			rs = append(rs, rd)
		}
		for _, a := range modbus {
			start := time.Now()
			rd := collectModbus(a, *timeout)
			rd.ScrapeSeconds = time.Since(start).Seconds()
			rs = append(rs, rd)
		}
		var b strings.Builder
		renderAll(&b, rs)
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		_, _ = io.WriteString(w, b.String())
	})
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("ok\n")) })

	log.Printf("densewatch-cdu  →  http://localhost%s/metrics  (%d redfish, %d modbus targets)", *listen, len(redfish), len(modbus))
	log.Fatal(http.ListenAndServe(*listen, nil))
}
