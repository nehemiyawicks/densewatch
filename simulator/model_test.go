package main

import (
	"testing"
	"time"
)

// TestCDUPhysics guards the physical consistency the dashboards rely on.
func TestCDUPhysics(t *testing.T) {
	m := newLoadModel(time.Now())
	c := m.cdu(time.Now().Add(42 * time.Second))

	if c.HeatLoadkW <= 0 {
		t.Fatalf("heat load should be positive, got %v", c.HeatLoadkW)
	}
	if c.DeltaTempC <= 0 || c.DeltaTempC > 30 {
		t.Fatalf("ΔT out of plausible range: %v", c.DeltaTempC)
	}
	if c.ReturnTempC <= c.SupplyTempC {
		t.Fatalf("return (%v) should exceed supply (%v)", c.ReturnTempC, c.SupplyTempC)
	}
	if c.DewPointC >= c.InletTempC {
		t.Fatalf("dew point (%v) should sit below inlet temp (%v)", c.DewPointC, c.InletTempC)
	}
	// HeatRemoved should track flow×ΔT within rounding.
	want := c.FlowLPM * c.DeltaTempC * waterQFactor
	if d := c.HeatLoadkW - want; d > 2 || d < -2 {
		t.Fatalf("heat balance off: heat=%v vs flow×ΔT=%v", c.HeatLoadkW, want)
	}
}

// TestGPUJobMapping ensures the fleet yields both job-mapped and idle GPUs, so
// the correlation engine has a real attribution signal to test against.
func TestGPUJobMapping(t *testing.T) {
	m := newLoadModel(time.Now())
	gs := m.gpuSamples(time.Now())

	if got, want := len(gs), len(fleet)*gpusPerNode; got != want {
		t.Fatalf("expected %d GPUs, got %d", want, got)
	}
	var busy, idle int
	for _, g := range gs {
		if g.Node.Job == "" {
			idle++
		} else {
			busy++
		}
	}
	if busy == 0 || idle == 0 {
		t.Fatalf("expected a mix of job-mapped and idle GPUs, got busy=%d idle=%d", busy, idle)
	}
}
