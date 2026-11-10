package spreadsheet

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"strconv"
)

// XLSX uses inline strings so names beginning with '=' remain text, never formulas.
func XLSX(headers []string, rows [][]string) ([]byte, error) {
	if len(rows) > 1048575 {
		return nil, fmt.Errorf("too many spreadsheet rows")
	}
	var output bytes.Buffer
	archive := zip.NewWriter(&output)
	parts := []struct{ name, body string }{
		{"[Content_Types].xml", `<?xml version="1.0" encoding="UTF-8"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/><Override PartName="/xl/worksheets/sheet1.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/></Types>`},
		{"_rels/.rels", `<?xml version="1.0" encoding="UTF-8"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/></Relationships>`},
		{"xl/workbook.xml", `<?xml version="1.0" encoding="UTF-8"?><workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets><sheet name="Ученики" sheetId="1" r:id="rId1"/></sheets></workbook>`},
		{"xl/_rels/workbook.xml.rels", `<?xml version="1.0" encoding="UTF-8"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/></Relationships>`},
	}
	for _, part := range parts {
		w, err := archive.Create(part.name)
		if err != nil {
			return nil, err
		}
		if _, err = io.WriteString(w, part.body); err != nil {
			return nil, err
		}
	}
	sheet, err := archive.Create("xl/worksheets/sheet1.xml")
	if err != nil {
		return nil, err
	}
	if _, err = io.WriteString(sheet, `<?xml version="1.0" encoding="UTF-8"?><worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetViews><sheetView workbookViewId="0"><pane ySplit="1" topLeftCell="A2" activePane="bottomLeft" state="frozen"/></sheetView></sheetViews><cols><col min="1" max="1" width="8" customWidth="1"/><col min="2" max="7" width="28" customWidth="1"/></cols><sheetData>`); err != nil {
		return nil, err
	}
	writeRow := func(number int, values []string) error {
		if _, err := fmt.Fprintf(sheet, `<row r="%d">`, number); err != nil {
			return err
		}
		for _, value := range values {
			if _, err := io.WriteString(sheet, `<c t="inlineStr"><is><t xml:space="preserve">`); err != nil {
				return err
			}
			if err := xml.EscapeText(sheet, []byte(value)); err != nil {
				return err
			}
			if _, err := io.WriteString(sheet, `</t></is></c>`); err != nil {
				return err
			}
		}
		_, err := io.WriteString(sheet, `</row>`)
		return err
	}
	if err = writeRow(1, headers); err != nil {
		return nil, err
	}
	for i, row := range rows {
		if err = writeRow(i+2, row); err != nil {
			return nil, err
		}
	}
	if _, err = io.WriteString(sheet, `</sheetData><autoFilter ref="A1:G`+strconv.Itoa(len(rows)+1)+`"/></worksheet>`); err != nil {
		return nil, err
	}
	if err = archive.Close(); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}
