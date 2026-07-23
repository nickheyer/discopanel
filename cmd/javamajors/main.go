package main

import (
	"flag"
	"fmt"
	"strings"

	"github.com/nickheyer/discopanel/pkg/javaversions"
)

// Prints published Java majors, the single list consumers share
func main() {
	graal := flag.Bool("graal", false, "print graal majors")
	flag.Parse()
	versions := javaversions.Supported
	if *graal {
		versions = javaversions.Graal
	}
	out := make([]string, len(versions))
	for i, v := range versions {
		out[i] = fmt.Sprint(v)
	}
	fmt.Println(strings.Join(out, " "))
}
