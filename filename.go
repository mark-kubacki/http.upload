package upload // import "blitznote.com/src/caddy.upload"

import (
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

const (
	// Runes that are not safe to use with network shares.
	//
	// Please note that '/' is already discarded at an earlier stage.
	AlwaysRejectRunes = `"*:<>?|\`

	runeSpatium = '\u2009'
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
