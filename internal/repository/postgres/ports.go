package postgres

import (
	"qauipsiz/internal/usecase/auth"
	"qauipsiz/internal/usecase/checklist"
	"qauipsiz/internal/usecase/dashboard"
	"qauipsiz/internal/usecase/hazard"
	"qauipsiz/internal/usecase/induction"
	"qauipsiz/internal/usecase/people"
	"qauipsiz/internal/usecase/pilot"
)

// Store реализует все порты usecase — проверка на этапе компиляции.
var (
	_ auth.Repo           = (*Store)(nil)
	_ hazard.Repo         = (*Store)(nil)
	_ hazard.Directory    = (*Store)(nil)
	_ induction.Repo      = (*Store)(nil)
	_ induction.Directory = (*Store)(nil)
	_ checklist.Repo      = (*Store)(nil)
	_ checklist.Directory = (*Store)(nil)
	_ people.Repo         = (*Store)(nil)
	_ dashboard.Repo      = (*Store)(nil)
	_ pilot.Repo          = (*Store)(nil)
)
