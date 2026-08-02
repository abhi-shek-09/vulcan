package aggregator

import (
	"os"

	"github.com/joho/godotenv"
)

type Config struct {
	NATSURL string
	VictoriaMetricsURL string
}

func DefaultConfig() *Config {
	_ = godotenv.Load("../../.env")

	return &Config{
		NATSURL: os.Getenv("NATS_URL"),
		VictoriaMetricsURL: os.Getenv("VICTORIA_METRICS_URL") ,
	}
}
