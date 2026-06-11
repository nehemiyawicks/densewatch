package main

import (
	"encoding/binary"
	"io"
	"net"
	"sync"
	"testing"
	"time"
)

// TestReadRegisters checks PDU encoding and the exception path without a socket.
func TestReadRegisters(t *testing.T) {
	regs := []uint16{320, 420, 100, 642, 448} // supply 32.0, return 42.0, ΔT 10.0, flow 64.2, heat 44.8 (×10)

	resp := readRegisters(0x04, []byte{0x04, 0, 0, 0, 5}, regs) // FC4, start 0, qty 5
	if resp[0] != 0x04 || resp[1] != 10 {
		t.Fatalf("bad response header: % x", resp[:2])
	}
	if got := binary.BigEndian.Uint16(resp[2:4]); got != 320 {
		t.Fatalf("reg0 (supply ×10): want 320, got %d", got)
	}

	bad := readRegisters(0x04, []byte{0x04, 0, 0, 0, 99}, regs) // qty beyond map
	if bad[0] != 0x84 || bad[1] != 0x02 {
		t.Fatalf("want exception 0x84/0x02 (illegal data address), got % x", bad)
	}
}

// TestModbusRoundTrip dials a live server and decodes a real register read -
// end-to-end proof of the fallback-protocol CDU path.
func TestModbusRoundTrip(t *testing.T) {
	s := &modbusServer{model: newLoadModel(time.Now())}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go s.serveListener(ln)

	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	// MBAP(TxID=1, Proto=0, Len=6, Unit=1) + PDU(FC4, start=0, qty=regCount)
	if _, err := conn.Write([]byte{0, 1, 0, 0, 0, 6, 1, 0x04, 0, 0, 0, regCount}); err != nil {
		t.Fatal(err)
	}

	head := make([]byte, 7)
	if _, err := io.ReadFull(conn, head); err != nil {
		t.Fatal(err)
	}
	body := make([]byte, int(binary.BigEndian.Uint16(head[4:6]))-1)
	if _, err := io.ReadFull(conn, body); err != nil {
		t.Fatal(err)
	}
	if body[0] != 0x04 || int(body[1]) != regCount*2 {
		t.Fatalf("unexpected response: fc=0x%02x bytecount=%d", body[0], body[1])
	}

	reg := func(i int) float64 { return float64(binary.BigEndian.Uint16(body[2+i*2:4+i*2])) / 10 }
	supply, flow, heat := reg(regSupplyTempC), reg(regFlowLPM), reg(regHeatkW)
	t.Logf("modbus CDU decoded: supply=%.1f°C flow=%.1f L/min heat=%.1f kW", supply, flow, heat)
	if supply < 25 || supply > 40 {
		t.Fatalf("supply temp out of range: %v", supply)
	}
	if heat <= 0 {
		t.Fatalf("heat should be positive: %v", heat)
	}
}

// TestModbusConnectionCap: with more clients than the connection cap, the gate
// queues them (it does not drop or deadlock) and every client still completes.
func TestModbusConnectionCap(t *testing.T) {
	s := &modbusServer{model: newLoadModel(time.Now()), connSem: make(chan struct{}, 4)}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go s.serveListener(ln)

	const clients = 12 // more than the cap of 4, so the semaphore is exercised
	var wg sync.WaitGroup
	for i := 0; i < clients; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			conn, err := net.Dial("tcp", ln.Addr().String())
			if err != nil {
				t.Errorf("dial: %v", err)
				return
			}
			defer conn.Close()
			if _, err := conn.Write([]byte{0, 1, 0, 0, 0, 6, 1, 0x04, 0, 0, 0, regCount}); err != nil {
				t.Errorf("write: %v", err)
				return
			}
			head := make([]byte, 7)
			if _, err := io.ReadFull(conn, head); err != nil {
				t.Errorf("read head: %v", err)
				return
			}
			body := make([]byte, int(binary.BigEndian.Uint16(head[4:6]))-1)
			if _, err := io.ReadFull(conn, body); err != nil {
				t.Errorf("read body: %v", err)
				return
			}
			if body[0] != 0x04 {
				t.Errorf("unexpected function code 0x%02x", body[0])
			}
		}()
	}
	wg.Wait()
}
