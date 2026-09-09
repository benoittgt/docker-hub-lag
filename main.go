package main

import (
	"encoding/csv"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/stream"
	"github.com/google/go-containerregistry/pkg/v1/types"
)

const (
	repoName     = "docker-hub-lag"
	csvPath      = "data/lag.csv"
	timeout      = 60 * time.Second
	keepTags     = 10
	pollInterval = 500 * time.Millisecond
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
	tag := fmt.Sprintf("lag-%d", time.Now().Unix())
	ref := fmt.Sprintf("docker.io/%s/%s:%s", user, repoName, tag)

	if verbose {
		fmt.Fprintf(os.Stderr, "pushing %s\n", ref)
	}

	pushDuration, err := pushImage(ref, pat)
	if err != nil {
		return fmt.Errorf("push failed: %w", err)
	}

	if verbose {
		fmt.Fprintf(os.Stderr, "push completed in %dms\n", pushDuration.Milliseconds())
	}

	hubLag, registryLag := measureLag(tag, pat)

	if verbose {
		fmt.Fprintf(os.Stderr, "hub API lag: %dms, registry lag: %dms\n", hubLag, registryLag)
	}

	if err := recordResult(tag, pushDuration, hubLag, registryLag); err != nil {
		return fmt.Errorf("recording result: %w", err)
	}

	return nil
}

func pushImage(ref string, pat string) (time.Duration, error) {
	imgRef, err := name.ParseReference(ref)
	if err != nil {
		return 0, err
	}

	layer := stream.NewLayer(
		emptyReader{},
		stream.WithMediaType(types.OCILayer),
	)

	img, err := mutate.AppendLayers(empty.Image, layer)
	if err != nil {
		return 0, err
	}

	auth := &authn.Basic{Username: user, Password: pat}

	start := time.Now()
	err = remote.Write(imgRef, img, remote.WithAuth(auth))
	duration := time.Since(start)

	return duration, err
}

type emptyReader struct{}

func (emptyReader) Read(p []byte) (int, error) { return 0, io.EOF }
func (emptyReader) Close() error                { return nil }

func measureLag(tag string, pat string) (hubLagMs int64, registryLagMs int64) {
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		hubLagMs = pollHubAPI(tag)
	}()

	go func() {
		defer wg.Done()
		registryLagMs = pollRegistryManifest(tag, pat)
	}()

	wg.Wait()
	return
}

func pollHubAPI(tag string) int64 {
	url := fmt.Sprintf("https://hub.docker.com/v2/repositories/%s/%s/tags/%s", user, repoName, tag)
	client := &http.Client{Timeout: 10 * time.Second}

	start := time.Now()
	deadline := start.Add(timeout)

	for time.Now().Before(deadline) {
		resp, err := client.Get(url)
		if err != nil {
			if verbose {
				fmt.Fprintf(os.Stderr, "  hub API poll: error %v\n", err)
			}
			time.Sleep(pollInterval)
			continue
		}
		resp.Body.Close()

		if verbose {
			fmt.Fprintf(os.Stderr, "  hub API poll: %d (%dms)\n", resp.StatusCode, time.Since(start).Milliseconds())
		}

		if resp.StatusCode == http.StatusOK {
			return time.Since(start).Milliseconds()
		}

		time.Sleep(pollInterval)
	}

	return -1
}

func pollRegistryManifest(tag string, pat string) int64 {
	ref, err := name.ParseReference(fmt.Sprintf("docker.io/%s/%s:%s", user, repoName, tag))
	if err != nil {
		return -1
	}

	auth := &authn.Basic{Username: user, Password: pat}
	start := time.Now()
	deadline := start.Add(timeout)

	for time.Now().Before(deadline) {
		_, err := remote.Head(ref, remote.WithAuth(auth))

		if verbose {
			if err != nil {
				fmt.Fprintf(os.Stderr, "  registry poll: error (%dms)\n", time.Since(start).Milliseconds())
			} else {
				fmt.Fprintf(os.Stderr, "  registry poll: found (%dms)\n", time.Since(start).Milliseconds())
			}
		}

		if err == nil {
			return time.Since(start).Milliseconds()
		}

		time.Sleep(pollInterval)
	}

	return -1
}

func recordResult(tag string, pushDuration time.Duration, hubLagMs, registryLagMs int64) error {
	if err := os.MkdirAll(filepath.Dir(csvPath), 0755); err != nil {
		return err
	}

	writeHeader := false
	if _, err := os.Stat(csvPath); os.IsNotExist(err) {
		writeHeader = true
	}

	f, err := os.OpenFile(csvPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	w := csv.NewWriter(f)
	defer w.Flush()

	if writeHeader {
		if err := w.Write([]string{"timestamp", "tag", "push_ms", "hub_api_lag_ms", "registry_lag_ms"}); err != nil {
			return err
		}
	}

	return w.Write([]string{
		time.Now().UTC().Format(time.RFC3339),
		tag,
		fmt.Sprintf("%d", pushDuration.Milliseconds()),
		fmt.Sprintf("%d", hubLagMs),
		fmt.Sprintf("%d", registryLagMs),
	})
}

func runGraph() error {
	fmt.Println("graph mode not yet implemented")
	return nil
}
