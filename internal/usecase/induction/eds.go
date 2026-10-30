package induction

import (
	"context"
	"fmt"
	"sync"

	"qauipsiz/internal/domain"
)

type SigningOperation struct {
	ID, Batch, QRCode, LaunchLink string
	Done, Instructor              bool
	Error                         string
	SignedBy                      string
	state                         *operationState
}

type operationState struct {
	mu         sync.Mutex
	viewer     domain.User
	batch      string
	op         EDSOperation
	done       bool
	instructor bool
	err        error
	signedBy   string
}

func (s *Service) StartKiosk(ctx context.Context, u domain.User, batch string) (SigningOperation, error) {
	return s.startSigning(ctx, u, batch)
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
	// Доступ уже доказан назначением; технический viewer нужен только для чтения партии.
	viewer := u
	viewer.Role = domain.RoleHSE
	return s.startSigning(ctx, viewer, a.Batch)
}

func (s *Service) startSigning(ctx context.Context, u domain.User, batch string) (SigningOperation, error) {
	state, err := s.repo.KioskState(ctx, u, batch)
	if err != nil {
		return SigningOperation{}, err
	}
	if state.Doc.InstructorSignedAt != nil {
		return SigningOperation{}, domain.Invalid("Протокол уже полностью подписан")
	}
	payload, err := s.files.Read(u.OrgID, state.Doc.PDFPath)
	if err != nil {
		return SigningOperation{}, err
	}
	op, err := s.eds.Start(ctx, payload, "Протокол инструктажа "+batch)
	if err != nil {
		return SigningOperation{}, err
	}
	x := &operationState{viewer: u, batch: batch, op: op, instructor: state.InstructorPending}
	s.ops.Store(op.ID, x)
	return signingView(x), nil
}

func (s *Service) SigningStatus(ctx context.Context, u domain.User, operationID string) (SigningOperation, error) {
	v, ok := s.ops.Load(operationID)
	if !ok {
		return SigningOperation{}, domain.ErrNotFound
	}
	x := v.(*operationState)
	if x.viewer.OrgID != u.OrgID {
		return SigningOperation{}, domain.ErrNotFound
	}
	x.mu.Lock()
	defer x.mu.Unlock()
	if x.done || x.err != nil {
		return signingView(x), nil
	}
	r, err := s.eds.Poll(ctx, operationID)
	if err != nil {
		x.err = err
		return signingView(x), nil
	}
	if !r.Done {
		return signingView(x), nil
	}
	r.IIN = domain.NormalizeIIN(r.IIN)
	if !domain.ValidIIN(r.IIN) {
		x.err = domain.Invalid("SIGEX вернул некорректный ИИН подписанта")
		return signingView(x), nil
	}
	if err := s.repo.CanSignBatch(ctx, x.viewer, x.batch, r.IIN); err != nil {
		x.err = err
		return signingView(x), nil
	}
	state, err := s.repo.KioskState(ctx, x.viewer, x.batch)
	if err != nil {
		x.err = err
		return signingView(x), nil
	}
	payload, err := s.files.Read(u.OrgID, state.Doc.PDFPath)
	if err != nil {
		x.err = err
		return signingView(x), nil
	}
	docID, signID, err := s.eds.Register(ctx, state.Doc.SIGEXID, payload, r.CMS)
	if err != nil {
		x.err = err
		return signingView(x), nil
	}
	err = s.repo.ApplyBatchEDS(ctx, x.viewer, x.batch, domain.EDSSignature{
		IIN: r.IIN, Name: r.Name, CMS: r.CMS, SIGEXID: docID, SignID: signID,
	})
	if err != nil {
		x.err = err
		return signingView(x), nil
	}
	x.done, x.signedBy = true, r.Name
	return signingView(x), nil
}

func signingView(x *operationState) SigningOperation {
	v := SigningOperation{ID: x.op.ID, Batch: x.batch, QRCode: x.op.QRCode, LaunchLink: x.op.LaunchLink,
		Done: x.done, Instructor: x.instructor, SignedBy: x.signedBy, state: x}
	if x.err != nil {
		v.Error = x.err.Error()
	}
	return v
}

func (s *Service) CompleteFake(operationID, iin, name string) error {
	if s.eds == nil {
		return fmt.Errorf("SIGEX не настроен")
	}
	return s.eds.Complete(operationID, domain.NormalizeIIN(iin), name)
}

func (s *Service) DDC(ctx context.Context, u domain.User, batch string) ([]byte, error) {
	state, err := s.repo.KioskState(ctx, u, batch)
	if err != nil {
		return nil, err
	}
	payload, err := s.files.Read(u.OrgID, state.Doc.PDFPath)
	if err != nil {
		return nil, err
	}
	return s.eds.BuildDDC(ctx, state.Doc.SIGEXID, payload)
}
