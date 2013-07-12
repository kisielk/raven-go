package main

import (
	"fmt"
	"github.com/kisielk/raven-go/raven"
	"os"
	"strings"
)

func main() {

	var dsn string
	if len(os.Args) >= 2 {
		dsn = strings.Join(os.Args[1:], " ")
	} else {
		dsn = os.Getenv("SENTRY_DSN")
	}

	if dsn == "" {
		fmt.Printf("Error: No configuration detected!\n")
		fmt.Printf("You must either pass a DSN to the command, or set the SENTRY_DSN environment variable\n")
		return
	}

	fmt.Printf("Using DSN configuration:\n %v\n", dsn)
	client, err := raven.NewClient(dsn)

	if err != nil {
		fmt.Printf("could not connect: %v", dsn)
		return
	}

	fmt.Printf("Sending a test message...\n")
	id, err := client.CaptureMessage("This is a test message generated using ``goraven test``")

	if err != nil {
		fmt.Printf("failed: %v\n", err)
		return
	}

	fmt.Printf("Message captured, id: %v", id)
}
