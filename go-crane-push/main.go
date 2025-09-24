package main

import (
	"context"
	"fmt"
	"log"

	"github.com/google/go-containerregistry/pkg/crane"
)

func main() {
	ctx := context.Background()

	// 1) Pull a public image (from Docker Hub)
	src := "busybox:latest"
	fmt.Println("Pulling", src)
	img, err := crane.Pull(src, crane.WithContext(ctx))
	if err != nil {
		log.Fatalf("crane.Pull: %v", err)
	}

	// 2) Destination tag in local registry
	dst := "localhost:5000/mybusybox:latest"
	fmt.Println("Pushing", dst)

	// If your registry is using plain HTTP (no TLS), include crane.Insecure option.
	// If you have auth you'd add WithAuth(...) or WithAuthFromKeychain(...)
	if err := crane.Push(img, dst, crane.Insecure, crane.WithContext(ctx)); err != nil {
		log.Fatalf("crane.Push: %v", err)
	}

	fmt.Println("Push complete:", dst)
}

