package main

import (
	"fmt"
	"net/http"
	"os"
)

func main() {
	apiValue := os.Getenv("API_VALUE")
	if apiValue == "" {
		fmt.Println("API_VALUE environment variable not set")
		apiValue = "yodel"
	}
	fmt.Printf("Using API_VALUE=%s\n", apiValue)

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, apiValue)
	})

	address := os.Getenv("SERVE_ADDRESS")
	if address == "" {
		address = ":8080"
	}
	fmt.Printf("Starting server on '%s' ...\n", address)
	http.ListenAndServe(address, nil)
}
