// Package config читает настройки из переменных окружения.
package config

import (
	"errors"
	"os"
	"strconv"
)

type Config struct {
	DatabaseURL  string
	Addr         string
	BaseURL      string // для ссылок в SMS
	UploadsDir   string
	CookieSecure bool   // включать за HTTPS
	FixedOTP     string // временный общий код до подключения SMS; "" — случайный код. Только явным FIXED_OTP.
	PricePerUser int    // ₸ за сотрудника в месяц
	Seed         bool   // создать демо-данные в пустой базе
}

func Load() Config {
	c := Config{
		DatabaseURL:  get("DATABASE_URL", "postgres://qauipsiz:qauipsiz@localhost:5433/qauipsiz?sslmode=disable"),
		Addr:         get("ADDR", ":8080"),
		BaseURL:      get("BASE_URL", "http://localhost:8080"),
		UploadsDir:   get("UPLOADS_DIR", "uploads"),
		CookieSecure: get("COOKIE_SECURE", "") == "1",
		FixedOTP:     get("FIXED_OTP", ""), // ponytail: убрать вместе с подключением Mobizon/SMSC.kz
		PricePerUser: 990,
		Seed:         get("SEED", "") == "1",
	}
	if c.FixedOTP == "off" {
		c.FixedOTP = ""
	}
	if n, err := strconv.Atoi(get("PRICE_PER_USER", "")); err == nil {
		c.PricePerUser = n
	}
	return c
}

// Validate запрещает опасные сочетания: тестовый код входа за HTTPS — признак прода.
func (c Config) Validate() error {
	if c.FixedOTP != "" && c.CookieSecure {
		return errors.New("FIXED_OTP нельзя включать вместе с COOKIE_SECURE=1: любой войдёт под любым сотрудником")
	}
	return nil
}

func get(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
