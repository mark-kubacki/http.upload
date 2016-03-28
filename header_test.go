package upload // import "blitznote.com/src/caddy.upload"

import (
	"encoding/base64"
	"net/http"
	"strconv"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestAuthHeaderSerialization(t *testing.T) {
	valid := []struct {
		serialized   string // meant for http.Header
		deserialized AuthorizationHeader
	}{
		{`Signature keyId="(key=id)",algorithm="hmac-sha256",headers="timestamp token",signature="TWFyaw=="`,
			AuthorizationHeader{KeyId: "(key=id)", Algorithm: "hmac-sha256",
				HeadersToSign: []string{"timestamp", "token"},
				Signature:     []byte("Mark")},
		},
		{`Signature keyId="(key=id)", algorithm="hmac-sha256",  extensions="",
			headers="timestamp token",signature="TWFyaw=="`,
			AuthorizationHeader{KeyId: "(key=id)", Algorithm: "hmac-sha256",
				HeadersToSign: []string{"timestamp", "token"},
				Signature:     []byte("Mark")},
		},
	}

	Convey("Authorization header conversion", t, func() {
		Convey("works from string to struct with valid inputs", func() {
			for _, row := range valid {
				var fresh AuthorizationHeader
				err := fresh.Parse(row.serialized)
				So(fresh.KeyId[0], ShouldNotEqual, '"')
				So(err, ShouldBeNil)
				So(fresh, ShouldResemble, row.deserialized)
			}
		})
	})
}

func TestAuthHeaderChecks(t *testing.T) {
	valid := []struct {
		key       string
		timestamp uint64
		token     string
		sig       string
	}{
		{"geheim", 1458508452, "streng", "yql3kIDweM8KYm+9pHzX0PKNskYAU46Jb5D6nLftTvo="},
	}
	forged := []struct {
		key       string
		timestamp uint64
		token     string
		sig       string
	}{
		// key, timestamp, token, → signature
		{valid[0].key + "!", valid[0].timestamp, valid[0].token, valid[0].sig},
		{valid[0].key, valid[0].timestamp + 900, valid[0].token, valid[0].sig},
		{valid[0].key, valid[0].timestamp, valid[0].token + "!", valid[0].sig},
		{valid[0].key, valid[0].timestamp, valid[0].token, "MBfCB6Txi1rTKf6gDdMxE/SPUdePCFQFLdGkP7mXsI0="},
	}

	Convey("An sufficiently specified Authorization header", t, func() {
		Convey("is satisfied by valid inputs", func() {
			for _, row := range valid {
				signature, _ := base64.StdEncoding.DecodeString(row.sig)
				a := AuthorizationHeader{Algorithm: "hmac-sha256", HeadersToSign: []string{"timestamp", "token"}, Signature: signature}
				hdr := make(http.Header)
				hdr["Timestamp"] = []string{strconv.FormatUint(row.timestamp, 10)}
				hdr["Token"] = []string{row.token}

				So(a.CheckFormal(hdr, valid[0].timestamp, 1<<1), ShouldBeTrue)
				So(a.SatisfiedBy(hdr, []byte(row.key)), ShouldBeTrue)
			}
		})

		Convey("rejects invalid inputs", func() {
			for _, row := range forged {
				signature, _ := base64.StdEncoding.DecodeString(row.sig)
				a := AuthorizationHeader{Algorithm: "hmac-sha256", HeadersToSign: []string{"timestamp", "token"}, Signature: signature}
				hdr := make(http.Header)
				hdr["Timestamp"] = []string{strconv.FormatUint(row.timestamp, 10)}
				hdr["Token"] = []string{row.token}

				So(a.CheckFormal(hdr, row.timestamp, 1<<1), ShouldBeTrue)
				So(a.SatisfiedBy(hdr, []byte(row.key)), ShouldBeFalse)
			}
		})
	})

	Convey("Formal check on an Authorization header A", t, func() {
		Convey("doesn't pass on excessive timestamp differences", func() {
			for _, row := range valid {
				signature, _ := base64.StdEncoding.DecodeString(row.sig)
				a := AuthorizationHeader{Algorithm: "hmac-sha256", HeadersToSign: []string{"timestamp", "token"}, Signature: signature}
				hdr := make(http.Header)
				hdr["Timestamp"] = []string{strconv.FormatUint(row.timestamp, 10)}
				hdr["Token"] = []string{row.token}

				So(a.CheckFormal(hdr, valid[0].timestamp+3, 1<<1), ShouldBeFalse) // +3 here
				So(a.SatisfiedBy(hdr, []byte(row.key)), ShouldBeTrue)
			}
		})
		Convey("doesn't pass if A is over-specified", func() {
			for _, row := range valid {
				signature, _ := base64.StdEncoding.DecodeString(row.sig)
				a := AuthorizationHeader{Algorithm: "hmac-sha256", HeadersToSign: []string{"timestamp", "token"}, Signature: signature}
				hdr := make(http.Header)
				// timestamp is intentionally missing
				hdr["Token"] = []string{row.token}

				So(a.CheckFormal(hdr, valid[0].timestamp, 1<<1), ShouldBeFalse)
				So(a.SatisfiedBy(hdr, []byte(row.key)), ShouldBeFalse)
			}
		})
	})
}
