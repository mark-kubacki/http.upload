// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package auth

import (
	"fmt"
	"math"
	"testing"
)

func exampleAbs64() {
	fmt.Printf("%d", abs64(-1)) // 😃
	// Output: 1
}

var fromTo64 = []struct {
	given    int64
	expected uint64
}{
	{0, 0},
	{-0, 0},
	{1, 1},
	{-1, 1},
	{math.MaxInt64, math.MaxInt64},
	{-math.MaxInt64, math.MaxInt64},
	{-math.MaxInt64 - 1, math.MaxInt64 + 1},
}

func TestAbs64Inductive(t *testing.T) {
	for _, pair := range fromTo64 {
		got := abs64(pair.given)
		if got != pair.expected {
			t.Errorf("Abs64(%v) = %v, expected %v", pair.given, got, pair.expected)
		}
	}
}

var fromTo32 = []struct {
	given    int32
	expected uint32
}{
	{0, 0},
	{-0, 0},
	{1, 1},
	{-1, 1},
	{math.MaxInt32, math.MaxInt32},
	{-math.MaxInt32, math.MaxInt32},
	{-math.MaxInt32 - 1, math.MaxInt32 + 1},
}

func TestAbs32Inductive(t *testing.T) {
	for _, pair := range fromTo32 {
		got := abs32(pair.given)
		if got != pair.expected {
			t.Errorf("Abs32(%v) = %v, expected %v", pair.given, got, pair.expected)
		}
	}
}
