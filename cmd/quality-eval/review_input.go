package main

import (
	"errors"
	"os"
)

var errReviewInputType = errors.New("review input type")

func sameReviewInputFile(opened, canonical os.FileInfo) bool {
	return os.SameFile(opened, canonical)
}
