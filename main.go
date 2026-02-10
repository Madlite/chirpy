package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync/atomic"
)

type Server struct {
	Addr string
}

type apiConfig struct {
	fileserverHits atomic.Int32
}

func main() {
	mux := http.NewServeMux()
	server := &http.Server{
		Addr:    ":8080",
		Handler: mux,
	}
	apiCfg := apiConfig{}

	mux.Handle("/app/", apiCfg.middlewareMetricsInc(http.StripPrefix("/app", http.FileServer(http.Dir(".")))))
	mux.Handle("/app/assets/logo.png", http.StripPrefix("/app/", http.FileServer(http.Dir("."))))

	mux.HandleFunc("GET /api/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	mux.HandleFunc("GET  /admin/metrics", apiCfg.getHits)
	mux.HandleFunc("POST /admin/reset", apiCfg.resetHits)
	mux.HandleFunc("POST /api/validate_chirp", handlerValidateChirp)

	server.ListenAndServe()
}

func (cfg *apiConfig) middlewareMetricsInc(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cfg.fileserverHits.Add(1)
		next.ServeHTTP(w, r)
	})
}

func (api *apiConfig) getHits(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	w.WriteHeader(http.StatusOK)
	msg := fmt.Sprintf("<html><body><h1>Welcome, Chirpy Admin</h1><p>Chirpy has been visited %d times!</p></body></html>", api.fileserverHits.Load())
	w.Write([]byte(msg))
}

func (api *apiConfig) resetHits(w http.ResponseWriter, r *http.Request) {
	api.fileserverHits.Store(0)
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}

func handlerValidateChirp(w http.ResponseWriter, r *http.Request) {
	type Chirp struct {
		Text string `json:"body"`
	}

	decoder := json.NewDecoder(r.Body)
	chirp := Chirp{}
	err := decoder.Decode(&chirp)
	if err != nil {
		respondWithError(w, 500, "Something went wrong")
		return
	}

	if len(chirp.Text) > 140 {
		respondWithError(w, 400, "Chirp is too long")
		return
	}

	if clean_msg, ok := replaceBadWords(chirp.Text); !ok {
		respondWithJSON(w, 200, map[string]string{"cleaned_body": clean_msg})
	}

	respondWithJSON(w, 200, map[string]bool{
		"valid": true,
	})
}

func respondWithError(w http.ResponseWriter, code int, msg string) {
	respondWithJSON(w, code, map[string]string{"error": msg})
}

func respondWithJSON(w http.ResponseWriter, code int, payload any) {
	data, err := json.Marshal(payload)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	w.Write(data)
}

func replaceBadWords(msg string) (string, bool) {
	bad_words := []string{"kerfuffle", "sharbert", "fornax"}
	// check the string and replace - strings.ToLower strings.Split strings.Join
	clean_msg := msg
	return clean_msg
}
