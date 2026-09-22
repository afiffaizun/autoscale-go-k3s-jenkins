package main

import (
	"fmt"
	"log"
	"net/http"
	"time"
)

func homeHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintln(w, "Hello from Go CI/CD v3")
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	fmt.Fprintln(w, "OK")
}

func cpuHandler(w http.ResponseWriter, r *http.Request) {
	end := time.Now().Add(5 * time.Second)

	for time.Now().Before(end) {
		for i := 0; i < 1000000; i++ {
			_ = i * i
		}
	}

	fmt.Fprintln(w, "CPU load generated")
}

func main() {
	http.HandleFunc("/", homeHandler)
	http.HandleFunc("/health", healthHandler)
	http.HandleFunc("/cpu", cpuHandler)
	port := ":8080"

	log.Printf("Server running on port %s", port)

	if err := http.ListenAndServe(port, nil); err != nil {
		log.Fatal(err)
	}
}
