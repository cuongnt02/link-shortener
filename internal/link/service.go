package link

import (
	"context"
	"errors"
	"fmt"
	"net/url"

	"github.com/cuongnt02/link-shortener/internal/store"
)

var (
	// ErrInvalidURL shows that the URL is not in a valid format
	ErrInvalidURL = errors.New("invalid URL")
	// ErrNotFound shows that the code is not found in the storage
	ErrNotFound = errors.New("not found")
	// ErrMaxAttemptsReached shows the maximum attempts have been made for generating code and failed
	ErrMaxAttemptsReached = errors.New("max attempts reached generating code")
)

// MaxAttempts defines number of attempts should be made generating code before throwing error
const MaxAttempts = 5

type Generator func() (string, error)

// Store defines a service layer storing functionality for the internal store
// Save must return store.ErrCodeExists when the code is already taken
// Get must return store.ErrNotFound when the code is missing
type Store interface {
	Save(ctx context.Context, code, url string) error
	Get(ctx context.Context, code string) (string, error)
}

// Service defines a layer for logic rules handling before http layer
type Service struct {
	store     Store
	generator Generator
}

// NewService creates a reference to a Service
func NewService(st Store, generator Generator) *Service {
	return &Service{
		store:     st,
		generator: generator,
	}
}

// Shorten validates the url passed, generate a random code and store into the service store
func (svc *Service) Shorten(ctx context.Context, rawURL string) (string, error) {
	if err := validateURL(rawURL); err != nil {
		return "", fmt.Errorf("validating url: %w", err)
	}

	for range MaxAttempts {
		code, err := svc.generator()
		if err != nil {
			return "", fmt.Errorf("generating code: %w", err)
		}

		err = svc.store.Save(ctx, code, rawURL)
		if err == nil {
			return code, nil
		}
		if errors.Is(err, store.ErrCodeExists) {
			continue
		}
		return "", fmt.Errorf("saving link: %w", err)
	}
	return "", fmt.Errorf("generating code: %w", ErrMaxAttemptsReached)
}

// Resolve get the passed code from the store, which used for redirecting request
func (svc *Service) Resolve(ctx context.Context, code string) (string, error) {
	currentURL, err := svc.store.Get(ctx, code)
	switch {
	case errors.Is(err, store.ErrNotFound):
		return "", fmt.Errorf("%w: resolving code %s", ErrNotFound, code)
	case err != nil:
		return "", fmt.Errorf("%w: resolving code %s", err, code)
	default:
		return currentURL, nil
	}
}

func validateURL(rawURL string) error {
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("%w: parsing url %w", ErrInvalidURL, err)
	}

	scheme := parsedURL.Scheme
	if scheme != "https" && scheme != "http" {
		return fmt.Errorf("%w: unsupported scheme %q", ErrInvalidURL, scheme)
	}

	host := parsedURL.Host
	if host == "" {
		return fmt.Errorf("%w: host is empty", ErrInvalidURL)
	}

	return nil
}
