// Package pilot — заявки на бесплатный пилот с лендинга.
package pilot

import (
	"context"

	"qauipsiz/internal/domain"
)

type Repo interface {
	SavePilotRequest(ctx context.Context, company, name, phone, employees string) error
}

type Service struct{ repo Repo }

func New(repo Repo) *Service { return &Service{repo: repo} }

func (s *Service) Request(ctx context.Context, company, name, phone, employees string) error {
	company, name = domain.Clip(company, domain.MaxTitle), domain.Clip(name, domain.MaxTitle)
	phone = domain.NormalizePhone(phone)
	if company == "" || name == "" || phone == "" {
		return domain.Invalid("Заполните компанию, имя и телефон")
	}
	return s.repo.SavePilotRequest(ctx, company, name, phone, domain.Clip(employees, domain.MaxTitle))
}
