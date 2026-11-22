package waas

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/opencairn-org/cairn-core/internal/domain"
	"github.com/opencairn-org/cairn-core/internal/port"
)

const MaxAssetSize int64 = 32 << 20

type Catalog struct {
	base   string
	client *http.Client
}

func NewCatalog(base string, client *http.Client) *Catalog {
	copyClient := *client
	origin, _ := url.Parse(base)
	copyClient.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 || req.URL.Host != origin.Host || req.URL.Scheme != origin.Scheme {
			return http.ErrUseLastResponse
		}
		return nil
	}
	return &Catalog{base: strings.TrimRight(base, "/"), client: &copyClient}
}

func (c *Catalog) Open(ctx context.Context, name string) (port.RouteAsset, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/"+url.PathEscape(name), nil)
	if err != nil {
		return port.RouteAsset{}, fmt.Errorf("waas request: %w", err)
	}
	res, err := c.client.Do(req)
	if err != nil {
		return port.RouteAsset{}, fmt.Errorf("waas catalog: %w", err)
	}
	if res.StatusCode != http.StatusOK || res.ContentLength > MaxAssetSize {
		_ = res.Body.Close()
		if res.StatusCode == http.StatusNotFound {
			return port.RouteAsset{}, domain.ErrNotFound
		}
		return port.RouteAsset{}, fmt.Errorf("waas catalog returned status %d or an oversized asset", res.StatusCode)
	}
	return port.RouteAsset{Body: res.Body, Size: res.ContentLength, ETag: res.Header.Get("ETag")}, nil
}
