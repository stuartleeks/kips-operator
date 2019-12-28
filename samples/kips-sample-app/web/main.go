package main

import (
	"fmt"
	"html/template"
	"io/ioutil"
	"net/http"
	"os"
)

var apiAddress = ""
var webValue = ""
var rootTemplate *template.Template

// RootContext is the context passed to the HTML template
type RootContext struct {
	WebValue string // Value configured for Web app (via WEB_VALUE env var)
	APIValue string // Value read from the API
}

func main() {
	apiAddress = os.Getenv("API_ADDRESS")
	if apiAddress == "" {
		fmt.Println("API_ADDRESS environment variable not set")
	} else {
		fmt.Printf("Using API_ADDRESS=%s\n", apiAddress)
	}

	webValue = os.Getenv("WEB_VALUE")
	if webValue == "" {
		fmt.Println("WEB_VALUE environment variable not set")
		webValue = "yodel"
	}
	fmt.Printf("Using API_VALUE=%s\n", webValue)

	rootTemplate = template.Must(template.ParseFiles("main.template.html"))

	http.HandleFunc("/", serveRoot)

	address := os.Getenv("SERVE_ADDRESS")
	if address == "" {
		address = ":8080"
	}
	fmt.Printf("Starting server on '%s' ...\n", address)
	http.ListenAndServe(address, nil)
}

func serveRoot(w http.ResponseWriter, r *http.Request) {
	context := RootContext{
		WebValue: webValue,
		APIValue: getAPIValue(),
	}
	rootTemplate.Execute(w, context)
}

func getAPIValue() string {
	if apiAddress == "" {
		return "API_ADDRESS not set"
	}

	r, err := http.Get(apiAddress)
	if err != nil {
		return err.Error()
	}
	defer r.Body.Close()

	apiValue, err := ioutil.ReadAll(r.Body)
	if err != nil {
		return err.Error()
	}
	return string(apiValue)
}
