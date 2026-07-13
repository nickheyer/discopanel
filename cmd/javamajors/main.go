package main

import (
	"flag"
	"fmt"
	"strings"

	"github.com/nickheyer/discopanel/internal/docker"
)

// Prints published Java majors, the single list consumers share
func main() {
	graal := flag.Bool("graal", false, "print graal majors")
	flag.Parse()
	versions := docker.SupportedJavaVersions
	if *graal {
		versions = docker.GraalJavaVersions
	}
	out := make([]string, len(versions))
	for i, v := range versions {
		out[i] = fmt.Sprint(v)
	}
	fmt.Println(strings.Join(out, " "))
}
