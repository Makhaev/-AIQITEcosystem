package family

import (
	"strings"
	"testing"
)

func TestReportPeriodAndTemplate(t *testing.T) {
	for _, in := range []ReportInput{{Generate: true}, {From: "2026-02-30", To: "2026-03-01"}, {From: "2026-03-02", To: "2026-03-01"}, {From: "2026-03-01"}, {To: "2026-03-01"}, {From: "0000-01-01", To: "2026-01-01"}} {
		if validateReportComposition(in) == nil {
			t.Fatalf("accepted bad period: %+v", in)
		}
	}
	if err := validateReportComposition(ReportInput{Generate: true, From: "2024-02-29", To: "2024-02-29"}); err != nil {
		t.Fatal(err)
	}
	s := &ReportSnapshot{From: "2024-02-29", To: "2024-02-29", Works: []ReportWork{{Kind: "attempts", Title: "PRIVATE TEXT"}, {Kind: "achievements", Title: "PRIVATE TEXT"}}}
	body := reportTemplate(s)
	if strings.Contains(body, "PRIVATE") || !strings.Contains(body, "Нет учтённых") || !strings.Contains(body, "Принятых работ: 1") || !strings.Contains(body, "достижений: 1") {
		t.Fatal(body)
	}
}
