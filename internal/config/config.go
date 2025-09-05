package config

import (
	"log"
	"os"

	"github.com/joho/godotenv"
)

type Config struct {
	Port        string
	RabbitMQURL string
	JWTSecret   string
	JavaApiURL  string
	FrontendURL string
	RedisURL    string
}

func Load() *Config {
	if os.Getenv("GO_ENV") != "production" {
		err := godotenv.Load()
		if err != nil {
			log.Println("Warning: Could not find .env file, using system environment variables.")
		}
	}

	return &Config{
		Port:        getEnv("PORT", "4000"),
		RabbitMQURL: getEnv("RABBITMQ_URL", "amqp://guest:guest@localhost:5672/"),
		JWTSecret:   getEnv("JWT_SECRET", "default-secret"),
		JavaApiURL:  getEnv("JAVA_API_URL", "http://localhost:8080/api"),
		FrontendURL: getEnv("FRONTEND_URL", "http://localhost:5173"),
		RedisURL:    getEnv("REDIS_URL", "redis://localhost:6379/0"),
	}
}

func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}
