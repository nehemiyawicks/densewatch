package main

import (
	"fmt"
	"math"
	"time"
)

// Physical/topology constants for the simulated pod. Kept small and legible -
// the goal is believable, physically-consistent telemetry, not a digital twin.
const (
	gpusPerNode  = 8
	gpuTDPWatts  = 1000.0 // Blackwell-class peak board power
	gpuIdleWatts = 150.0
	waterQFactor = 0.0698 // kW ≈ L/min × °C × 0.0698  (water, cp≈4.186 kJ/kg·K)
	ratedFlowLPM = 180.0
	supplySetC   = 32.0 // warm-water direct-to-chip secondary supply setpoint
	targetDelta  = 10.0 // °C target ΔT across the secondary loop
)

// node is one simulated GPU server and how it maps into facility topology.
type node struct {
	Host string
	Rack string
	Job  string // dcgm hpc_job label; "" means idle / unallocated
}

// fleet is the static topology the simulator serves: two racks, a training job
// pinned to rack-A1, an inference job + idle nodes in rack-A2.
var fleet = []node{
	{"node01", "rack-A1", "train-llm-70b"},
	{"node02", "rack-A1", "train-llm-70b"},
	{"node03", "rack-A1", "train-llm-70b"},
	{"node04", "rack-A1", "train-llm-70b"},
	{"node05", "rack-A2", "infer-serving"},
	{"node06", "rack-A2", "infer-serving"},
	{"node07", "rack-A2", ""},
	{"node08", "rack-A2", ""},
}

type loadModel struct{ start time.Time }

func newLoadModel(start time.Time) *loadModel { return &loadModel{start: start} }

// noise is deterministic, concurrency-safe pseudo-jitter in ~[-1,1] (no RNG, so
// the HTTP handlers can call it concurrently without locks and tests stay stable).
func noise(seed, s float64) float64 {
	return 0.6*math.Sin(seed*1.7+s/5.0) + 0.4*math.Sin(seed*0.97+s/1.3)
}

// utilization is a shared 0..1 workload signal that drives BOTH GPU power and
// CDU heat load - so GPU telemetry and cooling telemetry genuinely correlate,
// which is the whole thesis the correlation engine (M3) will exploit.
func (m *loadModel) utilization(s, phase float64) float64 {
	v := 0.62 + 0.25*math.Sin((s+phase)/90.0) + 0.08*math.Sin((s+phase)/13.0) + 0.05*math.Sin((s+phase)/3.3)
	return clamp(v, 0.05, 1.0)
}

type gpuSample struct {
	Node    node
	GPU     int
	UUID    string
	PowerW  float64
	TempC   float64
	UtilPct float64
	ClockM  float64
}

func (m *loadModel) gpuSamples(t time.Time) []gpuSample {
	s := t.Sub(m.start).Seconds()
	out := make([]gpuSample, 0, len(fleet)*gpusPerNode)
	gid := 0
	for ni, n := range fleet {
		for i := 0; i < gpusPerNode; i++ {
			var u float64
			if n.Job == "" {
				u = clamp(0.05+0.02*math.Sin(s/30.0), 0.02, 0.12) // idle node idles
			} else {
				u = m.utilization(s, float64(ni*7+i))
			}
			u = clamp(u+0.03*noise(float64(gid), s), 0, 1)
			out = append(out, gpuSample{
				Node:    n,
				GPU:     gid,
				UUID:    fmt.Sprintf("GPU-sim-%010d", gid),
				PowerW:  round1(gpuIdleWatts + u*(gpuTDPWatts-gpuIdleWatts)),
				TempC:   round1(30 + u*48 + 1.5*noise(float64(gid)+100, s)),
				UtilPct: round1(u * 100),
				ClockM:  round1(900 + u*870),
			})
			gid++
		}
	}
	return out
}

// cduTelemetry is one CDU's live, physically-consistent secondary-loop state.
type cduTelemetry struct {
	HeatLoadkW   float64
	FlowLPM      float64
	SupplyTempC  float64
	ReturnTempC  float64
	DeltaTempC   float64
	SupplykPa    float64
	ReturnkPa    float64
	DeltakPa     float64
	PumpPct      float64
	ReservoirPct float64
	InletTempC   float64
	HumidityPct  float64
	DewPointC    float64
	CapacitykW   float64
}

func (m *loadModel) cdu(t time.Time) cduTelemetry {
	s := t.Sub(m.start).Seconds()
	total := 0.0
	for _, g := range m.gpuSamples(t) {
		total += g.PowerW
	}
	heatkW := total / 1000.0 * 1.12 // ~12% non-GPU loop overhead
	// Flow tracks heat load to hold the target ΔT, clamped to [60, rated]. The
	// 60 L/min floor means ΔT drops below target at low load; with a denser fleet
	// than the default, the rated-flow cap would let ΔT rise (cooling shortfall).
	flow := clamp(heatkW/(targetDelta*waterQFactor), 60, ratedFlowLPM)
	dT := heatkW / (flow * waterQFactor)
	supply := supplySetC + 0.4*math.Sin(s/120.0)
	inlet := 27 + 0.8*math.Sin(s/150.0) // tropical ambient (≈ the SL thesis)
	rh := 72 + 3*math.Sin(s/180.0)
	gamma := math.Log(rh/100) + (17.62*inlet)/(243.12+inlet) // Magnus dew point
	dew := 243.12 * gamma / (17.62 - gamma)
	supkPa := 340 + flow*0.15
	dkPa := 130 + flow*0.05
	return cduTelemetry{
		HeatLoadkW:   round1(heatkW),
		FlowLPM:      round1(flow),
		SupplyTempC:  round1(supply),
		ReturnTempC:  round1(supply + dT),
		DeltaTempC:   round1(dT),
		SupplykPa:    round1(supkPa),
		ReturnkPa:    round1(supkPa - dkPa),
		DeltakPa:     round1(dkPa),
		PumpPct:      round1(flow / ratedFlowLPM * 100),
		ReservoirPct: round1(85 + 1.5*math.Sin(s/200.0)),
		InletTempC:   round1(inlet),
		HumidityPct:  round1(rh),
		DewPointC:    round1(dew),
		CapacitykW:   round1(ratedFlowLPM * targetDelta * waterQFactor),
	}
}

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func round1(v float64) float64 { return math.Round(v*10) / 10 }
