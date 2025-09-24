// pull_and_save.go
//
// Pull an image from a registry and save it as a docker-compatible tarball
// into a specific folder ("downloaded-images" by default).
//
// Example:
//   go run pull_and_save.go --registry localhost:5000 --repo golang --tag 1.21-alpine
//   # saves ./downloaded-images/golang_1.21-alpine.tar
//
// Override output file with --out.

package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/google/go-containerregistry/pkg/crane"
)

func main() {
	var (
		registry = flag.String("registry", "localhost:5000", "registry host[:port]")
		repo     = flag.String("repo", "", "repository name (e.g. golang)")
		tag      = flag.String("tag", "latest", "image tag (e.g. 1.21-alpine)")
		out      = flag.String("out", "", "output tar filename (if empty, saved in ./downloaded-images/)")
		insecure = flag.Bool("insecure", true, "allow HTTP (insecure) registries; set false for TLS")
	)
	flag.Parse()

	if *repo == "" {
		log.Fatalf("please provide --repo (e.g. --repo golang)")
	}

	// If no --out given, save into ./downloaded-images/<repo>_<tag>.tar
	if *out == "" {
		folder := "downloaded-images"
		if err := os.MkdirAll(folder, 0o755); err != nil {
			log.Fatalf("failed to create output folder: %v", err)
		}
		base := fmt.Sprintf("%s_%s.tar", filepath.Base(*repo), *tag)
		*out = filepath.Join(folder, base)
	}

	refStr := fmt.Sprintf("%s/%s:%s", *registry, *repo, *tag)
	log.Printf("Pulling %s ...", refStr)

	// Build options
	var opts []crane.Option
	if *insecure {
		opts = append(opts, crane.Insecure)
	}
	opts = append(opts, crane.WithContextTimeout(10*time.Minute))

	// Pull
	img, err := crane.Pull(refStr, opts...)
	if err != nil {
		log.Fatalf("crane.Pull failed: %v", err)
	}

	// Save
	if err := crane.Save(img, refStr, *out); err != nil {
		log.Fatalf("crane.Save failed: %v", err)
	}

	log.Printf("Saved image to %s", *out)
	log.Printf("You can load it with: docker load -i %s", *out)
}

