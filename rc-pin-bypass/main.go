package main

import (
	"context"
	"crypto/rand"
	"io"
	"log"
	"math/big"
	"net/http"
	"net/url"
	"strconv"

	"github.com/go-redis/redis"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
)

func panicIfErr(err error, reason string) {
	if err != nil {
		log.Fatalf("%s: %v", reason, err)
	}
}

func main() {
	conn, err := pgxpool.New(context.Background(), "postgres://postgres:postgres@localhost:5432/postgres")
	panicIfErr(err, "Failed to connect to PostgreSQL")
	pin, err := rand.Int(rand.Reader, big.NewInt(999))
	panicIfErr(err, "Failed to generate the PIN")
	var id int
	err = conn.QueryRow(context.Background(), "INSERT INTO pins (pin) VALUES ($1) RETURNING id", pin).Scan(&id)
	panicIfErr(err, "Failed to insert the PIN")

	log.Printf("ID: %d, PIN: %d", id, pin)
	rdb := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
	})

	err = rdb.Set("rate_limit", 5, -1).Err()
	panicIfErr(err, "Failed to set the rate limit")
	h2server := &http2.Server{
		MaxConcurrentStreams: 1000,
	}

	router := http.NewServeMux()

	router.Handle("POST /", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bodyBytes, err := io.ReadAll(r.Body)
		if err != nil {
			log.Printf("Failed to read the request body: %v", err)
			return
		}
		val, err := rdb.Get("rate_limit").Int()
		if err != nil {
			log.Printf("Failed to retrieve the rate limit from Redis: %v", err)
			return
		}
		if val <= 0 {
			log.Println("Rate limit exceeded. Restart application to reset the rate limit!")
			return
		}
		form, err := url.ParseQuery(string(bodyBytes))
		if err != nil {
			log.Printf("Failed to parse request body as form: %v", err)
			return
		}
		idStr := form.Get("id")
		pinStr := form.Get("pin")

		id, err := strconv.Atoi(idStr)
		if err != nil {
			log.Printf("Failed to parse ID: %v", err)
			return
		}
		pin, err := strconv.Atoi(pinStr)
		if err != nil {
			log.Printf("Failed to parse pin: %v", err)
			return
		}

		var correctPin int
		err = conn.QueryRow(context.Background(), "SELECT pin FROM pins WHERE id = $1", id).Scan(&correctPin)
		if err != nil {
			log.Printf("Failed to retrieve pin: %v", err)
			return
		}
		if correctPin == pin {
			log.Println("PIN is correct!")
		} else {
			log.Printf("Incorrect PIN: %d (remaining attempts: %d)", pin, val-1)
		}

		err = rdb.Set("rate_limit", val-1, -1).Err()
		if err != nil {
			log.Printf("Failed to set the rate limit: %v", err)
			return
		}
	}))

	server := &http.Server{
		Addr:    "0.0.0.0:8080",
		Handler: h2c.NewHandler(router, h2server),
	}

	err = http2.ConfigureServer(server, h2server)
	panicIfErr(err, "Failed to configure the server for HTTP/2")

	log.Printf("Listening on [0.0.0.0:8080]...\n")
	err = server.ListenAndServe()
	panicIfErr(err, "Failed to start the server")
}
