// Command densewatch-cdu is a Prometheus exporter for CDU / liquid-cooling
// telemetry. It scrapes CDUs over Redfish (DSP2064 CoolingUnit) AND Modbus-TCP
// and normalizes both into one unified densewatch_cdu_* metric schema. Read-only.
package main

import (
	"crypto/subtle"
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

// version is stamped at build time via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	// Subcommand: `densewatch-cdu probe <redfish-url>` runs the conformance
	// probe. With no subcommand it runs the exporter (serve), the default.
	if len(os.Args) >= 2 && os.Args[1] == "probe" {
		fs := flag.NewFlagSet("probe", flag.ExitOnError)
		insecure := fs.Bool("insecure-skip-verify", false, "skip TLS certificate verification (for self-signed BMC/CDU certs)")
		caCert := fs.String("ca-cert", "", "path to a CA bundle for TLS verification")
		_ = fs.Parse(os.Args[2:])
		if fs.NArg() < 1 {
			log.Fatal("usage: densewatch-cdu probe [-insecure-skip-verify] [-ca-cert FILE] <redfish-url>\n" +
				"credentials: embed in the URL (https://user:pass@host/...) or set REDFISH_USERNAME / REDFISH_PASSWORD")
		}
		os.Exit(runProbe(fs.Arg(0), *insecure, *caCert))
	}

	var redfish, modbus stringSlice
	flag.Var(&redfish, "redfish", "Redfish CoolingUnit URL to scrape (repeatable)")
	flag.Var(&modbus, "modbus", "Modbus-TCP CDU address host:port to scrape (repeatable)")
	listen := flag.String("listen", "127.0.0.1:9839", "metrics listen address (use a routable address only behind your own controls)")
	timeout := flag.Duration("timeout", 5*time.Second, "per-CDU scrape timeout")
	insecure := flag.Bool("insecure-skip-verify", false, "skip Redfish TLS certificate verification (for self-signed BMC/CDU certs)")
	caCert := flag.String("ca-cert", "", "path to a CA bundle for Redfish TLS verification")
	authToken := flag.String("auth-token", "", "if set, require 'Authorization: Bearer <token>' on /metrics")
	modbusProfilePath := flag.String("modbus-profile", "", "path to a JSON Modbus register-map profile (default: built-in sim profile)")
	flag.Parse()

	if len(redfish) == 0 && len(modbus) == 0 {
		log.Fatal("no targets: pass at least one -redfish <url> or -modbus <host:port>")
	}

	// One HTTP client per Redfish target: a shared TLS transport with per-target
	// Basic auth (from the URL userinfo or REDFISH_USERNAME / REDFISH_PASSWORD).
	tr, err := baseTransport(*caCert, *insecure)
	if err != nil {
		log.Fatal(err)
	}
	type rfTarget struct {
		url    string
		client *http.Client
	}
	rfTargets := make([]rfTarget, 0, len(redfish))
	for i, u := range redfish {
		cleanURL, user, pass, err := redfishCreds(u)
		if err != nil {
			log.Fatalf("redfish target #%d: %v", i+1, err) // error is credential-free
		}
		rfTargets = append(rfTargets, rfTarget{cleanURL, redfishHTTPClient(*timeout, tr, user, pass)})
	}

	modbusProf := simModbusProfile
	if *modbusProfilePath != "" {
		p, err := loadModbusProfile(*modbusProfilePath)
		if err != nil {
			log.Fatal(err)
		}
		modbusProf = p
		log.Printf("modbus profile %q loaded (%d registers)", modbusProf.Name, len(modbusProf.Fields))
	}

	metrics := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		rs := make([]Reading, 0, len(rfTargets)+len(modbus))
		for _, t := range rfTargets {
			start := time.Now()
			rd := collectRedfish(t.url, t.client)
			rd.ScrapeSeconds = time.Since(start).Seconds()
			rs = append(rs, rd)
		}
		for _, a := range modbus {
			start := time.Now()
			rd := collectModbus(a, *timeout, modbusProf)
			rd.ScrapeSeconds = time.Since(start).Seconds()
			rs = append(rs, rd)
		}
		var b strings.Builder
		renderAll(&b, rs)
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		_, _ = io.WriteString(w, b.String())
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
	log.Printf("densewatch-cdu  →  http://%s/metrics  (%d redfish, %d modbus targets)", *listen, len(rfTargets), len(modbus))
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
