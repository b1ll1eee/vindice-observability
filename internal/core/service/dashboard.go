package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"time"

	"github.com/b1ll1eee/vindice-obs/internal/core/domain"
	"github.com/b1ll1eee/vindice-obs/internal/core/port"
)

type DashboardService struct {
	repo port.DashboardRepository
	now  func() time.Time
}

var _ port.DashboardManager = (*DashboardService)(nil)

func NewDashboardService(repo port.DashboardRepository) *DashboardService {
	return &DashboardService{repo: repo, now: time.Now}
}

func (s *DashboardService) ListDashboards(ctx context.Context) ([]domain.Dashboard, error) {
	return s.repo.ListDashboards(ctx)
}

func (s *DashboardService) GetDashboard(ctx context.Context, id string) (domain.Dashboard, error) {
	d, ok, err := s.repo.GetDashboard(ctx, id)
	if err == nil && !ok {
		err = domain.ErrNotFound
	}
	return d, err
}

func (s *DashboardService) SaveDashboard(ctx context.Context, d domain.Dashboard) (domain.Dashboard, error) {
	if err := d.Validate(); err != nil {
		return d, err
	}
	if d.ID == "" {
		d.ID = newID()
	} else if _, err := s.GetDashboard(ctx, d.ID); err != nil {
		return d, err
	}
	for i := range d.Panels {
		if d.Panels[i].ID == "" {
			d.Panels[i].ID = newID()
		}
	}
	d.UpdatedAt = s.now().UTC()
	return d, s.repo.SaveDashboard(ctx, d)
}

func (s *DashboardService) DeleteDashboard(ctx context.Context, id string) error {
	if _, err := s.GetDashboard(ctx, id); err != nil {
		return err
	}
	return s.repo.DeleteDashboard(ctx, id)
}

func newID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
