// Command densewatch-sim emits believable, physically-consistent telemetry for
// a high-density AI pod — a Redfish CoolingUnit tree and dcgm-exporter-style GPU
// metrics — so densewatch can be built and demoed with zero hardware (spec §5-C).
package main

import (
	"flag"
	"log"
	"net/http"
	"time"
)

func main() {
	redfishAddr := flag.String("redfish-addr", ":5000", "Redfish CoolingUnit simulator listen address")
	dcgmAddr := flag.String("dcgm-addr", ":9400", "dcgm-exporter-style metrics listen address")
	modbusAddr := flag.String("modbus-addr", ":5020", "Modbus-TCP CDU simulator (fallback-protocol CDU) listen address")
	flag.Parse()

	model := newLoadModel(time.Now())

	rmux := http.NewServeMux()
	(&redfishServer{model: model}).routes(rmux)

	dmux := http.NewServeMux()
	dmux.HandleFunc("GET /metrics", (&dcgmServer{model: model}).metrics)
	dmux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("ok\n")) })

	errc := make(chan error, 3)
	go func() {
		log.Printf("redfish  CoolingUnit sim  →  http://localhost%s/redfish/v1/ThermalEquipment/CDUs/1", *redfishAddr)
		errc <- http.ListenAndServe(*redfishAddr, rmux)
	}()
	go func() {
		log.Printf("dcgm     metrics sim      →  http://localhost%s/metrics", *dcgmAddr)
		errc <- http.ListenAndServe(*dcgmAddr, dmux)
	}()
	go func() {
		log.Printf("modbus   CDU sim (FC3/4)  →  modbus-tcp://localhost%s  (read input registers 0–%d)", *modbusAddr, regCount-1)
		errc <- (&modbusServer{model: model}).serve(*modbusAddr)
	}()
	log.Fatal(<-errc)
}
