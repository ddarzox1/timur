// Package sigex содержит порт SIGEX и безопасную fake-реализацию для локальной разработки.
package sigex

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"qauipsiz/internal/usecase/induction"
)

type Fake struct {
	mu   sync.Mutex
	ops  map[string]induction.EDSResult
	next int64
}

func NewFake() *Fake { return &Fake{ops: make(map[string]induction.EDSResult), next: 1} }

func (f *Fake) Start(_ context.Context, _ []byte, _ string) (induction.EDSOperation, error) {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return induction.EDSOperation{}, err
	}
	id := hex.EncodeToString(b)
	f.mu.Lock()
	f.ops[id] = induction.EDSResult{}
	f.mu.Unlock()
	return induction.EDSOperation{ID: id, ExpiresAt: time.Now().Add(10 * time.Minute)}, nil
}

func (f *Fake) Poll(_ context.Context, id string) (induction.EDSResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.ops[id]
	if !ok {
		return r, fmt.Errorf("SIGEX fake: операция не найдена")
	}
	return r, nil
}

func (f *Fake) Complete(id, iin, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.ops[id]; !ok {
		return fmt.Errorf("SIGEX fake: операция не найдена")
	}
	f.ops[id] = induction.EDSResult{Done: true, IIN: iin, Name: name, CMS: "FAKE-CMS-" + id}
	return nil
}

func (f *Fake) Register(_ context.Context, documentID string, _ []byte, cms string) (string, int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if documentID == "" {
		documentID = "fake-doc-" + fmt.Sprint(time.Now().UnixNano())
	}
	id := f.next
	f.next++
	if cms == "" {
		return "", 0, fmt.Errorf("SIGEX fake: пустая CMS")
	}
	return documentID, id, nil
}

func (f *Fake) BuildDDC(_ context.Context, documentID string, pdf []byte) ([]byte, error) {
	if documentID == "" {
		return nil, fmt.Errorf("SIGEX fake: документ ещё не зарегистрирован")
	}
	// В fake-режиме исходный PDF остаётся пригодным для просмотра и тестирования.
	return pdf, nil
}

var _ induction.EDS = (*Fake)(nil)
