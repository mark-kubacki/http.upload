package auth // import "hub.blitznote.com/src/caddy.upload/signature.auth"

import (
	"net/http"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestAuthorization(t *testing.T) {
	Convey("func Authorization", t, func() {
		h := make(http.Header)
		users := make(HmacSecrets)
		var now uint64 = 1458508452

		// no users, but auth is active
		err := Authenticate(h, users, now, now)
		So(err.SuggestedResponseCode(), ShouldEqual, http.StatusForbidden)
		So(err, ShouldNotBeNil)

		// now with users
		users.Insert([]string{"yui=Z2VoZWlt"}) // yui=geheim
		users.Insert([]string{"yui"})
		users.Insert([]string{"yui=3==="})

		// missing header
		err = Authenticate(h, users, now, now)
		So(err.SuggestedResponseCode(), ShouldEqual, http.StatusUnauthorized)
		So(err, ShouldNotBeNil)

		// feed a malformed one
		h.Add("Authorization", "Signature")
		err = Authenticate(h, users, now, now)
		So(err.SuggestedResponseCode(), ShouldEqual, http.StatusBadRequest)
		So(err, ShouldNotBeNil)

		// a valid request
		h.Set("Authorization", `Signature keyId="yui",algorithm="hmac-sha256",headers="timestamp token",signature="yql3kIDweM8KYm+9pHzX0PKNskYAU46Jb5D6nLftTvo="`)
		h.Set("Timestamp", "1458508452")
		h.Set("Token", "streng")
		err = Authenticate(h, users, now, 0)
		So(err, ShouldBeNil)

		// replay, must fail
		err = Authenticate(h, users, now+5, 1<<2)
		So(err.SuggestedResponseCode(), ShouldEqual, http.StatusForbidden)
		So(err, ShouldNotBeNil)

		// signature mismatch
		h.Set("Token", "streng++")
		err = Authenticate(h, users, now, 0)
		So(err.SuggestedResponseCode(), ShouldEqual, http.StatusForbidden)
		So(err, ShouldNotBeNil)
		h.Set("Token", "streng")

		// wrong order
		h.Set("Authorization", `Signature keyId="yui",headers="token timestamp",signature="yql3kIDweM8KYm+9pHzX0PKNskYAU46Jb5D6nLftTvo="`)
		err = Authenticate(h, users, now, 0)
		So(err.SuggestedResponseCode(), ShouldEqual, http.StatusUnauthorized)
		So(err, ShouldNotBeNil)

		// algorithm mismatch
		h.Set("Authorization", `Signature keyId="yui",algorithm="hmac-sha512",signature="yql3kIDweM8KYm+9pHzX0PKNskYAU46Jb5D6nLftTvo="`)
		err = Authenticate(h, users, now, 0)
		So(err.SuggestedResponseCode(), ShouldEqual, http.StatusUnauthorized)
		So(err, ShouldNotBeNil)
	})
}
