// delete_resolve_remote.go
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/google/go-containerregistry/pkg/crane"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"
)

func main() {
	var refStr string
	flag.StringVar(&refStr, "ref", "localhost:5000/mybusybox:latest", "image reference (host:port/repo:tag)")
	flag.Parse()

	ctx := context.Background()

	// Parse the reference (allow insecure (HTTP) registries)
	ref, err := name.ParseReference(refStr, name.Insecure)
	if err != nil {
		log.Fatalf("parsing reference %q: %v", refStr, err)
	}

	// Resolve manifest descriptor via remote.Head (this asks the registry for descriptor info)
	// Use a short timeout via context
	ctxHead, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	desc, err := remote.Head(ref, remote.WithContext(ctxHead))
	if err != nil {
		// Try to give helpful info if 404 / MANIFEST_UNKNOWN
		if strings.Contains(err.Error(), "MANIFEST_UNKNOWN") || strings.Contains(err.Error(), "404") {
			log.Fatalf("manifest not found for %s: %v\nMake sure the tag exists (docker push localhost:5000/<repo>:<tag>)", refStr, err)
		}
		log.Fatalf("remote.Head failed: %v", err)
	}

	digest := desc.Digest
	if digest == "" {
		log.Fatalf("no digest found in descriptor for %s", refStr)
	}

	fmt.Printf("Resolved digest: %s\n", digest.String())

	// Print tags (before delete)
	if err := printTags(ref); err != nil {
		fmt.Printf("Warning: unable to list tags: %v\n", err)
	}

	// Build digest reference like "host:port/repo@sha256:..."
	delRef := fmt.Sprintf("%s@%s", ref.Context().Name(), digest.String())
	fmt.Printf("Deleting manifest: %s\n", delRef)

	// Delete using crane (insecure allowed)
	if err := crane.Delete(delRef, crane.Insecure); err != nil {
		// Provide guidance if deletes disabled
		if strings.Contains(err.Error(), "UNSUPPORTED") {
			log.Fatalf("crane.Delete failed: %v\nRegistry likely has deletes disabled. Recreate registry with REGISTRY_STORAGE_DELETE_ENABLED=true or add storage.delete.enabled: true in config.yml", err)
		}
		log.Fatalf("crane.Delete failed: %v", err)
	}

	fmt.Println("Delete request sent successfully.")

	// Print tags (after delete) to show tag removed
	if err := printTags(ref); err != nil {
		fmt.Printf("Warning: unable to list tags after delete: %v\n", err)
	}
}

// printTags prints the tags list for the repository of ref (uses remote.List)
func printTags(ref name.Reference) error {
	repo := ref.Context()
	fmt.Printf("\nTags for repo %s:\n", repo.Name())
	tags, err := remote.List(repo)
	if err != nil {
		return err
	}
	if len(tags) == 0 {
		fmt.Println("  (no tags)")
		return nil
	}
	for _, t := range tags {
		fmt.Printf("  %s\n", t)
	}
	return nil
}

