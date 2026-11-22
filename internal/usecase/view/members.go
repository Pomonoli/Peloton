package view

import (
	"context"
	"errors"
	"strings"

	"github.com/opencairn-org/cairn-core/internal/domain"
	"github.com/opencairn-org/cairn-core/internal/port"
)

// Members resolves a local cycling-data subject for a signed-in member.
type Members struct {
	Users port.UserReader
}

func (m *Members) Get(ctx context.Context, viewer domain.UserID, member string) (domain.User, error) {
	if viewer == (domain.UserID{}) {
		return domain.User{}, ErrForbidden
	}
	if m == nil || m.Users == nil {
		return domain.User{}, ErrUnavailable
	}
	var u domain.User
	var err error
	if id, parseErr := domain.ParseUUID[domain.UserID](member); parseErr == nil {
		u, err = m.Users.GetUser(ctx, id)
	} else {
		u, err = m.Users.GetUserByUsername(ctx, strings.ToLower(strings.TrimSpace(member)))
	}
	if errors.Is(err, domain.ErrNotFound) || u.Status == domain.UserStatusDeleted {
		return domain.User{}, domain.ErrNotFound
	}
	return u, err
}

// Read selects the subject and retains the distinction between self and community reads.
func (m *Members) Read(ctx context.Context, viewer domain.UserID, member string) (context.Context, domain.UserID, error) {
	if viewer == (domain.UserID{}) {
		return ctx, viewer, ErrForbidden
	}
	if member == "" {
		return ctx, viewer, nil
	}
	u, err := m.Get(ctx, viewer, member)
	if err != nil {
		return ctx, domain.UserID{}, err
	}
	return port.WithActivityReadViewer(ctx, viewer == u.ID), u.ID, nil
}
