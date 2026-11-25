package induction

import (
	"context"
	"fmt"
	"time"

	"qauipsiz/internal/domain"
)

type SigningOperation struct {
	ID, Batch, QRCode, LaunchLink string
	AssignmentID                  int64
	Done, Instructor              bool
	Error                         string
	SignedBy                      string
}

func (s *Service) StartKiosk(ctx context.Context, u domain.User, batch string) (SigningOperation, error) {
	rows, err := s.repo.BatchAssignments(ctx, u, batch)
	if err != nil {
		return SigningOperation{}, err
	}
	for _, a := range rows {
		if a.SignedAt != nil && a.InstructorSignedAt == nil && a.RecordPDFPath != "" {
			return s.startAssignmentSigning(ctx, u, a, true)
		}
	}
	for _, a := range rows {
		if a.SignedAt == nil && a.KnowledgeResult != "" && a.RecordPDFPath != "" {
			return s.startAssignmentSigning(ctx, u, a, false)
		}
	}
	for _, a := range rows {
		if a.KnowledgeResult == "" {
			return SigningOperation{}, domain.Invalid("Сначала зафиксируйте результат проверки знаний для участников")
		}
	}
	return SigningOperation{}, domain.Invalid("Все записи партии уже подписаны")
}

func (s *Service) StartMobile(ctx context.Context, u domain.User, assignmentID int64) (SigningOperation, error) {
	a, err := s.repo.UserAssignment(ctx, u.ID, assignmentID)
	if err != nil {
		return SigningOperation{}, err
	}
	if a.SignedAt != nil {
		return SigningOperation{}, domain.Invalid("Инструктаж уже подписан")
	}
	if u.IIN == "" {
		return SigningOperation{}, domain.Invalid("Для подписи ЭЦП укажите ИИН в профиле")
	}
	if a.KnowledgeResult == "" || a.RecordPDFPath == "" {
		return SigningOperation{}, domain.Invalid("Инструктирующий ещё не зафиксировал результат проверки знаний")
	}
	viewer := u
	viewer.Role = domain.RoleHSE
	return s.startAssignmentSigning(ctx, viewer, a, false)
}

func (s *Service) startAssignmentSigning(ctx context.Context, u domain.User, a domain.Assignment, instructor bool) (SigningOperation, error) {
	if a.RecordPDFPath == "" {
		return SigningOperation{}, domain.Invalid("Запись ещё не подготовлена к подписанию")
	}
	expectedIIN := a.UserIIN
	label := "Запись инструктажа работника " + a.UserName
	if instructor {
		expectedIIN = a.InstructorIIN
		label = "Подтверждение инструктажа работника " + a.UserName
	}
	if !domain.ValidIIN(expectedIIN) {
		return SigningOperation{}, domain.Invalid("У ожидаемого подписанта не указан корректный ИИН")
	}
	payload, err := s.files.Read(u.OrgID, a.RecordPDFPath)
	if err != nil {
		return SigningOperation{}, err
	}
	provider, err := s.eds.Start(ctx, payload, label)
	if err != nil {
		return SigningOperation{}, err
	}
	op := domain.SigningOperationRecord{
		ID: provider.ID, OrgID: u.OrgID, ViewerID: u.ID, ViewerRole: u.Role, ViewerDepartmentID: u.DepartmentID,
		AssignmentID: a.ID, Batch: a.Batch, ExpectedIIN: expectedIIN, Instructor: instructor,
		QRCode: provider.QRCode, LaunchLink: provider.LaunchLink, DataURL: provider.DataURL, SignURL: provider.SignURL,
		ExpiresAt: provider.ExpiresAt, Stage: domain.SigningPending,
	}
	if err := s.repo.CreateSigningOperation(ctx, op); err != nil {
		return SigningOperation{}, err
	}
	return signingView(op), nil
}

func (s *Service) SigningStatus(ctx context.Context, u domain.User, operationID string) (SigningOperation, error) {
	op, err := s.repo.SigningOperation(ctx, u.OrgID, operationID)
	if err != nil {
		return SigningOperation{}, err
	}
	if op.ViewerID != u.ID && !u.HasBackoffice() {
		return SigningOperation{}, domain.ErrNotFound
	}
	if op.Stage == domain.SigningDone || op.Stage == domain.SigningFailed {
		return signingView(op), nil
	}
	op, claimed, err := s.repo.ClaimSigningOperation(ctx, u.OrgID, operationID)
	if err != nil {
		return SigningOperation{}, err
	}
	if !claimed {
		return signingView(op), nil
	}
	if op.Stage == domain.SigningPending {
		if time.Now().After(op.ExpiresAt) {
			return s.failSigning(ctx, op, "Срок действия QR-кода истёк")
		}
		r, err := s.eds.Poll(ctx, EDSOperation{ID: op.ID, QRCode: op.QRCode, LaunchLink: op.LaunchLink,
			DataURL: op.DataURL, SignURL: op.SignURL, ExpiresAt: op.ExpiresAt})
		if err != nil {
			_ = s.repo.ReleaseSigningOperation(ctx, op.OrgID, op.ID)
			return SigningOperation{}, err
		}
		if !r.Done {
			if err := s.repo.ReleaseSigningOperation(ctx, op.OrgID, op.ID); err != nil {
				return SigningOperation{}, err
			}
			return signingView(op), nil
		}
		r.IIN = domain.NormalizeIIN(r.IIN)
		if !domain.ValidIIN(r.IIN) {
			return s.failSigning(ctx, op, "SIGEX вернул некорректный ИИН подписанта")
		}
		if r.IIN != op.ExpectedIIN {
			return s.failSigning(ctx, op, "ИИН подписанта не совпадает с ожидаемым ИИН")
		}
		op.Stage, op.SignerIIN, op.SignerName, op.CMS = domain.SigningSigned, r.IIN, r.Name, r.CMS
		if err := s.repo.SaveSigningOperation(ctx, op); err != nil {
			return SigningOperation{}, err
		}
		return signingView(op), nil
	}

	viewer := domain.User{ID: op.ViewerID, OrgID: op.OrgID, Role: op.ViewerRole, DepartmentID: op.ViewerDepartmentID}
	a, err := s.repo.ScopedAssignment(ctx, viewer, op.AssignmentID)
	if err != nil {
		_ = s.repo.ReleaseSigningOperation(ctx, op.OrgID, op.ID)
		return SigningOperation{}, err
	}
	if op.Stage == domain.SigningSigned {
		payload, err := s.files.Read(op.OrgID, a.RecordPDFPath)
		if err != nil {
			_ = s.repo.ReleaseSigningOperation(ctx, op.OrgID, op.ID)
			return SigningOperation{}, err
		}
		docID, signID, err := s.eds.Register(ctx, a.SIGEXID, payload, op.CMS)
		if err != nil {
			_ = s.repo.ReleaseSigningOperation(ctx, op.OrgID, op.ID)
			return SigningOperation{}, err
		}
		op.Stage, op.SIGEXID, op.SIGEXSignID = domain.SigningRegistered, docID, &signID
		if err := s.repo.SaveSigningOperation(ctx, op); err != nil {
			return SigningOperation{}, err
		}
		return signingView(op), nil
	}

	if op.Stage == domain.SigningRegistered {
		if op.SIGEXSignID == nil {
			return s.failSigning(ctx, op, "SIGEX не вернул идентификатор подписи")
		}
		err = s.repo.ApplyAssignmentEDS(ctx, viewer, op.AssignmentID, op.Instructor, domain.EDSSignature{
			IIN: op.SignerIIN, Name: op.SignerName, CMS: op.CMS, SIGEXID: op.SIGEXID, SignID: *op.SIGEXSignID,
		})
		if err != nil {
			_ = s.repo.ReleaseSigningOperation(ctx, op.OrgID, op.ID)
			return SigningOperation{}, err
		}
		op.Stage = domain.SigningDone
		if err := s.repo.SaveSigningOperation(ctx, op); err != nil {
			return SigningOperation{}, err
		}
		return signingView(op), nil
	}

	return s.failSigning(ctx, op, "Неизвестное состояние операции подписания")
}

func (s *Service) failSigning(ctx context.Context, op domain.SigningOperationRecord, message string) (SigningOperation, error) {
	op.Stage, op.Error = domain.SigningFailed, message
	if err := s.repo.SaveSigningOperation(ctx, op); err != nil {
		return SigningOperation{}, err
	}
	return signingView(op), nil
}

func signingView(op domain.SigningOperationRecord) SigningOperation {
	return SigningOperation{ID: op.ID, Batch: op.Batch, AssignmentID: op.AssignmentID, QRCode: op.QRCode,
		LaunchLink: op.LaunchLink, Done: op.Stage == domain.SigningDone, Instructor: op.Instructor,
		Error: op.Error, SignedBy: op.SignerName}
}

func (s *Service) CompleteFake(operationID, iin, name string) error {
	if s.eds == nil {
		return fmt.Errorf("SIGEX не настроен")
	}
	return s.eds.Complete(operationID, domain.NormalizeIIN(iin), name)
}

func (s *Service) DDC(ctx context.Context, u domain.User, assignmentID int64) ([]byte, error) {
	a, err := s.repo.ScopedAssignment(ctx, u, assignmentID)
	if err != nil {
		return nil, err
	}
	if a.SIGEXID == "" || a.RecordPDFPath == "" {
		return nil, domain.Invalid("Запись ещё не подписана")
	}
	payload, err := s.files.Read(u.OrgID, a.RecordPDFPath)
	if err != nil {
		return nil, err
	}
	return s.eds.BuildDDC(ctx, a.SIGEXID, payload)
}
