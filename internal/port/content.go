package port

import (
	"context"
	"time"

	"github.com/opencairn-org/cairn-core/internal/domain"
)

// ContentURL is a signed, short-lived URL fetched by an authenticated member.
type ContentURL struct {
	URL       string
	ExpiresAt time.Time
}

// ContentSigner mints URLs for binary content downloads and uploads.
type ContentSigner interface {
	// Blob signs a stored object. contentType and filename are hints for the
	// response headers; filename empty → inline.
	Blob(ctx context.Context, key, contentType, filename string) (ContentURL, error)
	// MapImage signs the static course image of an activity, trimmed by the
	// owner's privacy zones when trimmed is true (non-owner views). Rendered
	// on demand and cached when the GET arrives.
	MapImage(ctx context.Context, activityID domain.ActivityID, trimmed bool) (ContentURL, error)
	// Export signs an interchange-file download of the owner's activity,
	// rendered on GET. format is gpx|tcx|fit; exclude lists export features
	// to drop.
	Export(ctx context.Context, activityID domain.ActivityID, format string, exclude []string) (ContentURL, error)
	// Upload mints a PUT target for a client file upload of at most maxBytes:
	// a public presigned PUT when available, else the server's signed route.
	Upload(ctx context.Context, key, contentType string, maxBytes int64) (PresignedURL, error)
}
