package upload // import "blitznote.com/src/caddy.upload"

import (
	"errors"
	"math"
	"sort"
	"strconv"
	"strings"
	"text/scanner"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

const (
	// AlwaysRejectRunes contains runes that are not safe to use with network shares.
	//
	// Please note that '/' is already discarded at an earlier stage.
	AlwaysRejectRunes = `"*:<>?|\`

	runeSpatium = '\u2009'

	errStrUnexpectedRange = "Unexpected Unicode range: "
)

// Happen when parsing ranges.
var (
	errOutOfBounds = errors.New("Value out of bounds")
)

// Not all runes in unicode.PrintRanges are suitable for filenames.
// They are collected here.
var excludedRunes = &unicode.RangeTable{
	R16: []unicode.Range16{
		{0x2028, 0x202f, 1}, // new line, paragraph etc.
		{0xfff0, 0xffff, 1}, // specials, and invalid (includes the obsolete (invalid) terminal boxes)
	},
	LatinOffset: 0,
}

// IsAcceptableFilename is used to enforce filenames in wanted alphabet(s).
// Setting 'reduceAcceptableRunesTo' reduces the supremum unicode.PrintRanges.
//
// A string with runes other than U+0020 (space) or U+2009 (spatium)
// representing space will be rejected.
//
// Filenames are not transliterated to prevent loops within clusters of mirrors.
func IsAcceptableFilename(s string, reduceAcceptableRunesTo []*unicode.RangeTable,
	enforceForm *norm.Form) bool {
	// most of the Internet is in NFC
	// (though that even changes within pages, for example for Japanese names)
	if enforceForm != nil && !enforceForm.IsNormalString(s) {
		return false
	}

	if reduceAcceptableRunesTo != nil {
		for _, r := range s {
			if !unicode.In(r, reduceAcceptableRunesTo...) {
				return false
			}
		}
	}

	for _, r := range s {
		if uint32(r) <= unicode.MaxLatin1 && strings.ContainsRune(AlwaysRejectRunes, r) {
			return false
		}
		if r == runeSpatium {
			continue
		}
		if unicode.Is(excludedRunes, r) ||
			!unicode.IsPrint(r) { // this takes care of the "spaces" as well
			return false
		}
	}

	return true
}

type tupleForRangeSlice [][3]uint64

func (a tupleForRangeSlice) Len() int      { return len(a) }
func (a tupleForRangeSlice) Swap(i, j int) { a[i], a[j] = a[j], a[i] }
func (a tupleForRangeSlice) Less(i, j int) bool {
	for n := range a[i] {
		if a[i][n] < a[j][n] {
			return true
		}
		if a[i][n] > a[j][n] {
			return false
		}
	}
	return false
}

// ParseUnicodeBlockList naïvely translates a string with space-delimited Unicode ranges to Go's unicode.RangeTable.
//
// All elements must fit into uint32.
// A Range must begin with its lower bound, and ranges must not overlap (we don't check this here!).
//
// The format of one range is as follows, with 'stride' being set to '1' if left empty.
//  <low>-<high>[:<stride>]
func ParseUnicodeBlockList(str string) (*unicode.RangeTable, error) {
	haveRanges := make(tupleForRangeSlice, 0, strings.Count(str, " "))

	// read
	var s scanner.Scanner
	s.Init(strings.NewReader(str))
	tok := s.Scan()
	for tok != scanner.EOF {
		var (
			low, high, stride uint64
			err               error
		)

		if tok != scanner.Ident {
			return nil, errors.New(errStrUnexpectedRange + s.Pos().String())
		}
		if low, err = strconv.ParseUint(strings.TrimLeft(s.TokenText(), "uU+x"), 16, 32); err != nil {
			return nil, errors.New(errStrUnexpectedRange + s.Pos().String())
		}

		tok = s.Scan()
		if !(tok == '-' || tok == '–') {
			return nil, errors.New(errStrUnexpectedRange + s.Pos().String())
		}

		tok = s.Scan()
		if tok != scanner.Ident {
			return nil, errors.New(errStrUnexpectedRange + s.Pos().String())
		}
		if high, err = strconv.ParseUint(strings.TrimLeft(s.TokenText(), "uU+x"), 16, 32); err != nil {
			return nil, errors.New(errStrUnexpectedRange + s.Pos().String())
		}

		tok = s.Scan()
		if tok != ':' {
			haveRanges = append(haveRanges, [3]uint64{low, high, 1})
			continue
		}

		tok = s.Scan()
		if tok != scanner.Int {
			return nil, errors.New(errStrUnexpectedRange + s.Pos().String())
		}
		if stride, err = strconv.ParseUint(s.TokenText(), 10, 32); err != nil {
			return nil, errors.New(errStrUnexpectedRange + s.Pos().String())
		}

		haveRanges = append(haveRanges, [3]uint64{low, high, stride})

		tok = s.Scan()
	}

	sort.Sort(haveRanges)

	// fold
	rt := unicode.RangeTable{}
	for i := range haveRanges {
		switch {
		case haveRanges[i][1] <= unicode.MaxLatin1:
			rt.LatinOffset++
			fallthrough
		case haveRanges[i][1] <= math.MaxUint16:
			if rt.R16 == nil {
				rt.R16 = []unicode.Range16{}
			}
			rt.R16 = append(rt.R16, unicode.Range16{
				Lo:     uint16(haveRanges[i][0]),
				Hi:     uint16(haveRanges[i][1]),
				Stride: uint16(haveRanges[i][2]),
			})
		case haveRanges[i][1] <= math.MaxUint32:
			if rt.R32 == nil {
				rt.R32 = []unicode.Range32{}
			}
			rt.R32 = append(rt.R32, unicode.Range32{
				Lo:     uint32(haveRanges[i][0]),
				Hi:     uint32(haveRanges[i][1]),
				Stride: uint32(haveRanges[i][2]),
			})
		default:
			return nil, errOutOfBounds
		}
	}

	return &rt, nil
}
