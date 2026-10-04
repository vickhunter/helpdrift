package main

import "flag"

var limit = flag.Int("limit", 100, "maximum rows")

func ExportDocument() {}

func main() { flag.Parse(); ExportDocument() }
