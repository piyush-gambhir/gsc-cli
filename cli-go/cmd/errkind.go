package cmd

import (
	"context"
	"errors"
	"net/url"
	"strings"

	"github.com/piyush-gambhir/gsc-cli/cli-go/internal/auth"
	"github.com/piyush-gambhir/gsc-cli/cli-go/internal/client"
)

const (
	kindUsage        = "usage"
	kindAuth         = "auth"
	kindReadOnly     = "read_only"
	kindConfirmation = "confirmation_required"
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
		case api.Quota():
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
	case strings.HasPrefix(err.Error(), "unknown command "):
		return kindUsage
	}
	return "error"
}
