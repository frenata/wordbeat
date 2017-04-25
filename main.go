package main

import (
	"os"

	"github.com/elastic/beats/libbeat/beat"

	"github.com/frenata/wordbeat/beater"
)

func main() {
	err := beat.Run("wordbeat", "", beater.New)
	if err != nil {
		os.Exit(1)
	}
}
