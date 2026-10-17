package pilot

import (
	"context"
	"errors"
	"testing"

	"qauipsiz/internal/domain"
)

type repo struct{ phone string }

func (r *repo) SavePilotRequest(_ context.Context, _, _, phone, _ string) error {
	r.phone = phone
	return nil
}

func TestRequest(t *testing.T) {
	r := &repo{}
	s := New(r)
	var ve domain.ValidationError
	if err := s.Request(context.Background(), "ТОО", "Имя", "123", ""); !errors.As(err, &ve) {
		t.Fatal("неверный телефон")
	}
	if err := s.Request(context.Background(), " ТОО ", "Имя", "8 701 000 00 01", "до 50"); err != nil || r.phone != "77010000001" {
		t.Fatalf("%v %s", err, r.phone)
	}
}
