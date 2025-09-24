// registry_crane_curl.go
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/google/go-containerregistry/pkg/crane"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"
)

// catalog response shape
type catalogResponse struct {
	Repositories []string `json:"repositories"`
}

func main() {
	var (
		registry   = flag.String("registry", "localhost:5000", "registry host[:port]")
		repo       = flag.String("repo", "", "repo to act on (e.g. golang)")
		tag        = flag.String("tag", "latest", "tag to act on (e.g. 1.21-alpine)")
		doDelete   = flag.Bool("delete", false, "delete manifest for repo:tag after resolving digest")
		doGC       = flag.Bool("gc", false, "run registry garbage-collect (docker exec <container>) AFTER delete")
		container  = flag.String("container", "local-registry", "registry container name (for docker exec gc)")
		timeoutSec = flag.Int("timeout", 10, "HTTP timeout seconds")
	)
	flag.Parse()

	ctx := context.Background()
	timeout := time.Duration(*timeoutSec) * time.Second

	base := strings.TrimSuffix(*registry, "/")
	fmt.Printf("Registry: %s\n", base)

	// 1) catalog via HTTP GET (curl equivalent)
	fmt.Println("\n==> 1. Listing catalog via HTTP GET /v2/_catalog")
	cat, err := fetchCatalog(base, timeout)
	if err != nil {
		log.Fatalf("fetchCatalog failed: %v", err)
	}
	if len(cat.Repositories) == 0 {
		fmt.Println("No repositories in catalog.")
	} else {
		for _, r := range cat.Repositories {
			fmt.Println(" -", r)
		}
	}

	// 2) For each repo, list tags using remote.List (crane ecosystem)
	fmt.Println("\n==> 2. Listing tags for each repository using remote.List()")
	for _, r := range cat.Repositories {
		repoRef := fmt.Sprintf("%s/%s", base, r)
		fmt.Printf("\nRepo: %s\n", repoRef)
		nrepo, err := name.NewRepository(repoRef, name.Insecure)
		if err != nil {
			fmt.Printf("  parsing repository failed: %v\n", err)
			continue
		}
		tags, err := remote.List(nrepo, remote.WithContext(ctx))
		if err != nil {
			fmt.Printf("  remote.List failed: %v\n", err)
			continue
		}
		if len(tags) == 0 {
			fmt.Println("  (no tags)")
			continue
		}
		for _, t := range tags {
			fmt.Printf("  %s\n", t)
		}
	}

	// If user did not ask for further action, exit now.
	if !*doDelete {
		fmt.Println("\nDone (no delete requested).")
		return
	}

	// Validate repo/tag provided
	if *repo == "" {
		log.Fatalf("please provide -repo when using -delete")
	}
	// 3) Resolve digest via HEAD (curl -I equivalent)
	fmt.Printf("\n==> 3. Resolving digest for %s:%s using HEAD /v2/<repo>/manifests/<tag>\n", *repo, *tag)
	digest, status, body, err := resolveDigestHTTP(base, *repo, *tag, timeout)
	if err != nil {
		log.Fatalf("resolveDigestHTTP failed (status %d): %v\nbody: %s", status, err, body)
	}
	fmt.Printf("Resolved digest: %s\n", digest)

	// 4) Delete using crane.Delete on repo@digest (crane library)
	delRef := fmt.Sprintf("%s/%s@%s", base, *repo, digest)
	fmt.Printf("\n==> 4. Deleting manifest via crane.Delete(%s)\n", delRef)
	if err := crane.Delete(delRef, crane.Insecure, crane.WithContext(ctx)); err != nil {
		// helpful guidance for common errors
		es := err.Error()
		if strings.Contains(es, "UNSUPPORTED") {
			log.Fatalf("crane.Delete returned UNSUPPORTED: deletes likely disabled on registry. Enable with REGISTRY_STORAGE_DELETE_ENABLED=true or storage.delete.enabled: true in config.yml. Error: %v", err)
		}
		if strings.Contains(es, "DIGEST_INVALID") {
			log.Fatalf("crane.Delete returned DIGEST_INVALID: server rejected digest. Ensure HEAD used proper Accept header and the digest matches server's manifest. Error: %v", err)
		}
		log.Fatalf("crane.Delete failed: %v", err)
	}
	fmt.Println("Delete request accepted by registry (HTTP 202).")

	// 5) Optionally run garbage-collect via docker exec inside registry container
	if *doGC {
		fmt.Printf("\n==> 5. Running garbage-collect inside container '%s'\n", *container)
		if err := runGarbageCollect(*container); err != nil {
			log.Fatalf("runGarbageCollect failed: %v", err)
		}
		fmt.Println("Garbage-collect completed (check container logs/output).")
	}

	fmt.Println("\nAll done.")
}

// fetchCatalog GET /v2/_catalog
func fetchCatalog(reg string, timeout time.Duration) (*catalogResponse, error) {
	url := fmt.Sprintf("http://%s/v2/_catalog", reg)
	client := &http.Client{Timeout: timeout}
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, fmt.Errorf("catalog returned status %d: %s", resp.StatusCode, string(b))
	}
	var c catalogResponse
	if err := json.NewDecoder(resp.Body).Decode(&c); err != nil {
		return nil, err
	}
	return &c, nil
}

// resolveDigestHTTP performs HEAD /v2/<repo>/manifests/<tag> and returns Docker-Content-Digest header value
func resolveDigestHTTP(reg, repo, tag string, timeout time.Duration) (digest string, status int, body string, err error) {
	url := fmt.Sprintf("http://%s/v2/%s/manifests/%s", reg, repo, tag)
	req, err := http.NewRequest(http.MethodHead, url, nil)
	if err != nil {
		return "", 0, "", err
	}
	// match your curl Accept
	req.Header.Set("Accept", "application/vnd.docker.distribution.manifest.v2+json,application/vnd.oci.image.manifest.v1+json")

	client := &http.Client{Timeout: timeout}
	resp, err := client.Do(req)
	if err != nil {
		return "", 0, "", err
	}
	defer resp.Body.Close()

	bs, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
	status = resp.StatusCode
	if status >= 400 {
		return "", status, string(bs), fmt.Errorf("manifest HEAD returned status %d", status)
	}
	d := resp.Header.Get("Docker-Content-Digest")
	if d == "" {
		d = resp.Header.Get("Content-Digest")
	}
	if d == "" {
		return "", status, string(bs), fmt.Errorf("digest header not found; headers: %v", resp.Header)
	}
	fmt.Println("Digest header found:", d)
	return d, status, string(bs), nil
}

// runGarbageCollect executes: docker exec -it <container> /bin/registry garbage-collect --delete-untagged /etc/docker/registry/config.yml
func runGarbageCollect(container string) error {
	// Build the command. We avoid -it in programmatic execution to keep it non-interactive.
	// If you want streaming output, set Stdout/Stderr to os.Stdout/os.Stderr.
	cmd := exec.Command("docker", "exec", "5a77465126df", "/bin/registry", "garbage-collect", "--delete-untagged", "/etc/docker/registry/config.yml")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	fmt.Printf("Running command: %s\n", strings.Join(cmd.Args, " "))
	return cmd.Run()
}
