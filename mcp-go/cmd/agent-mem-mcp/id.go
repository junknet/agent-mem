package main

import (
	"fmt"
	"strings"

	"github.com/google/uuid"
)

func newID() string {
	value := uuid.NewString()
	return strings.ReplaceAll(value, "-", "")
}

func newMemoryID() string {
	return "mem_" + newID()
}

func newFragmentID(index int) string {
	return fmt.Sprintf("frag_%s_%d", newID(), index)
}
