package httpdelivery

import (
	"encoding/csv"
	"net/http"
)

// startCSV пишет BOM и «;» — так Excel с русской локалью открывает файл без мастера импорта.
func startCSV(w http.ResponseWriter, name string) *csv.Writer {
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	w.Write([]byte("\xef\xbb\xbf"))
	cw := csv.NewWriter(w)
	cw.Comma = ';'
	return cw
}
