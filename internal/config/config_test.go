package config

import "testing"

func TestLoad(t *testing.T) {
	// Тест не должен зависеть от .env, который экспортирует Makefile.
	for _, k := range []string{"SEED", "ADDR", "COOKIE_SECURE", "STORAGE"} {
		t.Setenv(k, "")
	}
	t.Setenv("PRICE_PER_USER", "1500")
	c := Load()
	if c.Seed || c.PricePerUser != 1500 || c.Addr != ":8080" || c.Storage != "local" {
		t.Fatalf("по умолчанию без демо-данных, получили %+v", c)
	}
	t.Setenv("SEED", "1")
	if c := Load(); !c.Seed {
		t.Fatalf("явное включение: %+v", c)
	}
}

func TestValidate(t *testing.T) {
	if err := (Config{Storage: "s3", S3: S3{Endpoint: "x"}}).Validate(); err == nil {
		t.Fatal("S3 без бакета и ключей")
	}
	if err := (Config{Storage: "ftp"}).Validate(); err == nil {
		t.Fatal("неизвестное хранилище")
	}
	ok := Config{Storage: "s3", S3: S3{Endpoint: "object.pscloud.io", Bucket: "b", AccessKey: "k", SecretKey: "s"}, SIGEX: SIGEX{Fake: true}}
	if err := ok.Validate(); err != nil {
		t.Fatal(err)
	}
	if err := (Config{Storage: "local", SIGEX: SIGEX{URL: "https://sigex.kz"}}).Validate(); err != nil {
		t.Fatal(err)
	}
	if err := (Config{Storage: "local", SIGEX: SIGEX{URL: "https://sigex.kz", Cert: "cert.pem"}}).Validate(); err == nil {
		t.Fatal("сертификат без ключа")
	}
}
