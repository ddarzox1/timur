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
	CookieSecure bool // включать за HTTPS
	PricePerUser int  // ₸ за сотрудника в месяц
	Seed         bool // создать демо-данные в пустой базе

	Storage string // local — диск (UPLOADS_DIR), s3 — S3-совместимое хранилище
	S3      S3
	SIGEX   SIGEX
}

type S3 struct {
	Endpoint, Region, Bucket, AccessKey, SecretKey string
	UseSSL                                         bool
}

type SIGEX struct {
	URL, Cert, Key string
	Fake           bool
}

func Load() Config {
	c := Config{
		DatabaseURL:  get("DATABASE_URL", "postgres://qauipsiz:qauipsiz@localhost:5433/qauipsiz?sslmode=disable"),
		Addr:         get("ADDR", ":8080"),
		BaseURL:      get("BASE_URL", "http://localhost:8080"),
		UploadsDir:   get("UPLOADS_DIR", "uploads"),
		CookieSecure: get("COOKIE_SECURE", "") == "1",
		PricePerUser: 9990,
		Seed:         get("SEED", "") == "1",
		Storage:      get("STORAGE", "local"),
		S3: S3{
			Endpoint:  get("S3_ENDPOINT", ""),
			Region:    get("S3_REGION", ""),
			Bucket:    get("S3_BUCKET", ""),
			AccessKey: get("S3_ACCESS_KEY", ""),
			SecretKey: get("S3_SECRET_KEY", ""),
			UseSSL:    get("S3_USE_SSL", "1") == "1",
		},
		SIGEX: SIGEX{
			URL:  get("SIGEX_URL", "https://sigex.kz"),
			Cert: get("SIGEX_CERT", ""),
			Key:  get("SIGEX_KEY", ""),
			Fake: get("SIGEX_FAKE", "1") == "1",
		},
	}
	if n, err := strconv.Atoi(get("PRICE_PER_USER", "")); err == nil {
		c.PricePerUser = n
	}
	return c
}

func (c Config) Validate() error {
	switch c.Storage {
	case "local":
	case "s3":
		if c.S3.Endpoint == "" || c.S3.Bucket == "" || c.S3.AccessKey == "" || c.S3.SecretKey == "" {
			return errors.New("STORAGE=s3: нужны S3_ENDPOINT, S3_BUCKET, S3_ACCESS_KEY, S3_SECRET_KEY")
		}
	default:
		return errors.New("STORAGE: допустимо local или s3")
	}
	if !c.SIGEX.Fake {
		if c.SIGEX.URL == "" {
			return errors.New("SIGEX_FAKE=0: нужен SIGEX_URL")
		}
		if (c.SIGEX.Cert == "") != (c.SIGEX.Key == "") {
			return errors.New("SIGEX_CERT и SIGEX_KEY должны быть заданы вместе")
		}
	}
	return nil
}

func get(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
