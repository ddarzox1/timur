package pdf

import (
	"bytes"
	"testing"
	"time"

	"qauipsiz/internal/domain"
)

func TestProtocolBuild(t *testing.T) {
	b, sum, err := (Protocol{}).Build(domain.ProtocolInput{
		Org:      domain.Org{Name: "ТОО Қауіпсіз", BIN: "123456789012"},
		Template: domain.InductionTemplate{Title: "Работа на высоте", Kind: "primary", Body: "Требования безопасности"},
		Batch:    "batch-1", AssignedAt: time.Now(), DueAt: time.Now().AddDate(0, 0, 1), Instructor: "Айгерим Н.", MaterialSHA: "abc123",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(b) < 1000 || !bytes.HasPrefix(b, []byte("%PDF")) || len(sum) != 64 {
		t.Fatalf("неверный PDF: bytes=%d sum=%q", len(b), sum)
	}
}
