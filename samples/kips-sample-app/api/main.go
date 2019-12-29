package main

import (
	"fmt"
	"log"
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
		log.Printf("Serving response: ApiValue=%s\n", apiValue)
		fmt.Fprintf(w, apiValue)
	})

	address := os.Getenv("SERVE_ADDRESS")
	if address == "" {
		address = ":9000"
	}
	fmt.Printf("Starting server on '%s' ...\n", address)
	err := http.ListenAndServe(address, nil)
	if err != nil {
		fmt.Println(err)
	}
}
