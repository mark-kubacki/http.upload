package upload // import "blitznote.com/src/caddy.upload"

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

func checkHMAC(HmacSharedSecret, Token, Signature []byte, Timestamp uint64) bool {
	mac := hmac.New(sha256.New, []byte(HmacSharedSecret))
	mac.Write([]byte(strconv.FormatUint(Timestamp, 10)))
	mac.Write([]byte(Token))
	expectedMAC := mac.Sum(nil)
	return hmac.Equal(Signature, expectedMAC)
}

// Results in a syscall issued by 'runtime'.
func getTimestampUsingTime() uint64 {
	t := time.Now()
	return uint64(t.Unix())
}

// Seconds since 1970-01-01 00:00:00Z.
//
// Will be overwritten by another in-house package.
var getTimestamp func() uint64 = getTimestampUsingTime

func abs64(n uint64) uint64 {
	sign := n >> (64 - 1)
	return (n ^ sign) - sign
}

func (h UploadHandler) authenticate(r *http.Request) (httpResponseCode int, err error) {
	httpResponseCode = 200 // 200: ok/pass

	if len(h.Config.IncomingSharedHmacSecret) > 0 {
		p, v, e := r.Header.Get("Timestamp"), r.Header.Get("Token"), r.Header.Get("Signature")
		timestamp, err := strconv.ParseUint(p, 10, 64)
		sig, err2 := base64.StdEncoding.DecodeString(e)
		// Timing is unimportant here: the attacker can only learn our parsing abilities.
		if p == "" || v == "" || e == "" || err != nil || err2 != nil {
			return 400, nil // 400: catchall for bad requests
		}

		now := getTimestamp()
		timestampOk := abs64(now-timestamp) <= h.Config.TimestampTolerance

		// In this scheme (timestamp || v) constitutes the token.
		if !timestampOk || !checkHMAC(h.Config.IncomingSharedHmacSecret, []byte(v), sig, timestamp) {
			return 403, fmt.Errorf("Method Not Authorized") // 403: forbidden
		}
	}

	return
}
