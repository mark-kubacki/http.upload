package upload // import "blitznote.com/src/caddy.upload"

import (
	"testing"
	"unicode"

	"golang.org/x/text/unicode/norm"

	. "github.com/smartystreets/goconvey/convey"
)

func TestIsAcceptableFilename(t *testing.T) {
	Convey("IsAcceptableFilename", t, FailureContinues, func() {
		Convey("handles Latin-1 input correctly", FailureContinues, func() {
			samples := []struct {
				input    string
				returned bool
			}{
				// ASCII
				{"file.name", true},
				{"the space", true},
				{"line\nbreak", false},
				{"the\tTAB", false},
				{"Samba?", false},
				{"not print\x0e.", false}, {"fancier not print\u000e.", false},
				{"a null\x00.", false},
				{"form feed\x0c", false},
				// now comes Latin-1
				{"start \xb0", false}, {"end \xdf", false}, // obsolete blocks, like in old terminal programs
				{"stray box \xfe", false},
			}

			for i, tuple := range samples {
				tuple.returned = IsAcceptableFilename(samples[i].input, nil, nil)
				So(tuple, ShouldResemble, samples[i])
			}
		})

		Convey("accepts correct UTF-8 input", FailureContinues, func() {
			samples := []struct {
				input    string
				returned bool
			}{
				{"W. Mark Kubacki", true}, {"J. Edgar", true},
				{"keyboard → „typewriters’ keylayout“ ≠ »DIN T2 you ought better buy«", true},
				{"Döner macht schöner.", true},
				{"GENUẞMITTEL Kauﬂäche häuﬁg ǲerba", true}, // ligatures (capital ß after 1900 for historic documents)
				{"フ\u30d7", true}, {"プ\u30d5\u309a", true},
			}

			for i, tuple := range samples {
				tuple.returned = IsAcceptableFilename(samples[i].input, nil, nil)
				So(tuple, ShouldResemble, samples[i])
			}
		})

		Convey("rejects undesired runes", FailureContinues, func() {
			samples := []struct {
				input    string
				returned bool
			}{
				{"form\xfffeed", false}, {"feed\u000cform", false},
				{"IND\u0084", false}, {"NEL\u0085", false},
				{"line\u2028", false}, {"paragraph\u2029", false},
			}

			for i, tuple := range samples {
				tuple.returned = IsAcceptableFilename(samples[i].input, nil, nil)
				So(tuple, ShouldResemble, samples[i])
			}
		})

		Convey("allows to restrict the acceptable rune ranges", FailureContinues, func() {
			azOnly := unicode.RangeTable{
				R16: []unicode.Range16{
					{0x0061, 0x007a, 1}, // a-z
				},
				LatinOffset: 1,
			}

			samples := []struct {
				input    string
				restrict []*unicode.RangeTable
				returned bool
			}{
				{"az", []*unicode.RangeTable{&azOnly}, true},
				{"äz", []*unicode.RangeTable{&azOnly}, false},
			}

			for i, tuple := range samples {
				tuple.returned = IsAcceptableFilename(samples[i].input, samples[i].restrict, nil)
				So(tuple, ShouldResemble, samples[i])
			}
		})

		Convey("enforces inputs that are normalized under a Form", FailureContinues, func() {
			samples := []struct {
				input    string
				form     norm.Form
				returned bool
			}{
				{"säet", norm.NFC, true},
				{"säet", norm.NFD, false},
			}

			for i, tuple := range samples {
				tuple.returned = IsAcceptableFilename(samples[i].input, nil, &samples[i].form)
				So(tuple, ShouldResemble, samples[i])
			}
		})
	})
}

func TestParseUnicodeBlockList(t *testing.T) {
	Convey("ParseUnicodeBlockList works", t, FailureContinues, func() {
		samples := []struct {
			input string
			table *unicode.RangeTable
			err   error
		}{
			{`x0000-x007F x0100-x017F x2152-x217F:2  xf0000-xf0010 // don't use this`, &unicode.RangeTable{
				R16: []unicode.Range16{
					{0x0000, 0x007f, 1},
					{0x0100, 0x017f, 1},
					{0x2152, 0x217f, 2},
				},
				R32: []unicode.Range32{
					{Lo: 0xf0000, Hi: 0xf0010, Stride: 1},
				},
				LatinOffset: 1,
			}, nil},
		}

		for i, tuple := range samples {
			tuple.table, tuple.err = ParseUnicodeBlockList(samples[i].input)
			So(tuple, ShouldResemble, samples[i])
		}
	})
}
