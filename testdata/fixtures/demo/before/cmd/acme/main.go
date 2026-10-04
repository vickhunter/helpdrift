package main

import (
	"flag"
	"fmt"
	"os"
)

func main() {
	fs := flag.NewFlagSet("export", flag.ExitOnError)
	output := fs.String("output", "invoices.csv", "file to write")
	since := fs.String("since", "30d", "only export invoices newer than this")
	fs.Parse(os.Args[2:])
	fmt.Println("exporting to", *output, "since", *since)
}
