package upload // import "blitznote.com/src/caddy.upload"

import (
	"encoding/base64"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestCheckHMAC(t *testing.T) {
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

	Convey("Given valid signatures and inputs", t, func() {
		Convey("accept", func() {
			for _, row := range valid {
				signature, _ := base64.StdEncoding.DecodeString(row.sig)
				So(checkHMAC([]byte(row.key), []byte(row.token), signature, row.timestamp), ShouldBeTrue)
			}
		})
	})

	Convey("Forged signatures", t, func() {
		Convey("reject", func() {
			for _, row := range forged {
				signature, _ := base64.StdEncoding.DecodeString(row.sig)
				So(checkHMAC([]byte(row.key), []byte(row.token), signature, row.timestamp), ShouldBeFalse)
			}
		})
	})
}
