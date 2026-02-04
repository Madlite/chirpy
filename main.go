package main


import(
	"net/http"
)

type Server struct {
    Addr 	string
}

func main() {
	mux := http.NewServeMux()
	server := &http.Server{
		Addr:    ":8080",
		Handler: mux,
	}
	server.ListenAndServe()
}