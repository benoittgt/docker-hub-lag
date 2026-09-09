package main

import (
	"flag"
	"fmt"
	"os"
)

const (
	repoName     = "docker-hub-lag"
	csvPath      = "data/lag.csv"
	timeout      = 60 // seconds
	keepTags     = 10
	pollInterval = 500 // milliseconds
)

var (
	user    string
	verbose bool
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "graph" {
		if err := runGraph(); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	flag.StringVar(&user, "user", "", "Docker Hub username")
	flag.BoolVar(&verbose, "verbose", false, "print detailed output")
	flag.Parse()

	if user == "" {
		fmt.Fprintf(os.Stderr, "error: --user is required\n")
		os.Exit(1)
	}

	pat := os.Getenv("PAT")
	if pat == "" {
		fmt.Fprintf(os.Stderr, "error: PAT environment variable is required\n")
		os.Exit(1)
	}

	if err := runCheck(pat); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func runCheck(pat string) error {
	fmt.Println("check mode not yet implemented")
	return nil
}

func runGraph() error {
	fmt.Println("graph mode not yet implemented")
	return nil
}
