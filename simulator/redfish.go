package main

import (
	"encoding/json"
	"net/http"
	"time"
)

// cduBase is the canonical DSP2064 CoolingUnit URI this sim serves. The M1
// exporter is written against exactly this tree (plus per-vendor profiles).
const cduBase = "/redfish/v1/ThermalEquipment/CDUs/1"

type redfishServer struct{ model *loadModel }

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("OData-Version", "4.0")
	_ = json.NewEncoder(w).Encode(v)
}

// link is a Redfish navigation reference; reading is a SensorExcerpt-style value.
func link(id string) map[string]any     { return map[string]any{"@odata.id": id} }
func reading(v float64) map[string]any  { return map[string]any{"Reading": v} }

func (s *redfishServer) routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /redfish/v1", s.serviceRoot)
	mux.HandleFunc("GET /redfish/v1/{$}", s.serviceRoot)
	mux.HandleFunc("GET /redfish/v1/ThermalEquipment", s.thermalEquipment)
	mux.HandleFunc("GET /redfish/v1/ThermalEquipment/CDUs", s.cduCollection)
	mux.HandleFunc("GET "+cduBase, s.coolingUnit)
	mux.HandleFunc("GET "+cduBase+"/PrimaryCoolantConnectors", s.connectorCollection("Primary"))
	mux.HandleFunc("GET "+cduBase+"/PrimaryCoolantConnectors/1", s.connector("Primary"))
	mux.HandleFunc("GET "+cduBase+"/SecondaryCoolantConnectors", s.connectorCollection("Secondary"))
	mux.HandleFunc("GET "+cduBase+"/SecondaryCoolantConnectors/1", s.connector("Secondary"))
	mux.HandleFunc("GET "+cduBase+"/Pumps", s.pumpCollection)
	mux.HandleFunc("GET "+cduBase+"/Pumps/1", s.pump)
	mux.HandleFunc("GET "+cduBase+"/Reservoirs", s.reservoirCollection)
	mux.HandleFunc("GET "+cduBase+"/Reservoirs/1", s.reservoir)
	mux.HandleFunc("GET "+cduBase+"/LeakDetection", s.leakDetection)
	mux.HandleFunc("GET "+cduBase+"/EnvironmentMetrics", s.environmentMetrics)
}

func (s *redfishServer) serviceRoot(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{
		"@odata.id":        "/redfish/v1/",
		"@odata.type":      "#ServiceRoot.v1_16_0.ServiceRoot",
		"Id":               "RootService",
		"Name":             "densewatch CDU Simulator",
		"RedfishVersion":   "1.20.0",
		"ThermalEquipment": link("/redfish/v1/ThermalEquipment"),
	})
}

func (s *redfishServer) thermalEquipment(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{
		"@odata.id":   "/redfish/v1/ThermalEquipment",
		"@odata.type": "#ThermalEquipment.v1_1_0.ThermalEquipment",
		"Id":          "ThermalEquipment",
		"Name":        "Thermal Equipment",
		"CDUs":        link("/redfish/v1/ThermalEquipment/CDUs"),
	})
}

func (s *redfishServer) cduCollection(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{
		"@odata.id":           "/redfish/v1/ThermalEquipment/CDUs",
		"@odata.type":         "#CoolingUnitCollection.CoolingUnitCollection",
		"Name":                "Cooling Unit Collection",
		"Members@odata.count": 1,
		"Members":             []any{link(cduBase)},
	})
}

func (s *redfishServer) coolingUnit(w http.ResponseWriter, r *http.Request) {
	c := s.model.cdu(time.Now())
	writeJSON(w, map[string]any{
		"@odata.id":                  cduBase,
		"@odata.type":                "#CoolingUnit.v1_2_0.CoolingUnit",
		"Id":                         "1",
		"Name":                       "densewatch-sim CDU 1",
		"EquipmentType":              "CDU",
		"Manufacturer":               "DenseWatch Sim",
		"Model":                      "DW-SIM-CDU-180",
		"SerialNumber":               "SIM-CDU-0001",
		"Version":                    "1.0.0-sim",
		"Status":                     map[string]any{"State": "Enabled", "Health": "OK"},
		"CoolingCapacityWatts":       c.CapacitykW * 1000,
		"PrimaryCoolantConnectors":   link(cduBase + "/PrimaryCoolantConnectors"),
		"SecondaryCoolantConnectors": link(cduBase + "/SecondaryCoolantConnectors"),
		"Pumps":                      link(cduBase + "/Pumps"),
		"Reservoirs":                 link(cduBase + "/Reservoirs"),
		"LeakDetection":              link(cduBase + "/LeakDetection"),
		"EnvironmentMetrics":         link(cduBase + "/EnvironmentMetrics"),
		// Advertised but intentionally not actuated: read-only is a v0.1 safety
		// choice, not a schema limit (see spec §3).
		"Actions": map[string]any{
			"#CoolingUnit.SetMode": map[string]any{"target": cduBase + "/Actions/CoolingUnit.SetMode"},
		},
	})
}

func (s *redfishServer) connectorCollection(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{
			"@odata.id":           cduBase + "/" + kind + "CoolantConnectors",
			"@odata.type":         "#CoolantConnectorCollection.CoolantConnectorCollection",
			"Name":                kind + " Coolant Connector Collection",
			"Members@odata.count": 1,
			"Members":             []any{link(cduBase + "/" + kind + "CoolantConnectors/1")},
		})
	}
}

func (s *redfishServer) connector(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := s.model.cdu(time.Now())
		supply, ret := c.SupplyTempC, c.ReturnTempC
		if kind == "Primary" {
			// Facility (chilled-water) side runs cooler; same heat crosses the HX.
			supply = round1(c.SupplyTempC - 12)
			ret = round1(supply + c.DeltaTempC)
		}
		writeJSON(w, map[string]any{
			"@odata.id":                cduBase + "/" + kind + "CoolantConnectors/1",
			"@odata.type":              "#CoolantConnector.v1_1_0.CoolantConnector",
			"Id":                       "1",
			"Name":                     kind + " coolant connector",
			"CoolantConnectorType":     "Pair",
			"RatedFlowLitersPerMinute": ratedFlowLPM,
			"FlowLitersPerMinute":      reading(c.FlowLPM),
			"SupplyTemperatureCelsius": reading(supply),
			"ReturnTemperatureCelsius": reading(ret),
			"DeltaTemperatureCelsius":  reading(c.DeltaTempC),
			"SupplyPressurekPa":        reading(c.SupplykPa),
			"ReturnPressurekPa":        reading(c.ReturnkPa),
			"DeltaPressurekPa":         reading(c.DeltakPa),
			"HeatRemovedkW":            reading(c.HeatLoadkW),
		})
	}
}

func (s *redfishServer) pumpCollection(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{
		"@odata.id":           cduBase + "/Pumps",
		"@odata.type":         "#PumpCollection.PumpCollection",
		"Name":                "Pump Collection",
		"Members@odata.count": 1,
		"Members":             []any{link(cduBase + "/Pumps/1")},
	})
}

func (s *redfishServer) pump(w http.ResponseWriter, r *http.Request) {
	c := s.model.cdu(time.Now())
	writeJSON(w, map[string]any{
		"@odata.id":        cduBase + "/Pumps/1",
		"@odata.type":      "#Pump.v1_1_0.Pump",
		"Id":               "1",
		"Name":             "Primary pump 1",
		"PumpType":         "Liquid",
		"Status":           map[string]any{"State": "Enabled", "Health": "OK"},
		"PumpSpeedPercent": reading(c.PumpPct),
	})
}

func (s *redfishServer) reservoirCollection(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{
		"@odata.id":           cduBase + "/Reservoirs",
		"@odata.type":         "#ReservoirCollection.ReservoirCollection",
		"Name":                "Reservoir Collection",
		"Members@odata.count": 1,
		"Members":             []any{link(cduBase + "/Reservoirs/1")},
	})
}

func (s *redfishServer) reservoir(w http.ResponseWriter, r *http.Request) {
	c := s.model.cdu(time.Now())
	writeJSON(w, map[string]any{
		"@odata.id":     cduBase + "/Reservoirs/1",
		"@odata.type":   "#Reservoir.v1_0_0.Reservoir",
		"Id":            "1",
		"Name":          "Primary reservoir",
		"ReservoirType": "Reserve",
		"CapacityLiters": 50,
		"Status":         map[string]any{"State": "Enabled", "Health": "OK"},
		// Live fill level (sim convenience; real units vary in where they expose this).
		"CoolantLevelPercent": reading(c.ReservoirPct),
	})
}

func (s *redfishServer) leakDetection(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{
		"@odata.id":   cduBase + "/LeakDetection",
		"@odata.type": "#LeakDetection.v1_0_0.LeakDetection",
		"Id":          "LeakDetection",
		"Name":        "Leak Detection",
		"Status":      map[string]any{"State": "Enabled", "Health": "OK"},
		"LeakDetectorGroups": []any{map[string]any{
			"GroupName": "CDU",
			"Detectors": []any{map[string]any{
				"DataSourceUri": cduBase + "/EnvironmentMetrics",
				"DetectorState": "OK",
			}},
		}},
	})
}

func (s *redfishServer) environmentMetrics(w http.ResponseWriter, r *http.Request) {
	c := s.model.cdu(time.Now())
	writeJSON(w, map[string]any{
		"@odata.id":           cduBase + "/EnvironmentMetrics",
		"@odata.type":         "#EnvironmentMetrics.v1_3_0.EnvironmentMetrics",
		"Id":                  "EnvironmentMetrics",
		"Name":                "Environment Metrics",
		"TemperatureCelsius":  reading(c.InletTempC),
		"HumidityPercent":     reading(c.HumidityPct),
		"DewPointCelsius":     reading(c.DewPointC),
	})
}
