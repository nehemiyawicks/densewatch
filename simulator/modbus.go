package main

import (
	"encoding/binary"
	"io"
	"net"
	"time"
)

// modbusServer emulates a CDU that exposes telemetry over Modbus TCP instead of
// Redfish - the fallback protocol the research proved MANDATORY (real units like
// Stulz CyberCool ship no Redfish at all). Values are 16-bit registers, scaled
// ×10 (one decimal), a common real-world CDU convention. This register map is in
// effect a vendor profile: in M1 the exporter's fallback adapter loads maps like
// this and normalizes them into the same metric schema as the Redfish path.
type modbusServer struct {
	model   *loadModel
	connSem chan struct{} // bounds concurrent connections; lazily sized in serveListener
}

const (
	maxModbusConns    = 128              // cap on concurrent Modbus connections
	modbusIdleTimeout = 30 * time.Second // close idle / slow connections
)

// Input/holding register map (FC 0x03 and 0x04 both read it). ×10 unless noted.
const (
	regSupplyTempC  = 0  // °C ×10
	regReturnTempC  = 1  // °C ×10
	regDeltaTempC   = 2  // °C ×10
	regFlowLPM      = 3  // L/min ×10
	regHeatkW       = 4  // kW ×10
	regPumpPct      = 5  // % ×10
	regSupplykPa    = 6  // kPa ×10
	regReturnkPa    = 7  // kPa ×10
	regReservoirPct = 8  // % ×10
	regInletTempC   = 9  // °C ×10
	regHumidityPct  = 10 // % ×10
	regDewPointC    = 11 // °C ×10
	regLeakState    = 12 // 0 OK, 1 warning, 2 critical
	regCount        = 13
)

func (s *modbusServer) registers(t time.Time) []uint16 {
	c := s.model.cdu(t)
	r := make([]uint16, regCount)
	r[regSupplyTempC] = u16(c.SupplyTempC * 10)
	r[regReturnTempC] = u16(c.ReturnTempC * 10)
	r[regDeltaTempC] = u16(c.DeltaTempC * 10)
	r[regFlowLPM] = u16(c.FlowLPM * 10)
	r[regHeatkW] = u16(c.HeatLoadkW * 10)
	r[regPumpPct] = u16(c.PumpPct * 10)
	r[regSupplykPa] = u16(c.SupplykPa * 10)
	r[regReturnkPa] = u16(c.ReturnkPa * 10)
	r[regReservoirPct] = u16(c.ReservoirPct * 10)
	r[regInletTempC] = u16(c.InletTempC * 10)
	r[regHumidityPct] = u16(c.HumidityPct * 10)
	r[regDewPointC] = u16(c.DewPointC * 10)
	r[regLeakState] = 0
	return r
}

func (s *modbusServer) serve(addr string) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	return s.serveListener(ln)
}

func (s *modbusServer) serveListener(ln net.Listener) error {
	if s.connSem == nil {
		s.connSem = make(chan struct{}, maxModbusConns)
	}
	for {
		conn, err := ln.Accept()
		if err != nil {
			return err
		}
		s.connSem <- struct{}{} // blocks at the cap, bounding goroutines under a flood
		go s.handleConn(conn)
	}
}

// handleConn speaks Modbus TCP: a 7-byte MBAP header (TxID, Protocol, Length,
// UnitID) followed by the PDU. Connection stays open for multiple requests.
func (s *modbusServer) handleConn(conn net.Conn) {
	defer conn.Close()
	defer func() { <-s.connSem }() // release the connection slot
	header := make([]byte, 7)
	for {
		// Idle / slow clients must not hold the socket open forever.
		_ = conn.SetReadDeadline(time.Now().Add(modbusIdleTimeout))
		if _, err := io.ReadFull(conn, header); err != nil {
			return
		}
		length := int(binary.BigEndian.Uint16(header[4:6])) // UnitID + PDU bytes
		if length < 2 || length > 260 {
			return
		}
		pdu := make([]byte, length-1)
		if _, err := io.ReadFull(conn, pdu); err != nil {
			return
		}
		resp := s.handlePDU(pdu)
		out := make([]byte, 7+len(resp))
		copy(out[0:4], header[0:4]) // echo TxID + Protocol(0)
		binary.BigEndian.PutUint16(out[4:6], uint16(len(resp)+1))
		out[6] = header[6] // echo UnitID
		copy(out[7:], resp)
		if _, err := conn.Write(out); err != nil {
			return
		}
	}
}

func (s *modbusServer) handlePDU(pdu []byte) []byte {
	if len(pdu) < 1 {
		return []byte{0x80, 0x03}
	}
	switch fc := pdu[0]; fc {
	case 0x03, 0x04: // read holding / input registers
		return readRegisters(fc, pdu, s.registers(time.Now()))
	default:
		return []byte{fc | 0x80, 0x01} // illegal function
	}
}

// readRegisters is pure (no socket, no clock) so it is straightforward to test.
func readRegisters(fc byte, pdu []byte, regs []uint16) []byte {
	if len(pdu) < 5 {
		return []byte{fc | 0x80, 0x03} // illegal data value
	}
	start := int(binary.BigEndian.Uint16(pdu[1:3]))
	qty := int(binary.BigEndian.Uint16(pdu[3:5]))
	if qty < 1 || qty > 125 || start+qty > len(regs) {
		return []byte{fc | 0x80, 0x02} // illegal data address
	}
	out := make([]byte, 0, 2+qty*2)
	out = append(out, fc, byte(qty*2))
	for i := 0; i < qty; i++ {
		out = binary.BigEndian.AppendUint16(out, regs[start+i])
	}
	return out
}

func u16(v float64) uint16 {
	switch {
	case v < 0:
		return 0
	case v > 65535:
		return 65535
	default:
		return uint16(v + 0.5)
	}
}
