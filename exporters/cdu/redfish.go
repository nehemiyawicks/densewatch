package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

// collectRedfish scrapes a DSP2064 CoolingUnit tree by following @odata.id links
// (collection -> member) the way a real Redfish client must, and normalizes the
// SensorExcerpt {"Reading": x} values into the unified schema.
func collectRedfish(cuURL string, client *http.Client) Reading {
	r := Reading{Protocol: "redfish", CDU: cuURL}
	host := hostOf(cuURL)

	cu, err := getJSON(client, cuURL)
	if err != nil {
		return r // Up=false; CDU label keeps the URL so a down target is identifiable
	}
	if id := str(cu["Id"]); id != "" {
		r.CDU = id
	}
	r.Vendor = str(cu["Manufacturer"])
	r.Model = str(cu["Model"])
	if c, ok := cu["CoolingCapacityWatts"].(float64); ok {
		kw := c / 1000
		r.CapacitykW = &kw
	}

	if conn := followFirstMember(client, host, cu, "SecondaryCoolantConnectors"); conn != nil {
		r.SupplyTempC = readingOf(conn, "SupplyTemperatureCelsius")
		r.ReturnTempC = readingOf(conn, "ReturnTemperatureCelsius")
		r.DeltaTempC = readingOf(conn, "DeltaTemperatureCelsius")
		r.FlowLPM = readingOf(conn, "FlowLitersPerMinute")
		r.HeatRemovedkW = readingOf(conn, "HeatRemovedkW")
		r.SupplykPa = readingOf(conn, "SupplyPressurekPa")
		r.ReturnkPa = readingOf(conn, "ReturnPressurekPa")
	}
	if pump := followFirstMember(client, host, cu, "Pumps"); pump != nil {
		r.PumpPct = readingOf(pump, "PumpSpeedPercent")
	}
	if res := followFirstMember(client, host, cu, "Reservoirs"); res != nil {
		r.ReservoirPct = readingOf(res, "CoolantLevelPercent")
	}
	if env := follow(client, host, cu, "EnvironmentMetrics"); env != nil {
		r.InletTempC = readingOf(env, "TemperatureCelsius")
		r.HumidityPct = readingOf(env, "HumidityPercent")
		r.DewPointC = readingOf(env, "DewPointCelsius")
	}
	if leak := follow(client, host, cu, "LeakDetection"); leak != nil {
		b := leakDetected(leak)
		r.LeakDetected = &b
	}

	r.Up = true
	return r
}

func leakDetected(leak map[string]any) bool {
	if st, ok := leak["Status"].(map[string]any); ok {
		if h := str(st["Health"]); h != "" && h != "OK" {
			return true
		}
	}
	groups, _ := leak["LeakDetectorGroups"].([]any)
	for _, g := range groups {
		gm, ok := g.(map[string]any)
		if !ok {
			continue
		}
		dets, _ := gm["Detectors"].([]any)
		for _, d := range dets {
			dm, ok := d.(map[string]any)
			if !ok {
				continue
			}
			if s := str(dm["DetectorState"]); s != "" && s != "OK" {
				return true
			}
		}
	}
	return false
}

// --- Redfish navigation helpers ---

// maxRedfishBodyBytes caps a Redfish response: the CDU schema tree is small and
// predictable, and a timeout alone does not stop a huge body from exhausting memory.
const maxRedfishBodyBytes = 1 << 20 // 1 MiB

func getJSON(client *http.Client, rawurl string) (map[string]any, error) {
	resp, err := client.Get(rawurl)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s", rawurl, resp.Status)
	}
	// Read at most the cap + 1 byte, so we can distinguish "at the limit" from "over it".
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxRedfishBodyBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > maxRedfishBodyBytes {
		return nil, fmt.Errorf("response too large for Redfish JSON (> %d bytes)", maxRedfishBodyBytes)
	}
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, err
	}
	return m, nil
}

func hostOf(rawurl string) string {
	u, err := url.Parse(rawurl)
	if err != nil {
		return ""
	}
	return u.Scheme + "://" + u.Host
}

// odataURL resolves node[key].@odata.id (an absolute Redfish path) against host.
func odataURL(host string, node map[string]any, key string) string {
	v, ok := node[key].(map[string]any)
	if !ok {
		return ""
	}
	id, _ := v["@odata.id"].(string)
	if id == "" {
		return ""
	}
	return host + id
}

func follow(client *http.Client, host string, node map[string]any, key string) map[string]any {
	u := odataURL(host, node, key)
	if u == "" {
		return nil
	}
	m, err := getJSON(client, u)
	if err != nil {
		return nil
	}
	return m
}

func followFirstMember(client *http.Client, host string, node map[string]any, key string) map[string]any {
	coll := follow(client, host, node, key)
	if coll == nil {
		return nil
	}
	members, _ := coll["Members"].([]any)
	if len(members) == 0 {
		return nil
	}
	first, ok := members[0].(map[string]any)
	if !ok {
		return nil
	}
	id, _ := first["@odata.id"].(string)
	if id == "" {
		return nil
	}
	m, err := getJSON(client, host+id)
	if err != nil {
		return nil
	}
	return m
}

func str(v any) string { s, _ := v.(string); return s }

// readingOf extracts a Redfish SensorExcerpt {"Reading": <number>} as *float64.
func readingOf(m map[string]any, key string) *float64 {
	obj, ok := m[key].(map[string]any)
	if !ok {
		return nil
	}
	f, ok := obj["Reading"].(float64)
	if !ok {
		return nil
	}
	return &f
}
