// Package pdf формирует неизменяемый PDF-протокол инструктажа.
package pdf

import (
	"bytes"
	"crypto/sha256"
	"fmt"

	"github.com/go-pdf/fpdf"

	"qauipsiz/internal/domain"
	"qauipsiz/web"
)

type Protocol struct{}

func (Protocol) Build(in domain.ProtocolInput) ([]byte, string, error) {
	font, err := web.FS.ReadFile("static/fonts/GolosText.ttf")
	if err != nil {
		return nil, "", err
	}
	p := fpdf.New("P", "mm", "A4", "")
	p.SetMargins(18, 16, 18)
	p.SetAutoPageBreak(true, 16)
	p.AddUTF8FontFromBytes("Golos", "", font)
	p.SetFont("Golos", "", 16)
	p.AddPage()
	p.CellFormat(0, 9, "ПРОТОКОЛ ИНСТРУКТАЖА", "", 1, "C", false, 0, "")
	p.SetFont("Golos", "", 10)
	line(p, "Организация", fmt.Sprintf("%s (БИН %s)", in.Org.Name, in.Org.BIN))
	line(p, "Партия", in.Batch)
	line(p, "Вид", domain.KindLabels[in.Template.Kind])
	line(p, "Инструктаж", in.Template.Title)
	line(p, "Назначен", in.AssignedAt.Format("02.01.2006 15:04"))
	line(p, "Подписать до", in.DueAt.Format("02.01.2006"))
	line(p, "Инструктирующий", in.Instructor)
	line(p, "Рабочее место", in.Location)
	line(p, "Причина", in.Reason)
	line(p, "Основание", in.Basis)
	line(p, "Вид работ", in.WorkType)
	line(p, "Документы", in.Docs)

	line(p, "SHA-256 материала", in.MaterialSHA)
	p.Ln(3)
	p.SetFont("Golos", "", 8)
	p.MultiCell(0, 4, "Подписывая документ ЭЦП, участник подтверждает, что ознакомился с материалом, понял требования безопасности и согласен с указанными в протоколе сведениями.", "", "L", false)
	p.Ln(2)
	p.MultiCell(0, 4, "Электронные подписи и сведения об их проверке содержатся в карточке электронного документа SIGEX (DDC).", "", "L", false)

	var out bytes.Buffer
	if err := p.Output(&out); err != nil {
		return nil, "", err
	}
	pdfBytes := out.Bytes()
	return pdfBytes, fmt.Sprintf("%x", sha256.Sum256(pdfBytes)), nil
}

func line(p *fpdf.Fpdf, label, value string) {
	if value == "" {
		return
	}
	p.SetFont("Golos", "", 9)
	p.CellFormat(42, 6, label+":", "", 0, "L", false, 0, "")
	p.SetFont("Golos", "", 10)
	p.MultiCell(0, 6, value, "", "L", false)
}
