package main

import (
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"net"
	"time"
)

// regField maps one Modbus register into the unified schema with a scale factor.
type regField struct {
	addr  int
	scale float64
	set   func(*Reading, float64)
}

// simModbusProfile is a vendor profile in miniature: the register map of a CDU
// that speaks Modbus instead of Redfish. In production these ship per-vendor
// (YAML/JSON); this one matches the densewatch simulator's map (values are x10).
// Broad heterogeneous-CDU coverage via profiles like this is the coverage moat.
var simModbusProfile = []regField{
	{0, 0.1, func(r *Reading, v float64) { r.SupplyTempC = &v }},
	{1, 0.1, func(r *Reading, v float64) { r.ReturnTempC = &v }},
	{2, 0.1, func(r *Reading, v float64) { r.DeltaTempC = &v }},
	{3, 0.1, func(r *Reading, v float64) { r.FlowLPM = &v }},
	{4, 0.1, func(r *Reading, v float64) { r.HeatRemovedkW = &v }},
	{5, 0.1, func(r *Reading, v float64) { r.PumpPct = &v }},
	{6, 0.1, func(r *Reading, v float64) { r.SupplykPa = &v }},
	{7, 0.1, func(r *Reading, v float64) { r.ReturnkPa = &v }},
	{8, 0.1, func(r *Reading, v float64) { r.ReservoirPct = &v }},
	{9, 0.1, func(r *Reading, v float64) { r.InletTempC = &v }},
	{10, 0.1, func(r *Reading, v float64) { r.HumidityPct = &v }},
	{11, 0.1, func(r *Reading, v float64) { r.DewPointC = &v }},
	{12, 1.0, func(r *Reading, v float64) { b := v > 0; r.LeakDetected = &b }},
}

func collectModbus(addr string, timeout time.Duration) Reading {
	r := Reading{Protocol: "modbus", CDU: addr, Vendor: "sim-modbus", Model: "DW-SIM-CDU-180"}
	regs, err := modbusReadInputRegisters(addr, 0, 13, timeout)
	if err != nil {
		return r // Up=false
	}
	for _, f := range simModbusProfile {
		if f.addr < len(regs) {
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
