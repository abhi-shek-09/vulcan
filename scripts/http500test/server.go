package main

import (
	"fmt"
	"log"
	"net/http"
)

func main() {
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintln(w, "FAIL")
	})

	log.Println("Failure target listening on :9091...")
	log.Fatal(http.ListenAndServe(":9091", nil))
}
