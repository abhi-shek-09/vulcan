package aggregator

type Config struct {
	NATSURL string
}

func DefaultConfig() *Config {
	return &Config{
		NATSURL: "nats://localhost:4222",
	}
}