package cmd

import (
	"context"
	"errors"
	"net/url"
	"strings"

	"github.com/piyush-gambhir/gsc-cli/cli-go/internal/auth"
	"github.com/piyush-gambhir/gsc-cli/cli-go/internal/client"
)

// Every failure exits 1, like the rest of the CLI suite. Structured errors add a
// "kind" so agents can branch without parsing the message.
const (
	kindUsage        = "usage"                 // unknown command or flag, bad arguments
	kindAuth         = "auth"                  // not logged in, or the credentials no longer work
	kindReadOnly     = "read_only"             // --read-only mode or a read-only scope blocked a write
	kindConfirmation = "confirmation_required" // destructive command without --yes and no terminal
)

type kindError struct {
	kind string
	err  error
}

func (e *kindError) Error() string { return e.err.Error() }
func (e *kindError) Unwrap() error { return e.err }

func withKind(kind string, err error) error {
	if err == nil {
		return nil
	}
	return &kindError{kind, err}
}

// errorKind classifies a command error for the structured "kind" field.
func errorKind(err error) string {
	var k *kindError
	var cred *auth.CredentialError
	var api *client.APIError
	var ue *url.Error
	switch {
	case errors.As(err, &k):
		return k.kind
	case errors.As(err, &cred), errors.Is(err, auth.ErrNoClient):
		return kindAuth
	case errors.As(err, &api):
		switch {
		case api.Quota(): // includes Google's legacy 403 quota responses
			return "rate_limit"
		case api.Status == 401:
			return kindAuth
		case api.Status == 403:
			return "permission"
		case api.Status == 404:
			return "not_found"
		case api.Status == 429:
			return "rate_limit"
		case api.Status >= 500:
			return "server"
		}
		return "invalid_request"
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.Is(err, context.Canceled):
		return "cancelled"
	case errors.As(err, &ue):
		if ue.Timeout() {
			return "timeout"
		}
		return "network"
	case strings.HasPrefix(err.Error(), "unknown command "): // Cobra's root lookup error is untyped
		return kindUsage
	}
	return "error"
}
