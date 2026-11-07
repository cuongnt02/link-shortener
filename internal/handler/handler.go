package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"github.com/cuongnt02/link-shortener/internal/link"
)

// Handler create a handler struct for request handling for the application
type Handler struct {
	linkService LinkService
	baseURL     string
	logger      *slog.Logger
}

type shortenRequest struct {
	URL string `json:"url"`
}

type shortenResponse struct {
	Code     string `json:"code"`
	ShortURL string `json:"short_url"`
}

// LinkService define the dependent service methods that is used for handling link logic
type LinkService interface {
	Resolve(ctx context.Context, code string) (string, error)
	Shorten(ctx context.Context, rawURL string) (string, error)
}

// New creates a new handler for the application and returns the pointer to it
func New(svc LinkService, baseURL string, logger *slog.Logger) *Handler {
	return &Handler{
		linkService: svc,
		baseURL:     baseURL,
		logger:      logger,
	}
}

// Health checks if the application is healthy and ready to accept requests
func (h *Handler) Health(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, err := w.Write([]byte("ok"))
	if err != nil {
		h.logger.WarnContext(r.Context(), "error at handler", slog.Any("error", err))
	}
}

// Shorten accepts a url and returns a shorten version of it
func (h *Handler) Shorten(w http.ResponseWriter, r *http.Request) {
	var payload shortenRequest
	err := json.NewDecoder(r.Body).Decode(&payload)
	if err != nil {
		http.Error(w, makeRequestErrorMessage(err), http.StatusBadRequest)
		return
	}

	code, err := h.linkService.Shorten(r.Context(), payload.URL)
	switch {
	case errors.Is(err, link.ErrInvalidURL):
		http.Error(w, fmt.Sprintf("invalid url used for shortening: %v", err), http.StatusBadRequest)
		return
	case err != nil:
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		h.logger.ErrorContext(r.Context(), "unexpected error occurred when shortening", slog.Any("error", err))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	response := shortenResponse{
		Code:     code,
		ShortURL: h.baseURL + "/" + code,
	}
	w.WriteHeader(http.StatusCreated)
	err = json.NewEncoder(w).Encode(response)
	if err != nil {
		h.logger.WarnContext(r.Context(), "error encoding response", slog.Any("error", err))
	}
}

// Redirect routes the user to the expanded version of the url
func (h *Handler) Redirect(w http.ResponseWriter, r *http.Request) {
	code := r.PathValue("code")
	linkResolved, err := h.linkService.Resolve(r.Context(), code)
	switch {
	case errors.Is(err, link.ErrNotFound):
		http.Error(w, "short link not found", http.StatusNotFound)
		return
	case err != nil:
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		h.logger.ErrorContext(r.Context(), "failed to resolve link", slog.Any("error", err), slog.String("code", code))
		return
	}
	http.Redirect(w, r, linkResolved, http.StatusFound)
}

func makeRequestErrorMessage(err error) string {
	var typeErr *json.UnmarshalTypeError
	var syntaxErr *json.SyntaxError
	switch {
	case errors.Is(err, io.EOF):
		return "request body is empty"
	case errors.Is(err, io.ErrUnexpectedEOF):
		return "request body is incomplete JSON"
	case errors.As(err, &typeErr):
		return fmt.Sprintf("field %q must be a %s, got %s", typeErr.Field, typeErr.Type, typeErr.Value)
	case errors.As(err, &syntaxErr):
		return fmt.Sprintf("request body at byte %d is not valid JSON", syntaxErr.Offset)
	default:
		return "invalid request body"
	}
}

// Routes define the application http routes
func (h *Handler) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", h.Health)
	mux.HandleFunc("GET /{code}", h.Redirect)
	mux.HandleFunc("POST /shorten", h.Shorten)
	return mux
}
