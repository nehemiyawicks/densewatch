package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestProbeCoverage serves a deliberately PARTIAL CoolingUnit (some properties
// present, many absent) and checks the probe scores coverage correctly - the
// whole point of the tool is detecting which DSP2064 properties a unit serves.
func TestProbeCoverage(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/cdu", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"@odata.type":"#CoolingUnit.v1_2_0.CoolingUnit","EquipmentType":"CDU",
			"Manufacturer":"Acme","Model":"Z","CoolingCapacityWatts":100000,
			"SecondaryCoolantConnectors":{"@odata.id":"/sec"},"EnvironmentMetrics":{"@odata.id":"/env"},
			"Actions":{"#CoolingUnit.SetMode":{"target":"/x"}}}`)
	})
	mux.HandleFunc("/sec", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"Members":[{"@odata.id":"/sec/1"}]}`)
	})
	mux.HandleFunc("/sec/1", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"SupplyTemperatureCelsius":{"Reading":30},"FlowLitersPerMinute":{"Reading":50}}`)
	})
	mux.HandleFunc("/env", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"TemperatureCelsius":{"Reading":25}}`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	res, err := probeCoolingUnit(srv.Client(), srv.URL+"/cdu")
	if err != nil {
		t.Fatal(err)
	}
	if res.total != 16 {
		t.Fatalf("expected 16 checks, got %d", res.total)
	}
	// Served: EquipmentType, CoolingCapacityWatts, SetMode, SupplyTemp, Flow, EnvTemperature = 6
	if res.present != 6 {
		t.Errorf("expected 6 present, got %d", res.present)
	}

	got := map[string]bool{}
	for _, c := range res.checks {
		got[c.name] = c.present
	}
	if !got["SecondaryConnector.SupplyTemperatureCelsius"] {
		t.Error("SupplyTemperatureCelsius should be present")
	}
	if got["SecondaryConnector.HeatRemovedkW"] {
		t.Error("HeatRemovedkW should be flagged missing")
	}
	if got["Pump.PumpSpeedPercent"] {
		t.Error("Pump should be flagged missing (no Pumps link served)")
	}
}

// TestProbeNonRedfish: a non-Redfish endpoint must error (→ NO REDFISH verdict).
func TestProbeNonRedfish(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "# HELP not_json a prometheus metric\nnot_json 1\n")
	}))
	defer srv.Close()
	if _, err := probeCoolingUnit(srv.Client(), srv.URL); err == nil {
		t.Fatal("expected an error probing a non-Redfish endpoint")
	}
}

// TestSanitizeTerminalString: fields from a remote Redfish endpoint must not be
// able to inject ANSI escapes, carriage returns, or fake lines into the terminal.
func TestSanitizeTerminalString(t *testing.T) {
	in := "Vendor\x1b[31mRED\x1b[0m\r\nFAKE LINE\x07\x7f end"
	out := sanitizeTerminalString(in)
	for _, ctrl := range []string{"\x1b", "\r", "\n", "\x07", "\x7f"} {
		if strings.Contains(out, ctrl) {
			t.Errorf("control byte %q survived sanitizing: %q", ctrl, out)
		}
	}
	if want := "Vendor[31mRED[0mFAKE LINE end"; out != want {
		t.Errorf("got %q, want %q", out, want)
	}
}
