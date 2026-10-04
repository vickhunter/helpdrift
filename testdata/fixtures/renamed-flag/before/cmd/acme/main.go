package main

import (
	"flag"
	"fmt"
)

func main() {
	output := flag.String("output", "report.csv", "where to write the export")
	verbose := flag.Bool("verbose", false, "print progress while exporting")
	flag.Parse()
	fmt.Println(*output, *verbose)
}
