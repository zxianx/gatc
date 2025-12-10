package main

import (
	"os"
	"strconv"
	"time"
)

var (
	BatchCollectTimeout = 120 * time.Second
	BatchMaxSize        = 20
	Debug               = false
)

func init() {
	if tmp := os.Getenv("BatchCollectTimeout"); tmp != "" {
		second, err := strconv.Atoi(tmp)
		if err != nil || second < 0 {
			panic("env BatchCollectTimeout (second) must be a non-negative integer , empty default 120 ")
		}
		BatchCollectTimeout = time.Duration(second) * time.Second
	}
	if tmp := os.Getenv("BatchMaxSize"); tmp != "" {
		second, err := strconv.Atoi(tmp)
		if err != nil || second < 0 {
			panic("env BatchMaxSize must be a non-negative integer, empty default 20 ")
		}
		BatchMaxSize = second
	}
	if tmp := os.Getenv("Debug"); tmp == "true" {
		Debug = true
	}
}
