package main

import (
	"fmt"
	"net/http"
	"strings"
	"time"
)

// The conformance probe answers the question the research surfaced: vendors
// advertise "Redfish" but rarely document DSP2064 CoolingUnit conformance. The
// probe walks a unit's CoolingUnit tree and reports which schema properties it
// ACTUALLY serves - so you know your real coverage before trusting a datasheet,
// and whether you need a Modbus/SNMP vendor profile instead.

type check struct {
	name    string
	present bool
	detail  string
}

type probeResult struct {
	cuURL          string
	cuType         string
	vendor, model  string
	checks         []check
	present, total int
}

func probeCoolingUnit(client *http.Client, target string) (*probeResult, error) {
	host := hostOf(target)
	cuURL, cu, err := resolveCoolingUnit(client, host, target)
	if err != nil {
		return nil, err
	}
	res := &probeResult{
		cuURL:  cuURL,
		cuType: str(cu["@odata.type"]),
		vendor: str(cu["Manufacturer"]),
		model:  str(cu["Model"]),
	}
	add := func(name string, present bool, detail string) {
		res.checks = append(res.checks, check{name, present, detail})
	}

	add("EquipmentType", cu["EquipmentType"] != nil, str(cu["EquipmentType"]))
	capW, capOK := cu["CoolingCapacityWatts"].(float64)
	add("CoolingCapacityWatts", capOK, numStr(capW, capOK))
	add("SetMode action (read-only)", hasAction(cu, "#CoolingUnit.SetMode"), "")

	conn := followFirstMember(client, host, cu, "SecondaryCoolantConnectors")
	for _, p := range []string{
		"SupplyTemperatureCelsius", "ReturnTemperatureCelsius", "DeltaTemperatureCelsius",
		"FlowLitersPerMinute", "HeatRemovedkW", "SupplyPressurekPa", "ReturnPressurekPa",
	} {
		v := readingOf(conn, p)
		add("SecondaryConnector."+p, v != nil, ptrStr(v))
	}

	pump := followFirstMember(client, host, cu, "Pumps")
	add("Pump.PumpSpeedPercent", readingOf(pump, "PumpSpeedPercent") != nil, ptrStr(readingOf(pump, "PumpSpeedPercent")))

	resv := followFirstMember(client, host, cu, "Reservoirs")
	add("Reservoir.CoolantLevelPercent", readingOf(resv, "CoolantLevelPercent") != nil, ptrStr(readingOf(resv, "CoolantLevelPercent")))

	env := follow(client, host, cu, "EnvironmentMetrics")
	for _, p := range []string{"TemperatureCelsius", "HumidityPercent", "DewPointCelsius"} {
		v := readingOf(env, p)
		add("EnvironmentMetrics."+p, v != nil, ptrStr(v))
	}

	add("LeakDetection", follow(client, host, cu, "LeakDetection") != nil, "")

	for _, c := range res.checks {
		res.total++
		if c.present {
			res.present++
		}
	}
	return res, nil
}

func runProbe(target string, insecure bool, caCert string) int {
	cleanURL, user, pass, err := redfishCreds(target)
	if err != nil {
		fmt.Printf("Verdict: ERROR - %v\n", err)
		return 1
	}
	fmt.Printf("densewatch-cdu conformance probe\ntarget: %s\n\n", sanitizeTerminalString(cleanURL))

	tr, err := baseTransport(caCert, insecure)
	if err != nil {
		fmt.Printf("Verdict: ERROR - %v\n", err)
		return 1
	}
	client := redfishHTTPClient(10*time.Second, tr, user, pass)

	res, err := probeCoolingUnit(client, cleanURL)
	if err != nil {
		fmt.Printf("Verdict: NO REDFISH CoolingUnit - %v\n", err)
		fmt.Println("         Fall back to a Modbus/SNMP vendor profile for this unit.")
		return 1
	}

	fmt.Printf("CoolingUnit: %s  (%s / %s)\n", sanitizeTerminalString(res.cuType), sanitizeTerminalString(res.vendor), sanitizeTerminalString(res.model))
	fmt.Printf("       at  : %s\n\n", sanitizeTerminalString(res.cuURL))
	for _, c := range res.checks {
		mark := "MISSING"
		if c.present {
			mark = "ok     "
		}
		fmt.Printf("  [%s]  %-42s %s\n", mark, sanitizeTerminalString(c.name), sanitizeTerminalString(c.detail))
	}
	fmt.Printf("\nCoverage: %d/%d checked DSP2064 CoolingUnit properties served.\n", res.present, res.total)

	switch pct := float64(res.present) / float64(res.total); {
	case pct >= 0.9:
		fmt.Println("Verdict: GOOD - densewatch-cdu's Redfish path is fully supported on this unit.")
	case pct >= 0.5:
		fmt.Println("Verdict: PARTIAL - some properties absent; a Modbus/SNMP profile may give fuller coverage.")
	default:
		fmt.Println("Verdict: SPARSE - minimal CoolingUnit coverage; prefer a Modbus/SNMP vendor profile.")
	}
	if res.present < res.total {
		var miss []string
		for _, c := range res.checks {
			if !c.present {
				miss = append(miss, sanitizeTerminalString(c.name))
			}
		}
		fmt.Println("Missing : " + strings.Join(miss, ", "))
	}
	return 0
}

// resolveCoolingUnit accepts a CoolingUnit URL, a ThermalEquipment URL, or a
// Redfish service root, and navigates down to the first CoolingUnit it finds.
func resolveCoolingUnit(client *http.Client, host, target string) (string, map[string]any, error) {
	m, err := getJSON(client, target)
	if err != nil {
		return "", nil, err
	}
	if isCoolingUnit(m) {
		return target, m, nil
	}
	if te := follow(client, host, m, "ThermalEquipment"); te != nil {
		m = te // service root -> thermal equipment
	}
	if cduColl := odataURL(host, m, "CDUs"); cduColl != "" {
		if coll, err := getJSON(client, cduColl); err == nil {
			if members, _ := coll["Members"].([]any); len(members) > 0 {
				if first, ok := members[0].(map[string]any); ok {
					if id, _ := first["@odata.id"].(string); id != "" {
						if cu, err := getJSON(client, host+id); err == nil && isCoolingUnit(cu) {
							return host + id, cu, nil
						}
					}
				}
			}
		}
	}
	return "", nil, fmt.Errorf("no CoolingUnit resource found at or under %s", target)
}

func isCoolingUnit(m map[string]any) bool {
	if strings.Contains(str(m["@odata.type"]), "CoolingUnit") {
		return true
	}
	return m["EquipmentType"] != nil || m["SecondaryCoolantConnectors"] != nil || m["PrimaryCoolantConnectors"] != nil
}

func hasAction(cu map[string]any, name string) bool {
	a, ok := cu["Actions"].(map[string]any)
	if !ok {
		return false
	}
	_, ok = a[name]
	return ok
}

func ptrStr(v *float64) string {
	if v == nil {
		return ""
	}
	return fmt.Sprintf("%g", *v)
}

func numStr(v float64, ok bool) string {
	if !ok {
		return ""
	}
	return fmt.Sprintf("%g", v)
}

// sanitizeTerminalString strips C0 control characters (including ESC, CR, LF) and
// DEL from any string that originated at a remote Redfish endpoint, so a hostile
// field cannot inject ANSI escape sequences or fake lines into the terminal. It
// also caps length so a giant field cannot flood the output. Display-time only;
// the underlying probe data model is left untouched.
func sanitizeTerminalString(s string) string {
	const maxLen = 256
	var b strings.Builder
	for _, r := range s {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) { // drop C0, DEL, and C1 (e.g. CSI U+009B)
			continue
		}
		if b.Len() >= maxLen {
			b.WriteString("...")
			break
		}
		b.WriteRune(r)
	}
	return b.String()
}
