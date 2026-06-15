package main

import (
	"fmt"
	"strings"
)

// Reading is the unified CDU telemetry schema that EVERY protocol collector
// (Redfish, Modbus, later SNMP/BACnet) normalizes into. Pointer fields are nil
// when a given unit doesn't expose that value - so heterogeneous / partial CDU
// coverage renders as simply-absent metrics rather than fake zeros. This single
// schema across protocols is densewatch's core differentiator.
type Reading struct {
	CDU           string
	Protocol      string // "redfish" | "modbus"
	Vendor        string
	Model         string
	Up            bool
	ScrapeSeconds float64

	SupplyTempC   *float64
	ReturnTempC   *float64
	DeltaTempC    *float64
	FlowLPM       *float64
	HeatRemovedkW *float64
	SupplykPa     *float64
	ReturnkPa     *float64

	PumpPct      *float64
	ReservoirPct *float64
	CapacitykW   *float64

	InletTempC  *float64
	HumidityPct *float64
	DewPointC   *float64

	LeakDetected *bool
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// escapeLabel renders s as a Prometheus exposition label value: double-quoted, with
// only \\, \" and \n escaped and every other control character (C0, C1, DEL)
// dropped. Vendor/Model/Id arrive from a remote CDU and are untrusted - a raw
// control byte emitted via %q (e.g. \x1b or \t) is invalid exposition and would
// make Prometheus reject the entire scrape, not just one series.
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
			// drop other C0/C1 controls (CR, tab, ESC, NEL, CSI, ...)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// cduGauges drives both the exposition and the schema's documentation in one place.
var cduGauges = []struct {
	name, help string
	get        func(Reading) *float64
}{
	{"coolant_supply_temp_celsius", "CDU secondary supply coolant temperature (C).", func(r Reading) *float64 { return r.SupplyTempC }},
	{"coolant_return_temp_celsius", "CDU secondary return coolant temperature (C).", func(r Reading) *float64 { return r.ReturnTempC }},
	{"coolant_delta_temp_celsius", "CDU secondary loop delta-T (C).", func(r Reading) *float64 { return r.DeltaTempC }},
	{"coolant_flow_lpm", "CDU secondary coolant flow (L/min).", func(r Reading) *float64 { return r.FlowLPM }},
	{"heat_removed_kw", "Heat removed by the CDU (kW).", func(r Reading) *float64 { return r.HeatRemovedkW }},
	{"coolant_supply_pressure_kpa", "CDU supply pressure (kPa).", func(r Reading) *float64 { return r.SupplykPa }},
	{"coolant_return_pressure_kpa", "CDU return pressure (kPa).", func(r Reading) *float64 { return r.ReturnkPa }},
	{"pump_speed_percent", "CDU pump speed (%).", func(r Reading) *float64 { return r.PumpPct }},
	{"reservoir_level_percent", "CDU reservoir coolant level (%).", func(r Reading) *float64 { return r.ReservoirPct }},
	{"cooling_capacity_kw", "CDU rated cooling capacity (kW).", func(r Reading) *float64 { return r.CapacitykW }},
	{"inlet_temp_celsius", "CDU air inlet temperature (C).", func(r Reading) *float64 { return r.InletTempC }},
	{"humidity_percent", "CDU relative humidity (%).", func(r Reading) *float64 { return r.HumidityPct }},
	{"dew_point_celsius", "CDU dew point (C).", func(r Reading) *float64 { return r.DewPointC }},
}

// renderAll writes Prometheus exposition: one HELP/TYPE block per metric, with
// every target's series grouped beneath it (the format Prometheus expects). All
// label values go through escapeLabel so untrusted remote strings can't corrupt it.
func renderAll(b *strings.Builder, rs []Reading) {
	b.WriteString("# HELP densewatch_cdu_build_info densewatch-cdu build version (value always 1).\n# TYPE densewatch_cdu_build_info gauge\n")
	fmt.Fprintf(b, "densewatch_cdu_build_info{version=%s} 1\n", escapeLabel(version))
	b.WriteString("# HELP densewatch_cdu_up 1 if the CDU scrape succeeded, else 0.\n# TYPE densewatch_cdu_up gauge\n")
	for _, r := range rs {
		fmt.Fprintf(b, "densewatch_cdu_up{cdu=%s,protocol=%s} %d\n", escapeLabel(r.CDU), escapeLabel(r.Protocol), b2i(r.Up))
	}

	b.WriteString("# HELP densewatch_cdu_scrape_duration_seconds Time taken to scrape the CDU.\n# TYPE densewatch_cdu_scrape_duration_seconds gauge\n")
	for _, r := range rs {
		fmt.Fprintf(b, "densewatch_cdu_scrape_duration_seconds{cdu=%s,protocol=%s} %g\n", escapeLabel(r.CDU), escapeLabel(r.Protocol), r.ScrapeSeconds)
	}

	b.WriteString("# HELP densewatch_cdu_info CDU metadata (value is always 1).\n# TYPE densewatch_cdu_info gauge\n")
	for _, r := range rs {
		fmt.Fprintf(b, "densewatch_cdu_info{cdu=%s,protocol=%s,vendor=%s,model=%s} 1\n", escapeLabel(r.CDU), escapeLabel(r.Protocol), escapeLabel(r.Vendor), escapeLabel(r.Model))
	}

	for _, m := range cduGauges {
		fmt.Fprintf(b, "# HELP densewatch_cdu_%s %s\n# TYPE densewatch_cdu_%s gauge\n", m.name, m.help, m.name)
		for _, r := range rs {
			if v := m.get(r); v != nil {
				fmt.Fprintf(b, "densewatch_cdu_%s{cdu=%s,protocol=%s} %g\n", m.name, escapeLabel(r.CDU), escapeLabel(r.Protocol), *v)
			}
		}
	}

	b.WriteString("# HELP densewatch_cdu_leak_detected 1 if a coolant leak is indicated.\n# TYPE densewatch_cdu_leak_detected gauge\n")
	for _, r := range rs {
		if r.LeakDetected != nil {
			fmt.Fprintf(b, "densewatch_cdu_leak_detected{cdu=%s,protocol=%s} %d\n", escapeLabel(r.CDU), escapeLabel(r.Protocol), b2i(*r.LeakDetected))
		}
	}
}
