package connect

import (
	"context"
	"errors"

	connectrpc "connectrpc.com/connect/v2"

	"github.com/opencairn-org/cairn-core/internal/domain"
	"github.com/opencairn-org/cairn-core/internal/usecase/view"
)

func memberRead(ctx context.Context, members *view.Members, member string) (context.Context, domain.UserID, error) {
	viewer, err := userIDFromHandlerCtx(ctx)
	if err != nil {
		return ctx, viewer, err
	}
	ctx, subject, err := members.Read(ctx, viewer, member)
	if errors.Is(err, domain.ErrNotFound) {
		err = connectrpc.NewError(connectrpc.CodeNotFound, "member not found")
	} else if err != nil {
		err = actionErr(err)
	}
	return ctx, subject, err
}
