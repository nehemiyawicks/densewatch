package main

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net"
	"os"
	"time"
)

// regField maps one Modbus register into the unified schema with a scale factor.
type regField struct {
	addr  int
	scale float64
	set   func(*Reading, float64)
}

// modbusFieldSetters maps a profile's field name to the schema field it fills. Both
// the built-in sim profile and external vendor profiles reference these names; an
// unknown name is rejected at load. So adding a CDU is writing a JSON profile, not Go.
var modbusFieldSetters = map[string]func(*Reading, float64){
	"supply_temp_c":   func(r *Reading, v float64) { r.SupplyTempC = &v },
	"return_temp_c":   func(r *Reading, v float64) { r.ReturnTempC = &v },
	"delta_temp_c":    func(r *Reading, v float64) { r.DeltaTempC = &v },
	"flow_lpm":        func(r *Reading, v float64) { r.FlowLPM = &v },
	"heat_removed_kw": func(r *Reading, v float64) { r.HeatRemovedkW = &v },
	"pump_pct":        func(r *Reading, v float64) { r.PumpPct = &v },
	"supply_kpa":      func(r *Reading, v float64) { r.SupplykPa = &v },
	"return_kpa":      func(r *Reading, v float64) { r.ReturnkPa = &v },
	"reservoir_pct":   func(r *Reading, v float64) { r.ReservoirPct = &v },
	"inlet_temp_c":    func(r *Reading, v float64) { r.InletTempC = &v },
	"humidity_pct":    func(r *Reading, v float64) { r.HumidityPct = &v },
	"dew_point_c":     func(r *Reading, v float64) { r.DewPointC = &v },
	"leak":            func(r *Reading, v float64) { b := v > 0; r.LeakDetected = &b },
}

// modbusProfile is a CDU's register map: which register carries which schema field,
// and the scale to apply. Breadth of these across vendors is the coverage moat.
type modbusProfile struct {
	Name   string
	Fields []regField
}

// qty is how many consecutive registers (from 0) the profile needs read.
func (p modbusProfile) qty() uint16 {
	max := 0
	for _, f := range p.Fields {
		if f.addr+1 > max {
			max = f.addr + 1
		}
	}
	return uint16(max)
}

// simModbusProfile matches the densewatch simulator's register map (values x10). It
// is the built-in default and the worked example external profiles are modeled on.
var simModbusProfile = modbusProfile{
	Name: "densewatch-sim",
	Fields: []regField{
		{0, 0.1, modbusFieldSetters["supply_temp_c"]},
		{1, 0.1, modbusFieldSetters["return_temp_c"]},
		{2, 0.1, modbusFieldSetters["delta_temp_c"]},
		{3, 0.1, modbusFieldSetters["flow_lpm"]},
		{4, 0.1, modbusFieldSetters["heat_removed_kw"]},
		{5, 0.1, modbusFieldSetters["pump_pct"]},
		{6, 0.1, modbusFieldSetters["supply_kpa"]},
		{7, 0.1, modbusFieldSetters["return_kpa"]},
		{8, 0.1, modbusFieldSetters["reservoir_pct"]},
		{9, 0.1, modbusFieldSetters["inlet_temp_c"]},
		{10, 0.1, modbusFieldSetters["humidity_pct"]},
		{11, 0.1, modbusFieldSetters["dew_point_c"]},
		{12, 1.0, modbusFieldSetters["leak"]},
	},
}

// profileSpec is the on-disk JSON form of a vendor profile.
type profileSpec struct {
	Name      string `json:"name"`
	Registers []struct {
		Field string  `json:"field"`
		Addr  int     `json:"addr"`
		Scale float64 `json:"scale"`
	} `json:"registers"`
}

const maxModbusProfileBytes = 1 << 20 // 1 MiB; a register map is tiny

// parseModbusProfile resolves a JSON profile's register field names to schema
// setters. Unknown field names and out-of-range addresses are rejected.
func parseModbusProfile(data []byte) (modbusProfile, error) {
	var spec profileSpec
	if err := json.Unmarshal(data, &spec); err != nil {
		return modbusProfile{}, fmt.Errorf("invalid profile JSON: %w", err)
	}
	if len(spec.Registers) == 0 {
		return modbusProfile{}, fmt.Errorf("profile has no registers")
	}
	p := modbusProfile{Name: spec.Name}
	if p.Name == "" {
		p.Name = "modbus"
	}
	for _, reg := range spec.Registers {
		set, ok := modbusFieldSetters[reg.Field]
		if !ok {
			return modbusProfile{}, fmt.Errorf("unknown field %q (see docs for the valid set)", reg.Field)
		}
		if reg.Addr < 0 || reg.Addr > 0xffff {
			return modbusProfile{}, fmt.Errorf("register addr %d out of range for field %q", reg.Addr, reg.Field)
		}
		p.Fields = append(p.Fields, regField{addr: reg.Addr, scale: reg.Scale, set: set})
	}
	return p, nil
}

// loadModbusProfile reads a JSON vendor profile from disk (bounded) and parses it.
func loadModbusProfile(path string) (modbusProfile, error) {
	f, err := os.Open(path)
	if err != nil {
		return modbusProfile{}, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxModbusProfileBytes+1))
	if err != nil {
		return modbusProfile{}, err
	}
	if int64(len(data)) > maxModbusProfileBytes {
		return modbusProfile{}, fmt.Errorf("modbus profile %s: file too large", path)
	}
	p, err := parseModbusProfile(data)
	if err != nil {
		return modbusProfile{}, fmt.Errorf("modbus profile %s: %w", path, err)
	}
	return p, nil
}

func collectModbus(addr string, timeout time.Duration, profile modbusProfile) Reading {
	r := Reading{Protocol: "modbus", CDU: addr, Vendor: profile.Name}
	regs, err := modbusReadInputRegisters(addr, 0, profile.qty(), timeout)
	if err != nil {
		return r // Up=false
	}
	for _, f := range profile.Fields {
		if f.addr >= 0 && f.addr < len(regs) {
			f.set(&r, round3(float64(regs[f.addr])*f.scale))
		}
	}
	r.Up = true
	return r
}

// round3 strips floating-point noise from scaled register values so the exported
// metric reads e.g. 44.8 rather than 44.800000000000004.
func round3(v float64) float64 { return math.Round(v*1000) / 1000 }

// maxModbusResponseBytes caps the PDU read from a CDU's Modbus reply: a conformant
// FC4 response for <=125 registers is at most 253 bytes, so anything larger is
// malformed and must not drive an allocation.
const maxModbusResponseBytes = 253

// modbusReadInputRegisters is a minimal Modbus-TCP client (function code 0x04).
func modbusReadInputRegisters(addr string, start, qty uint16, timeout time.Duration) ([]uint16, error) {
	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(timeout))

	// MBAP(TxID, Proto=0, Len=6, Unit=1) + PDU(FC4, start, qty)
	req := []byte{0, 1, 0, 0, 0, 6, 1, 0x04, byte(start >> 8), byte(start), byte(qty >> 8), byte(qty)}
	if _, err := conn.Write(req); err != nil {
		return nil, err
	}

	head := make([]byte, 7)
	if _, err := io.ReadFull(conn, head); err != nil {
		return nil, err
	}
	n := int(binary.BigEndian.Uint16(head[4:6])) - 1
	if n < 2 || n > maxModbusResponseBytes {
		return nil, fmt.Errorf("invalid modbus response length %d", n)
	}
	body := make([]byte, n)
	if _, err := io.ReadFull(conn, body); err != nil {
		return nil, err
	}
	if body[0]&0x80 != 0 {
		return nil, fmt.Errorf("modbus exception 0x%02x", body[1])
	}
	if body[0] != 0x04 {
		return nil, fmt.Errorf("unexpected function code 0x%02x", body[0])
	}
	count := int(body[1])
	if count%2 != 0 || 2+count > len(body) {
		return nil, fmt.Errorf("invalid modbus byte count %d", count)
	}
	regs := make([]uint16, count/2)
	for i := range regs {
		regs[i] = binary.BigEndian.Uint16(body[2+i*2:])
	}
	return regs, nil
}
