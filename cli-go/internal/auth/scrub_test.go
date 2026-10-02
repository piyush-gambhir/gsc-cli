package auth

import (
	"errors"
	"strings"
	"testing"
)

func TestTokenErrorsNeverEchoCredentials(t *testing.T) {
	// Fixtures are assembled at run time so the source never contains a
	// token-shaped literal that secret scanners would flag.
	jwt := "eyJ" + "hbGciOiJSUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.c2lnbmF0dXJlLXNpZ25hdHVyZS1zaWduYXR1cmU"
	access := "ya" + "29.a0AfH6SMBxyzSECRET-token_value.more"
	refresh := "1/" + "/0gSecretRefreshTokenValue-abc"
	for _, raw := range []string{
		`executable response {"id_token":"` + jwt + `","expiration_time":"soon"} is malformed`,
		"bad access token " + access,
		"refresh " + refresh,
	} {
		msg := friendlyTokenError(errors.New(raw)).Error()
		for _, leak := range []string{jwt, access[:20], refresh[:15]} {
			if strings.Contains(msg, leak) {
				t.Fatalf("leaked %q in %q", leak, msg)
			}
		}
	}
}
