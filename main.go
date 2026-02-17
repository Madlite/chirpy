package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/Madlite/chirpy/internal/auth"
	"github.com/Madlite/chirpy/internal/database"
	"github.com/google/uuid"
	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
)

type Server struct {
	Addr string
}

type apiConfig struct {
	fileserverHits atomic.Int32
	db             *database.Queries
	platform       string
	jwtSecret      string
	polkaKey       string
}

type User struct {
	ID           uuid.UUID `json:"id"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	Email        string    `json:"email"`
	Token        string    `json:"token"`
	RefreshToken string    `json:"refresh_token"`
	IsChirpyRed  bool      `json:"is_chirpy_red"`
}

type Chirp struct {
	ID        uuid.UUID `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Body      string    `json:"body"`
	UserId    uuid.UUID `json:"user_id"`
}

func main() {
	godotenv.Load()
	dbURL := os.Getenv("DB_URL")
	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		log.Fatalf("Error opening database: %s", err)
	}

	dbQueries := database.New(db)
	apiCfg := apiConfig{
		db:        dbQueries,
		platform:  os.Getenv("PLATFORM"),
		jwtSecret: os.Getenv("JWT_SECRET"),
		polkaKey:  os.Getenv("POLKA_KEY"),
	}
	mux := http.NewServeMux()
	mux.Handle("/app/", apiCfg.middlewareMetricsInc(http.StripPrefix("/app", http.FileServer(http.Dir(".")))))
	mux.Handle("/app/assets/logo.png", http.StripPrefix("/app/", http.FileServer(http.Dir("."))))

	mux.HandleFunc("GET /api/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	mux.HandleFunc("GET  /admin/metrics", apiCfg.getHits)
	mux.HandleFunc("POST /admin/reset", apiCfg.handlerResetUsers)
	mux.HandleFunc("POST /api/users", apiCfg.handlerCreateUser)
	mux.HandleFunc("POST /api/login", apiCfg.handlerLoginUser)
	mux.HandleFunc("GET  /api/chirps", apiCfg.handlerGetChirps)
	mux.HandleFunc("GET  /api/chirps/{chirpID}", apiCfg.handlerGetChirp)
	mux.HandleFunc("POST /api/chirps", apiCfg.handlerCreateChirp)
	mux.HandleFunc("POST /api/refresh", apiCfg.handlerRefreshToken)
	mux.HandleFunc("POST /api/revoke", apiCfg.handlerRevokeRefreashToken)
	mux.HandleFunc("PUT /api/users", apiCfg.handlerUpdateUser)
	mux.HandleFunc("DELETE /api/chirps/{chirpID}", apiCfg.handlerDeleteChirp)
	mux.HandleFunc("POST /api/polka/webhooks", apiCfg.handlerUpgradeUserChirpyRed)

	server := &http.Server{
		Addr:    ":8080",
		Handler: mux,
	}
	log.Printf("Serving on port: %s\n", server.Addr)
	log.Fatal(server.ListenAndServe())
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

func (api *apiConfig) handlerCreateChirp(w http.ResponseWriter, r *http.Request) {
	type parameters struct {
		Body   string    `json:"body"`
		UserID uuid.UUID `json:"user_id"`
	}
	decoder := json.NewDecoder(r.Body)
	params := parameters{}
	err := decoder.Decode(&params)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Something went wrong")
		return
	}

	if len(params.Body) > 140 {
		respondWithError(w, http.StatusBadRequest, "Chirp is too long")
		return
	}
	params.Body = replaceBadWords(params.Body)

	token, err := auth.GetBearerToken(r.Header)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Error getting bearer token")
		return
	}
	userID, err := auth.ValidateJWT(token, api.jwtSecret)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Error validating token")
		return
	}

	params.UserID = userID
	dbParams := database.CreateChirpParams{
		Body:   params.Body,
		UserID: params.UserID,
	}
	var dbChirp database.Chirp
	dbChirp, err = api.db.CreateChirp(r.Context(), dbParams)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Something went wrong in db creation")
		return
	}
	responseChirp := Chirp{
		ID:        dbChirp.ID,
		CreatedAt: dbChirp.CreatedAt,
		UpdatedAt: dbChirp.UpdatedAt,
		Body:      dbChirp.Body,
		UserId:    dbChirp.UserID,
	}
	respondWithJSON(w, http.StatusCreated, responseChirp)
}

func (api *apiConfig) handlerCreateUser(w http.ResponseWriter, r *http.Request) {
	type parameters struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	decoder := json.NewDecoder(r.Body)
	params := parameters{}
	err := decoder.Decode(&params)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't decode parameters")
		return
	}

	hash, err := auth.HashPassword(params.Password)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't hash password")
		return
	}

	dbParams := database.CreateUserParams{
		Email:          params.Email,
		HashedPassword: hash,
	}
	user, err := api.db.CreateUser(r.Context(), dbParams)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't create user")
		return
	}
	type response struct {
		User
	}
	respondWithJSON(w, http.StatusCreated, response{
		User: User{
			ID:          user.ID,
			CreatedAt:   user.CreatedAt,
			UpdatedAt:   user.UpdatedAt,
			Email:       user.Email,
			IsChirpyRed: user.IsChirpyRed,
		},
	})
}

func (api *apiConfig) handlerResetUsers(w http.ResponseWriter, r *http.Request) {
	if api.platform != "dev" {
		respondWithError(w, http.StatusForbidden, "Not dev platform")
		return
	}
	api.db.DeleteUsers(r.Context())
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
}

func (api *apiConfig) handlerGetChirps(w http.ResponseWriter, r *http.Request) {
	chirpsAuthor := r.URL.Query().Get("author_id")
	var chirps []database.Chirp
	var err error
	if chirpsAuthor == "" {
		chirps, err = api.db.GetChirps(r.Context())
	} else {
		authorID, err := uuid.Parse(chirpsAuthor)
		if err != nil {
			respondWithError(w, http.StatusInternalServerError, "Couldn't parse authorID")
		}
		chirps, err = api.db.GetChirpsAuthor(r.Context(), authorID)
	}
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't get chirps")
		return
	}
	var responseChirps []Chirp
	for _, chirp := range chirps {
		responseChirps = append(responseChirps, Chirp{
			ID:        chirp.ID,
			CreatedAt: chirp.CreatedAt,
			UpdatedAt: chirp.UpdatedAt,
			Body:      chirp.Body,
			UserId:    chirp.UserID,
		})
	}
	respondWithJSON(w, http.StatusOK, responseChirps)
}

func (api *apiConfig) handlerGetChirp(w http.ResponseWriter, r *http.Request) {
	chirpID, err := uuid.Parse(r.PathValue("chirpID"))
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error with ID")
		return
	}
	chirp, err := api.db.GetChirp(r.Context(), chirpID)
	if err != nil {
		respondWithError(w, http.StatusNotFound, "Couldn't get chirp")
		return
	}

	var responseChirps Chirp
	responseChirps = Chirp{
		ID:        chirp.ID,
		CreatedAt: chirp.CreatedAt,
		UpdatedAt: chirp.UpdatedAt,
		Body:      chirp.Body,
		UserId:    chirp.UserID,
	}
	respondWithJSON(w, http.StatusOK, responseChirps)
}
func (api *apiConfig) handlerLoginUser(w http.ResponseWriter, r *http.Request) {
	type parameters struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}

	decoder := json.NewDecoder(r.Body)
	params := parameters{}
	err := decoder.Decode(&params)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't decode parameters")
		return
	}

	user, err := api.db.GetUser(r.Context(), params.Email)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error getting user from database")
		return
	}
	password_valid, err := auth.CheckPasswordHash(params.Password, user.HashedPassword)
	if !password_valid {
		respondWithError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	refresh_token, err := auth.MakeRefreshToken()
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't create refresh token")
		return
	}
	var dbRefreshToken = database.CreateRefreshTokenParams{
		Token:  refresh_token,
		UserID: user.ID,
	}
	_, err = api.db.CreateRefreshToken(r.Context(), dbRefreshToken)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't create refresh_token db entry")
		return
	}

	token, err := auth.MakeJWT(user.ID, api.jwtSecret)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't create JWT token")
		return
	}

	type response struct {
		User
	}
	respondWithJSON(w, http.StatusOK, response{
		User: User{
			ID:           user.ID,
			CreatedAt:    user.CreatedAt,
			UpdatedAt:    user.UpdatedAt,
			Email:        user.Email,
			Token:        token,
			RefreshToken: refresh_token,
			IsChirpyRed:  user.IsChirpyRed,
		},
	})
}

func (api *apiConfig) handlerRefreshToken(w http.ResponseWriter, r *http.Request) {
	token, err := auth.GetBearerToken(r.Header)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "No autherization bearere token")
		return
	}
	dbRefreshToken, err := api.db.GetRefreshToken(r.Context(), token)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Refresh token not in database")
		return
	}
	if time.Now().After(dbRefreshToken.ExpiresAt) {
		respondWithError(w, http.StatusUnauthorized, "Refresh token expired")
		return
	}
	if dbRefreshToken.RevokedAt.Valid {
		respondWithError(w, http.StatusUnauthorized, "Token is revoked")
		return
	}
	user, err := api.db.GetUserFromRefreshToken(r.Context(), dbRefreshToken.Token)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Could not fetch user from refresh token")
		return
	}
	jwt_token, err := auth.MakeJWT(user.ID, api.jwtSecret)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Couldn't create JWT token")
		return
	}
	type response struct {
		Token string `json:"token"`
	}
	respondWithJSON(w, http.StatusOK, response{
		Token: jwt_token,
	})
}

func (api *apiConfig) handlerRevokeRefreashToken(w http.ResponseWriter, r *http.Request) {
	token, err := auth.GetBearerToken(r.Header)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "No autherization bearere token")
		return
	}
	err = api.db.PostRevokeRefreshToken(r.Context(), token)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unauthorizewd to revoke refresh token")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (api *apiConfig) handlerUpdateUser(w http.ResponseWriter, r *http.Request) {
	token, err := auth.GetBearerToken(r.Header)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Error getting bearer token")
		return
	}
	userID, err := auth.ValidateJWT(token, api.jwtSecret)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Error validating token")
		return
	}
	type parameters struct {
		Password string `json:"password"`
		Email    string `json:"email"`
	}
	decoder := json.NewDecoder(r.Body)
	params := parameters{}
	err = decoder.Decode(&params)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Something went wrong")
		return
	}
	hashed_password, err := auth.HashPassword(params.Password)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error hashing password")
	}
	err = api.db.UpdateUserEmail(r.Context(), database.UpdateUserEmailParams{
		ID:    userID,
		Email: params.Email,
	})
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error updating user email")
	}
	err = api.db.UpdateUserPassword(r.Context(), database.UpdateUserPasswordParams{
		ID:             userID,
		HashedPassword: hashed_password,
	})
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error updating user password")
	}

	user, err := api.db.GetUser(r.Context(), params.Email)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error getting user from database")
		return
	}
	type response struct {
		User
	}
	respondWithJSON(w, http.StatusOK, response{
		User: User{
			ID:          user.ID,
			CreatedAt:   user.CreatedAt,
			UpdatedAt:   user.UpdatedAt,
			Email:       user.Email,
			IsChirpyRed: user.IsChirpyRed,
		},
	})
}

func (api *apiConfig) handlerDeleteChirp(w http.ResponseWriter, r *http.Request) {
	token, err := auth.GetBearerToken(r.Header)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Error getting bearer token")
		return
	}
	userID, err := auth.ValidateJWT(token, api.jwtSecret)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Error validating token")
		return
	}
	chirpID, err := uuid.Parse(r.PathValue("chirpID"))
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error with chirp ID")
		return
	}
	chirp, err := api.db.GetChirp(r.Context(), chirpID)
	if err != nil {
		respondWithError(w, http.StatusNotFound, "Error getting chirp from db")
		return
	}
	if chirp.UserID != userID {
		respondWithError(w, http.StatusForbidden, "Not owner of chirp")
		return
	}
	err = api.db.DeleteChirp(r.Context(), database.DeleteChirpParams{
		UserID: userID,
		ID:     chirpID,
	})
	if err != nil {
		respondWithError(w, http.StatusNotFound, "Error deleting chirp, not in database")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (api *apiConfig) handlerUpgradeUserChirpyRed(w http.ResponseWriter, r *http.Request) {
	type parameters struct {
		Event string `json:"event"`
		Data  struct {
			UserID string `json:"user_id"`
		} `json:"data"`
	}
	decoder := json.NewDecoder(r.Body)
	params := parameters{}
	err := decoder.Decode(&params)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Something went wrong")
		return
	}

	if params.Event != "user.upgraded" {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	polkaKey, err := auth.GetAPIKey(r.Header)
	if polkaKey != api.polkaKey || err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	userID, err := uuid.Parse(params.Data.UserID)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Problem parsing user ID")
		return
	}
	err = api.db.UpdateUserChirpyRed(r.Context(), userID)
	if err != nil {
		respondWithError(w, http.StatusNotFound, "Error with updating user chirpy red in database")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func respondWithError(w http.ResponseWriter, code int, msg string) {
	respondWithJSON(w, code, map[string]string{"error": msg})
}

func respondWithJSON(w http.ResponseWriter, code int, payload any) {
	data, err := json.Marshal(payload)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	w.Write(data)
}

func replaceBadWords(msg string) string {
	bad_words := map[string]struct{}{
		"kerfuffle": {},
		"sharbert":  {},
		"fornax":    {},
	}
	words := strings.Split(msg, " ")
	for i, word := range words {
		if _, ok := bad_words[strings.ToLower(word)]; ok {
			words[i] = "****"
		}
	}
	clean_msg := strings.Join(words, " ")
	return clean_msg
}
