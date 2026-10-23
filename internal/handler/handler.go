package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"

	"github.com/cuongnt02/link-shortener/internal/link"
)

// Handler create a handler struct for request handling for the application
type Handler struct {
	linkService LinkService
	baseURL     string
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
func New(svc LinkService, baseURL string) *Handler {
	return &Handler{
		linkService: svc,
		baseURL:     baseURL,
	}
}

// Health checks if the application is healthy and ready to accept requests
func (h *Handler) Health(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, err := w.Write([]byte("ok"))
	if err != nil {
		log.Printf("error at handler: %v", err)
	}
}

// Shorten accepts a url and returns a shorten version of it
func (h *Handler) Shorten(w http.ResponseWriter, r *http.Request) {
	var payload shortenRequest
	err := json.NewDecoder(r.Body).Decode(&payload)
	if err != nil {
		http.Error(w, fmt.Sprintf("malformed or invalid json request body: %s", r.Body), http.StatusBadRequest)
		return
	}

	code, err := h.linkService.Shorten(r.Context(), payload.URL)
	switch {
	case errors.Is(err, link.ErrInvalidURL):
		http.Error(w, fmt.Sprintf("invalid url used for shortening: %s", payload.URL), http.StatusBadRequest)
		return
	case err != nil:
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		log.Printf("unexpected error occurred when shortening: %v", err)
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
		log.Printf("error encoding response, encode failed for response %v, error: %v", response, err)
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
		log.Printf("unexpected error raised: error is %v", err)
		return
	}
	http.Redirect(w, r, linkResolved, http.StatusFound)
}

// Routes define the application http routes
func (h *Handler) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", h.Health)
	mux.HandleFunc("GET /{code}", h.Redirect)
	mux.HandleFunc("POST /shorten", h.Shorten)
	return mux
}
