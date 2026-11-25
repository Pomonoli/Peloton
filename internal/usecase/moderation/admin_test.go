package moderation

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/opencairn-org/cairn-core/internal/domain"
	"github.com/opencairn-org/cairn-core/internal/port"
	"github.com/opencairn-org/cairn-core/internal/usecase/audit"
)

type fakeReports struct {
	port.ReportRepo
	rows    []domain.ContentReport
	updated map[domain.ContentReportID]domain.ReportStatus
}

func (f *fakeReports) List(_ context.Context, status domain.ReportStatus, _ int, _ int) ([]domain.ContentReport, error) {
	var out []domain.ContentReport
	for _, r := range f.rows {
		if r.Status == status {
			out = append(out, r)
		}
	}
	return out, nil
}
func (f *fakeReports) Create(_ context.Context, r domain.ContentReport) (domain.ContentReportID, error) {
	f.rows = append(f.rows, r)
	return domain.ContentReportID(uuid.New()), nil
}
func (f *fakeReports) UpdateStatus(_ context.Context, id domain.ContentReportID, st domain.ReportStatus, _ domain.UserID) error {
	f.updated[id] = st
	return nil
}

type fakeUsers struct {
	port.UserReader
	names map[domain.UserID]string
}

func (f *fakeUsers) GetUser(_ context.Context, id domain.UserID) (domain.User, error) {
	if n, ok := f.names[id]; ok {
		return domain.User{ID: id, Username: n}, nil
	}
	return domain.User{}, domain.ErrNotFound
}

func kindOf(t *testing.T, err error) port.ActionErrorKind {
	t.Helper()
	var ae *port.ActionError
	if !errors.As(err, &ae) {
		t.Fatalf("not an ActionError: %v", err)
	}
	return ae.Kind
}

func TestReports(t *testing.T) {
	reporter := domain.UserID(uuid.New())
	open := domain.ContentReportID(uuid.New())
	reps := &fakeReports{rows: []domain.ContentReport{{ID: open, ReporterID: reporter, Status: domain.ReportOpen, TargetKind: domain.ReportTargetActivity, TargetID: uuid.New()}}, updated: map[domain.ContentReportID]domain.ReportStatus{}}
	r := &Reports{Reports: reps, Users: &fakeUsers{names: map[domain.UserID]string{reporter: "alice"}}}
	ctx := t.Context()

	rows, err := r.List(ctx, "", 0, 0)
	if err != nil || len(rows) != 1 || rows[0].ReporterUsername != "alice" {
		t.Fatalf("list: %v %+v", err, rows)
	}
	if st, err := r.Resolve(ctx, audit.Actor{}, open, "bogus"); err != nil || st != domain.ReportReviewed || reps.updated[open] != domain.ReportReviewed {
		t.Fatalf("resolve defaults to reviewed: %v %s", err, st)
	}
	if st, _ := r.Resolve(ctx, audit.Actor{}, open, domain.ReportDismissed); st != domain.ReportDismissed {
		t.Fatalf("dismiss: %s", st)
	}
	if err := r.SetActivityHidden(ctx, audit.Actor{}, domain.ActivityID(uuid.New()), true); kindOf(t, err) != port.ActionErrorUnavailable {
		t.Fatalf("no moderation repo: %v", err)
	}

}

func TestReportsFile(t *testing.T) {
	ctx := context.Background()
	repo := &fakeReports{}
	r := &Reports{Reports: repo}
	me, target := domain.UserID(uuid.New()), uuid.New()
	if _, err := r.File(ctx, me, "planet", target, ""); err == nil {
		t.Fatal("unknown target kind must fail")
	}
	long := strings.Repeat("x", domain.MaxReportReasonLength+10)
	if _, err := r.File(ctx, me, domain.ReportTargetActivity, target, "  "+long+"  "); err != nil {
		t.Fatal(err)
	}
	got := repo.rows[0]
	if got.ReporterID != me || got.TargetID != target || len(got.Reason) != domain.MaxReportReasonLength {
		t.Fatalf("report = %+v", got)
	}
	if _, err := (&Reports{}).File(ctx, me, domain.ReportTargetActivity, target, ""); err == nil {
		t.Fatal("no repo must be unavailable")
	}
}
