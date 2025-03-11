package config

import (
	"github.com/joho/godotenv"
	"log"
	"log/slog"
	"os"
)

type Config struct {
	AuthSecret string
}

func LoadConfig(s *slog.Logger) Config {
	err := godotenv.Load()
	if err != nil {
		log.Fatal("Error loading .env file")
	}
	secret := os.Getenv("auth")
	if secret == "" {
		s.Info("problem to load secret")
		//TODO: if time handle it better
		secret = "default"
	}
	return Config{AuthSecret: secret}
}
