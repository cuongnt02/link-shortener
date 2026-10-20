package handler

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/url"

	"github.com/cuongnt02/link-shortener/internal/shortener"
	"github.com/cuongnt02/link-shortener/internal/store"
)

const (
	codeLength  = 6
	maxAttempts = 5
)

// Handler create a handler struct for request handling for the application
type Handler struct {
	store   *store.MemoryStore
	baseURL string
}

type shortenRequest struct {
	URL string `json:"url"`
}

type shortenResponse struct {
	Code     string `json:"code"`
	ShortURL string `json:"short_url"`
}

// New creates a new handler for the application and returns the pointer to it
func New(s *store.MemoryStore, baseURL string) *Handler {
	return &Handler{
		store:   s,
		baseURL: baseURL,
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

func validateURL(rawURL string) error {
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return err
	}

	scheme := parsedURL.Scheme
	if scheme != "https" && scheme != "http" {
		return errors.New("scheme is not supported or empty")
	}

	host := parsedURL.Host
	if host == "" {
		return errors.New("host is empty")
	}

	return nil
}

// Shorten accepts a url and returns a shorten version of it
func (h *Handler) Shorten(w http.ResponseWriter, r *http.Request) {
	var payload shortenRequest
	err := json.NewDecoder(r.Body).Decode(&payload)
	if err != nil {
		http.Error(w, "malformed or invalid json request body", http.StatusBadRequest)
		return
	}

	if err = validateURL(payload.URL); err != nil {
		http.Error(w, "invalid url in request body: "+err.Error(), http.StatusBadRequest)
		return
	}
	var code string
	for range maxAttempts {
		code, err = shortener.GenCode(codeLength)
		if err != nil {
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			log.Printf("generate code failed for url %q, %v", payload.URL, err)
			return
		}

		err = h.store.Save(code, payload.URL)
		if err != nil {
			if errors.Is(err, store.ErrCodeExists) {
				continue
			}
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			log.Printf("saving code for url %q got an unexpected error, %v", payload.URL, err)
			return
		}

		break
	}

	if err != nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		log.Printf("max attempts of retries reached for generating code for url %q", payload.URL)
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
	link, err := h.store.Get(code)
	switch {
	case errors.Is(err, store.ErrNotFound):
		http.Error(w, "short link not found", http.StatusNotFound)
		return
	case err != nil:
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		log.Printf("unexpected error raised: error is %v", err)
		return
	}
	http.Redirect(w, r, link, http.StatusFound)
}

func (h *Handler) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", h.Health)
	mux.HandleFunc("GET /{code}", h.Redirect)
	mux.HandleFunc("POST /shorten", h.Shorten)
	return mux
}
