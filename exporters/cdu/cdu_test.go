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

	r := collectModbus(ln.Addr().String(), 2*time.Second)
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
	cleanURL, user, pass := redfishCreds(strings.Replace(srv.URL, "http://", "http://admin:secret@", 1) + "/cdu")
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
