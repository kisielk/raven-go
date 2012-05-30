package main

import (
	"flag"
	"fmt"
	"github.com/kisielk/raven-go/raven"
)

func main() {
	dsn := flag.String("dsn", "", "Sentry dsn")
	flag.Parse()

	if *dsn == "" {
		fmt.Printf("You need to use the --dsn flag to specify the Sentry server's dsn\n")
		return
	}

	fmt.Printf("Connecting to dsn: %v\n", *dsn)
	client, err := raven.NewRavenClient(*dsn)
	if err != nil {
		fmt.Printf("Could not connect: %v", dsn)
	}
	client.CaptureMessage("Hello world")
}
