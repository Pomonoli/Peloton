package account

import (
	"context"
	"errors"
	"strings"

	"github.com/opencairn-org/cairn-core/internal/domain"
	"github.com/opencairn-org/cairn-core/internal/port"
)

// ProfilePatch carries the fields a caller wants to change; nil leaves a
// field as it is. Email is deliberately absent: an address change needs a
// verification round-trip that neither surface offers yet.
type ProfilePatch struct {
	DisplayName *string
	AvatarURL   *string
	Locale      *string
	Timezone    *string
	Units       *string
}

// ErrEmailChangeUnsupported answers a patch that names the email.
var ErrEmailChangeUnsupported = errors.New("email changes are not supported; contact an admin")

// Profiles edits the caller's own profile.
type Profiles struct {
	Users port.UserProfileStore
}

// Update validates the patch, writes it and returns the new row.
func (p *Profiles) Update(ctx context.Context, userID domain.UserID, patch ProfilePatch) (domain.User, error) {
	upd, err := applyPatch(patch)
	if err != nil {
		return domain.User{}, invalid(err)
	}
	if err := p.Users.UpdateProfile(ctx, userID, upd); err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return domain.User{}, port.NewActionError(port.ActionErrorNotFound, "user not found")
		}
		return domain.User{}, internalErr("update profile", err)
	}
	u, err := p.Users.GetUser(ctx, userID)
	if err != nil {
		return domain.User{}, internalErr("reload user", err)
	}
	return u, nil
}

// applyPatch normalizes and validates each supplied field.
func applyPatch(patch ProfilePatch) (port.ProfileUpdate, error) {
	var upd port.ProfileUpdate
	set := func(dst **string, src *string, check func(string) error) error {
		if src == nil {
			return nil
		}
		v := strings.TrimSpace(*src)
		*dst = &v
		return check(v)
	}
	steps := []error{
		set(&upd.DisplayName, patch.DisplayName, func(v string) error {
			if v == "" {
				return domain.ErrDisplayNameRequired
			}
			return nil
		}),
		set(&upd.AvatarURL, patch.AvatarURL, domain.ValidateAvatarURL),
		set(&upd.Locale, patch.Locale, domain.ValidateLocale),
		set(&upd.Timezone, patch.Timezone, domain.ValidateTimezone),
		set(&upd.Units, patch.Units, func(v string) error {
			if !domain.UserUnits(v).Valid() {
				return errors.New("units must be metric or imperial")
			}
			return nil
		}),
	}
	for _, err := range steps {
		if err != nil {
			return port.ProfileUpdate{}, err
		}
	}
	return upd, nil
}
