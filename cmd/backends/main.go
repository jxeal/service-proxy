package main

import (
	"flag"
	"log"
	"net/http"
	"strings"
	"time"
)

func main() {
	portsFlag := flag.String("ports", "9001,9002,9003", "comma-separated backend ports")
	delaysFlag := flag.String("delays", "5ms,50ms,200ms", "comma-separated response delays")
	flag.Parse()

	ports := strings.Split(*portsFlag, ",")
	delayStrs := strings.Split(*delaysFlag, ",")

	for i, port := range ports {
		port = strings.TrimSpace(port)
		delayStr := strings.TrimSpace(delayStrs[i%len(delayStrs)])
		delay, err := time.ParseDuration(delayStr)
		if err != nil {
			log.Fatalf("Invalid delay %q: %v", delayStr, err)
		}
		body := "backend-" + port

		mux := http.NewServeMux()
		handler := func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(delay)
			w.Write([]byte(body))
		}
		mux.HandleFunc("/", handler)
		mux.HandleFunc("/get", handler)

		go func(p string, m *http.ServeMux) {
			addr := "0.0.0.0:" + p
			log.Printf("backend %s listening on %s", body, addr)
			if err := http.ListenAndServe(addr, m); err != nil {
				log.Fatalf("backend %s failed: %v", p, err)
			}
		}(port, mux)
	}

	select {}
}
