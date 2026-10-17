package config

import "testing"

func TestLoad(t *testing.T) {
	// Тест не должен зависеть от .env, который экспортирует Makefile.
	for _, k := range []string{"FIXED_OTP", "SEED", "ADDR", "COOKIE_SECURE", "STORAGE"} {
		t.Setenv(k, "")
	}
	t.Setenv("PRICE_PER_USER", "1500")
	c := Load()
	if c.FixedOTP != "" || c.Seed || c.PricePerUser != 1500 || c.Addr != ":8080" || c.Storage != "local" {
		t.Fatalf("по умолчанию: случайный код и без демо-данных, получили %+v", c)
	}
	t.Setenv("FIXED_OTP", "1111")
	t.Setenv("SEED", "1")
	if c := Load(); c.FixedOTP != "1111" || !c.Seed {
		t.Fatalf("явное включение: %+v", c)
	}
	t.Setenv("FIXED_OTP", "off")
	if Load().FixedOTP != "" {
		t.Fatal("off выключает тестовый код")
	}
}

func TestValidate(t *testing.T) {
	if err := (Config{FixedOTP: "1111", Storage: "local"}).Validate(); err != nil {
		t.Fatal("локально тестовый код разрешён")
	}
	if err := (Config{FixedOTP: "1111", CookieSecure: true, Storage: "local"}).Validate(); err == nil {
		t.Fatal("тестовый код за HTTPS должен запрещаться")
	}
	if err := (Config{Storage: "s3", S3: S3{Endpoint: "x"}}).Validate(); err == nil {
		t.Fatal("S3 без бакета и ключей")
	}
	if err := (Config{Storage: "ftp"}).Validate(); err == nil {
		t.Fatal("неизвестное хранилище")
	}
	ok := Config{Storage: "s3", S3: S3{Endpoint: "object.pscloud.io", Bucket: "b", AccessKey: "k", SecretKey: "s"}}
	if err := ok.Validate(); err != nil {
		t.Fatal(err)
	}
}
