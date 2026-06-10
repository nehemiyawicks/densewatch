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
	cleanURL, user, pass := redfishCreds(target)
	fmt.Printf("densewatch-cdu conformance probe\ntarget: %s\n\n", cleanURL)

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

	fmt.Printf("CoolingUnit: %s  (%s / %s)\n", res.cuType, res.vendor, res.model)
	fmt.Printf("       at  : %s\n\n", res.cuURL)
	for _, c := range res.checks {
		mark := "MISSING"
		if c.present {
			mark = "ok     "
		}
		fmt.Printf("  [%s]  %-42s %s\n", mark, c.name, c.detail)
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
				miss = append(miss, c.name)
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
