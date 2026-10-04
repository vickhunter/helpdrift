package main

import (
	"flag"
	"os"
)

var (
	dryRun = flag.Bool("dry-run", false, "show what would be exported")
	limit  = flag.Int("limit", 100, "maximum rows")
	debug  = os.Getenv("ACME_DEBUG") != ""
)

func ExportPDF() {}

func main() { flag.Parse(); ExportPDF() }
