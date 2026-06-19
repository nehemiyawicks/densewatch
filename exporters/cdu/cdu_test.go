package main

import (
	"encoding/binary"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRenderAllUnifiedSchema(t *testing.T) {
	supply, flow := 32.0, 64.2
	r := Reading{CDU: "cdu1", Protocol: "redfish", Vendor: "v", Model: "m", Up: true, SupplyTempC: &supply, FlowLPM: &flow}
	var b strings.Builder
	renderAll(&b, []Reading{r})
	out := b.String()

	for _, want := range []string{
		`densewatch_cdu_up{cdu="cdu1",protocol="redfish"} 1`,
		`densewatch_cdu_coolant_supply_temp_celsius{cdu="cdu1",protocol="redfish"} 32`,
		`densewatch_cdu_coolant_flow_lpm{cdu="cdu1",protocol="redfish"} 64.2`,
		`# TYPE densewatch_cdu_heat_removed_kw gauge`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	// A nil field must render as an absent metric, not a fake zero.
	if strings.Contains(out, "densewatch_cdu_pump_speed_percent{") {
		t.Error("pump metric should be absent when the field is nil")
	}
}

func TestCollectRedfishFollowsLinks(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/cdu", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"Id":"1","Manufacturer":"Test","Model":"X","CoolingCapacityWatts":125600,
			"SecondaryCoolantConnectors":{"@odata.id":"/sec"},"EnvironmentMetrics":{"@odata.id":"/env"}}`)
	})
	mux.HandleFunc("/sec", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"Members":[{"@odata.id":"/sec/1"}]}`)
	})
	mux.HandleFunc("/sec/1", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"SupplyTemperatureCelsius":{"Reading":32},"FlowLitersPerMinute":{"Reading":64.2},"HeatRemovedkW":{"Reading":44.8}}`)
	})
	mux.HandleFunc("/env", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"TemperatureCelsius":{"Reading":27},"DewPointCelsius":{"Reading":21.5}}`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	r := collectRedfish(srv.URL+"/cdu", srv.Client())
	if !r.Up {
		t.Fatal("expected Up")
	}
	if r.SupplyTempC == nil || *r.SupplyTempC != 32 {
		t.Errorf("supply temp: %v", r.SupplyTempC)
	}
	if r.HeatRemovedkW == nil || *r.HeatRemovedkW != 44.8 {
		t.Errorf("heat removed: %v", r.HeatRemovedkW)
	}
	if r.CapacitykW == nil || *r.CapacitykW != 125.6 {
		t.Errorf("capacity: %v", r.CapacitykW)
	}
	if r.InletTempC == nil || *r.InletTempC != 27 {
		t.Errorf("inlet temp: %v", r.InletTempC)
	}
	// Pumps weren't served → must stay absent.
	if r.PumpPct != nil {
		t.Errorf("pump should be absent, got %v", *r.PumpPct)
	}
}

func TestCollectModbusViaProfile(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		req := make([]byte, 12) // MBAP(7) + PDU(FC4,start,qty = 5)
		if _, err := io.ReadFull(c, req); err != nil {
			return
		}
		regs := []uint16{320, 420, 100, 642, 448, 356, 3497, 2165, 850, 270, 720, 215, 0}
		body := []byte{0x04, byte(len(regs) * 2)}
		for _, v := range regs {
			body = append(body, byte(v>>8), byte(v))
		}
		out := make([]byte, 7+len(body))
		copy(out[0:4], req[0:4]) // echo TxID + Proto
		binary.BigEndian.PutUint16(out[4:6], uint16(len(body)+1))
		out[6] = req[6] // echo UnitID
		copy(out[7:], body)
		_, _ = c.Write(out)
	}()

	r := collectModbus(ln.Addr().String(), 2*time.Second, simModbusProfile)
	if !r.Up {
		t.Fatal("expected Up")
	}
	if r.SupplyTempC == nil || *r.SupplyTempC != 32 { // 320 × 0.1
		t.Errorf("supply temp: %v", r.SupplyTempC)
	}
	if r.FlowLPM == nil || *r.FlowLPM != 64.2 { // 642 × 0.1
		t.Errorf("flow: %v", r.FlowLPM)
	}
	if r.HeatRemovedkW == nil || *r.HeatRemovedkW != 44.8 { // 448 × 0.1
		t.Errorf("heat: %v", r.HeatRemovedkW)
	}
	// Pressure now carries the same 0.1 precision as the Redfish path (no integer truncation).
	if r.SupplykPa == nil || *r.SupplykPa != 349.7 { // 3497 × 0.1
		t.Errorf("supply pressure: %v", r.SupplykPa)
	}
}

// TestRedfishBasicAuth: real BMCs require auth. Without it the scrape fails; with
// it (from the URL userinfo) auth is applied to every request, including the
// @odata.id link we follow to read a value.
func TestRedfishBasicAuth(t *testing.T) {
	guard := func(h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if u, p, ok := r.BasicAuth(); !ok || u != "admin" || p != "secret" {
				w.Header().Set("WWW-Authenticate", "Basic")
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			h(w, r)
		}
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/cdu", guard(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"Id":"1","SecondaryCoolantConnectors":{"@odata.id":"/sec"}}`)
	}))
	mux.HandleFunc("/sec", guard(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"Members":[{"@odata.id":"/sec/1"}]}`)
	}))
	mux.HandleFunc("/sec/1", guard(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"FlowLitersPerMinute":{"Reading":50}}`)
	}))
	srv := httptest.NewServer(mux)
	defer srv.Close()

	if collectRedfish(srv.URL+"/cdu", redfishHTTPClient(2*time.Second, &http.Transport{}, "", "")).Up {
		t.Error("expected Up=false without credentials")
	}
	cleanURL, user, pass, err := redfishCreds(strings.Replace(srv.URL, "http://", "http://admin:secret@", 1) + "/cdu")
	if err != nil {
		t.Fatal(err)
	}
	r := collectRedfish(cleanURL, redfishHTTPClient(2*time.Second, &http.Transport{}, user, pass))
	if !r.Up {
		t.Fatal("expected Up=true with credentials")
	}
	if r.FlowLPM == nil || *r.FlowLPM != 50 {
		t.Errorf("flow via authenticated followed link: %v", r.FlowLPM)
	}
}

// TestRedfishTLSSelfSigned: real BMCs serve HTTPS with self-signed certs. They are
// rejected by default and reachable with -insecure-skip-verify.
func TestRedfishTLSSelfSigned(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"Id":"1","Manufacturer":"Test"}`)
	}))
	defer srv.Close()

	trVerify, _ := baseTransport("", false)
	if collectRedfish(srv.URL, redfishHTTPClient(2*time.Second, trVerify, "", "")).Up {
		t.Error("expected Up=false against a self-signed cert without -insecure-skip-verify")
	}
	trInsecure, _ := baseTransport("", true)
	if !collectRedfish(srv.URL, redfishHTTPClient(2*time.Second, trInsecure, "", "")).Up {
		t.Error("expected Up=true with -insecure-skip-verify")
	}
}

// TestRedfishOversizedBodyRejected: a hostile/buggy endpoint must not be able to
// stream an unbounded body into memory; getJSON caps it and fails cleanly.
func TestRedfishOversizedBodyRejected(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, strings.Repeat("x", maxRedfishBodyBytes+1024))
	}))
	defer srv.Close()
	if _, err := getJSON(srv.Client(), srv.URL); err == nil || !strings.Contains(err.Error(), "too large") {
		t.Fatalf("expected a 'too large' error, got %v", err)
	}
}

// TestRedfishCredsRejectsNonHTTP: non-http(s) and malformed URLs are rejected up
// front, before any network request is attempted.
func TestRedfishCredsRejectsNonHTTP(t *testing.T) {
	for _, bad := range []string{"ftp://host/redfish", "file:///etc/passwd", "://nohost", "redfish/v1"} {
		if _, _, _, err := redfishCreds(bad); err == nil {
			t.Errorf("expected rejection for %q, got nil", bad)
		}
	}
	if _, _, _, err := redfishCreds("https://bmc.example/redfish/v1"); err != nil {
		t.Errorf("valid https URL should be accepted: %v", err)
	}
}

// TestRenderAllEscapesLabels: a remote CDU's Id/Vendor/Model may carry control
// bytes; they must be dropped/escaped so the exposition stays parseable (a raw ESC
// or tab via %q would make Prometheus reject the whole scrape).
func TestRenderAllEscapesLabels(t *testing.T) {
	var b strings.Builder
	renderAll(&b, []Reading{{
		CDU:      "cdu\x1b[31m\t1",
		Protocol: "redfish",
		Vendor:   "Ac\x1bme\"Co",
		Model:    "Z\\9",
		Up:       true,
	}})
	out := b.String()
	for _, raw := range []string{"\x1b", "\t", "\r"} {
		if strings.Contains(out, raw) {
			t.Errorf("raw control byte %q leaked into exposition:\n%s", raw, out)
		}
	}
	if !strings.Contains(out, `vendor="Acme\"Co"`) {
		t.Errorf("vendor not escaped per Prometheus rules:\n%s", out)
	}
	if !strings.Contains(out, `model="Z\\9"`) {
		t.Errorf("model backslash not escaped:\n%s", out)
	}
}

// TestCoolantPairReversed: a supply warmer than the return (beyond a sensor margin)
// indicates reversed supply/return; an absent temperature yields no metric at all.
func TestCoolantPairReversed(t *testing.T) {
	cold, warm := 30.0, 40.0
	if v, ok := (Reading{SupplyTempC: &cold, ReturnTempC: &warm}).coolantPairReversed(); !ok || v {
		t.Errorf("healthy loop (supply<return) should not be reversed: v=%v ok=%v", v, ok)
	}
	if v, ok := (Reading{SupplyTempC: &warm, ReturnTempC: &cold}).coolantPairReversed(); !ok || !v {
		t.Errorf("reversed loop (supply>return) should be flagged: v=%v ok=%v", v, ok)
	}
	if _, ok := (Reading{SupplyTempC: &cold}).coolantPairReversed(); ok {
		t.Error("missing return temp should yield no metric (ok=false)")
	}
}

// TestParseModbusProfile: a JSON vendor profile resolves field names to setters and
// computes the register count; unknown field names are rejected (not silently dropped).
func TestParseModbusProfile(t *testing.T) {
	p, err := parseModbusProfile([]byte(`{"name":"Acme X","registers":[
		{"field":"supply_temp_c","addr":0,"scale":0.1},
		{"field":"flow_lpm","addr":3,"scale":0.1}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "Acme X" || len(p.Fields) != 2 || p.qty() != 4 {
		t.Errorf("unexpected profile: name=%q fields=%d qty=%d", p.Name, len(p.Fields), p.qty())
	}
	if _, err := parseModbusProfile([]byte(`{"registers":[{"field":"nonsense","addr":0,"scale":1}]}`)); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Errorf("expected an unknown-field error, got %v", err)
	}
	if _, err := parseModbusProfile([]byte(`{"name":"empty","registers":[]}`)); err == nil {
		t.Error("expected an error for a profile with no registers")
	}
	// the shipped example profile must always stay valid and loadable
	if p, err := loadModbusProfile("profiles/example.json"); err != nil || len(p.Fields) != 13 {
		t.Errorf("profiles/example.json should load with 13 fields: err=%v fields=%d", err, len(p.Fields))
	}
}
