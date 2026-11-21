// Package garmin reads cycling activities through Garmin Connect's Personal API.
package garmin

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/opencairn-org/cairn-core/internal/domain"
	"github.com/opencairn-org/cairn-core/internal/port"
)

const (
	apiBase     = "https://personal.connectapi.garmin.com"
	tokenPath   = "/personalapitoken-service/personal-api/personal-api-key/token"
	profilePath = "/userprofile-service/userprofile/settings"
	searchPath  = "/activitylist-service/activities/search/activities"
	filePath    = "/download-service/files/activity/"
)

type Client struct {
	base string
	http *http.Client
}

var _ port.GarminConnect = (*Client)(nil)

func New() *Client {
	return &Client{base: apiBase, http: &http.Client{
		Timeout: 20 * time.Second,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}}
}

func (c *Client) ExchangeKey(ctx context.Context, key string) (domain.GarminSession, error) {
	body, err := c.request(ctx, http.MethodPost, tokenPath, "ApiKey "+key, "*/*", 16<<10, false)
	if err != nil {
		return domain.GarminSession{}, err
	}
	token := strings.TrimSpace(string(body))
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return domain.GarminSession{}, errors.New("Garmin returned an invalid access token")
	}
	// This token comes directly from Garmin over HTTPS. The expiry is used
	// only to schedule refresh. Garmin validates the token on each call.
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	var claims struct {
		Expiry int64 `json:"exp"`
	}
	if err != nil || json.Unmarshal(payload, &claims) != nil || claims.Expiry <= 0 {
		return domain.GarminSession{}, errors.New("Garmin returned an invalid token expiry")
	}
	return domain.GarminSession{Key: key, Token: token, ExpiresAt: time.Unix(claims.Expiry, 0).UTC()}, nil
}

func (c *Client) ProfileHandle(ctx context.Context, token string) (string, error) {
	body, err := c.request(ctx, http.MethodGet, profilePath, "Bearer "+token, "application/json", 1<<20, false)
	if err != nil {
		return "", err
	}
	var profile struct {
		DisplayName string `json:"displayName"`
	}
	if err := json.Unmarshal(body, &profile); err != nil {
		return "", errors.New("Garmin returned an invalid profile")
	}
	if profile.DisplayName == "" {
		return "", errors.New("Garmin did not return an account handle")
	}
	return profile.DisplayName, nil
}

func (c *Client) Rides(ctx context.Context, token string, from, through time.Time, offset, limit int) ([]domain.GarminActivity, error) {
	query := url.Values{
		"start": {strconv.Itoa(offset)}, "limit": {strconv.Itoa(limit)},
		"activityType": {"cycling"}, "endDate": {through.Format(time.DateOnly)}, "excludeChildren": {"false"},
	}
	if !from.IsZero() {
		query.Set("startDate", from.Format(time.DateOnly))
	}
	body, err := c.request(ctx, http.MethodGet, searchPath+"?"+query.Encode(), "Bearer "+token, "application/json", 2<<20, false)
	if err != nil {
		return nil, err
	}
	var rows []activitySummary
	if err := json.Unmarshal(body, &rows); err != nil || rows == nil || len(rows) > limit {
		return nil, errors.New("Garmin returned an invalid activity page")
	}
	rides := make([]domain.GarminActivity, 0, len(rows))
	for _, row := range rows {
		rides = append(rides, row.ride())
	}
	return rides, nil
}

type activitySummary struct {
	ID           int64    `json:"activityId"`
	Title        string   `json:"activityName"`
	Description  string   `json:"description"`
	StartTime    string   `json:"startTimeGMT"`
	LocalTime    string   `json:"startTimeLocal"`
	Distance     *float64 `json:"distance"`
	Elevation    *float64 `json:"elevationGain"`
	AvgHeartRate *float64 `json:"averageHR"`
	AvgPower     *float64 `json:"avgPower"`
	AvgCadence   *float64 `json:"averageBikingCadenceInRevPerMinute"`
	Duration     float64  `json:"duration"`
	Elapsed      float64  `json:"elapsedDuration"`
	Moving       float64  `json:"movingDuration"`
	ActivityType struct {
		Key string `json:"typeKey"`
	} `json:"activityType"`
}

func (s activitySummary) ride() domain.GarminActivity {
	start, err := time.Parse(time.RFC3339Nano, s.StartTime)
	if err != nil {
		start, _ = time.Parse("2006-01-02 15:04:05", s.StartTime)
		if start.IsZero() {
			start, _ = time.Parse("2006-01-02T15:04:05", s.StartTime)
		}
	}
	var localDay time.Time
	if len(s.LocalTime) >= len(time.DateOnly) {
		localDay, _ = time.Parse(time.DateOnly, s.LocalTime[:len(time.DateOnly)])
	}
	ride := domain.GarminActivity{
		Title: s.Title, Type: s.ActivityType.Key, Description: s.Description, StartTime: start.UTC(), LocalDay: localDay,
		Elapsed: seconds(s.Elapsed), Moving: seconds(s.Moving),
		DistanceM: s.Distance, ElevationM: s.Elevation,
		AvgHeartRate: wholeNumber(s.AvgHeartRate), AvgPower: wholeNumber(s.AvgPower), AvgCadence: wholeNumber(s.AvgCadence),
	}
	if s.ID > 0 {
		ride.ID = strconv.FormatInt(s.ID, 10)
	}
	if ride.Elapsed <= 0 {
		ride.Elapsed = seconds(s.Duration)
	}
	if ride.Moving <= 0 {
		ride.Moving = seconds(s.Duration)
	}
	return ride
}

func seconds(v float64) time.Duration {
	if v <= 0 || v > (365*24*time.Hour).Seconds() {
		return 0
	}
	return time.Duration(v * float64(time.Second))
}

func wholeNumber(v *float64) *int32 {
	if v == nil || *v < 0 || *v > 1<<31-1 {
		return nil
	}
	n := int32(*v)
	return &n
}

func (c *Client) Original(ctx context.Context, token, id string) (port.UploadInput, error) {
	if n, err := strconv.ParseInt(id, 10, 64); err != nil || n <= 0 || strconv.FormatInt(n, 10) != id {
		return port.UploadInput{}, port.ErrGarminInvalidRide
	}
	body, err := c.request(ctx, http.MethodGet, filePath+id, "Bearer "+token,
		"application/zip, application/x-zip-compressed, application/octet-stream", port.MaxUploadBytes+(1<<20), true)
	if err != nil {
		return port.UploadInput{}, err
	}
	if len(body) >= 12 && string(body[8:12]) == ".FIT" {
		if len(body) > port.MaxUploadBytes {
			return port.UploadInput{}, port.ErrGarminNoFile
		}
		return port.UploadInput{Filename: id + ".fit", Raw: body}, nil
	}
	return readOriginal(body, id)
}

func readOriginal(body []byte, id string) (port.UploadInput, error) {
	z, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		return port.UploadInput{}, port.ErrGarminNoFile
	}
	// Prefer the device file. Older imported activities can have a TCX or GPX
	// as their original file. Read in memory without extracting ZIP paths.
	for _, ext := range []string{".fit", ".tcx", ".gpx"} {
		for _, file := range z.File {
			if !file.Mode().IsRegular() || !strings.EqualFold(path.Ext(file.Name), ext) ||
				file.UncompressedSize64 == 0 || file.UncompressedSize64 > uint64(port.MaxUploadBytes) {
				continue
			}
			src, err := file.Open()
			if err != nil {
				continue
			}
			raw, err := io.ReadAll(io.LimitReader(src, port.MaxUploadBytes+1))
			_ = src.Close()
			if err == nil && len(raw) <= port.MaxUploadBytes {
				return port.UploadInput{Filename: id + ext, Raw: raw}, nil
			}
		}
	}
	return port.UploadInput{}, port.ErrGarminNoFile
}

func (c *Client) request(ctx context.Context, method, endpoint, auth, accept string, maxBytes int64, download bool) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.base+endpoint, nil)
	if err != nil {
		return nil, errors.New("could not prepare Garmin request")
	}
	req.Header.Set("Authorization", auth)
	req.Header.Set("Accept", accept)
	res, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, errors.New("Garmin Connect is unreachable")
	}
	defer res.Body.Close()
	switch {
	case res.StatusCode == http.StatusUnauthorized, res.StatusCode == http.StatusForbidden,
		method == http.MethodPost && res.StatusCode == http.StatusBadRequest:
		return nil, port.ErrGarminUnauthorized
	case res.StatusCode == http.StatusTooManyRequests:
		return nil, &port.GarminRateLimit{RetryAfter: retryAfter(res.Header.Get("Retry-After"), time.Now())}
	case download && (res.StatusCode == http.StatusNotFound || res.StatusCode == http.StatusGone || res.StatusCode == http.StatusNoContent):
		return nil, port.ErrGarminNoFile
	case res.StatusCode != http.StatusOK:
		// Error bodies can contain sensitive upstream data.
		return nil, fmt.Errorf("Garmin returned HTTP %d", res.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, maxBytes+1))
	if err != nil {
		return nil, errors.New("could not read Garmin response")
	}
	if int64(len(body)) > maxBytes {
		if download {
			return nil, port.ErrGarminNoFile
		}
		return nil, errors.New("Garmin response exceeded the size limit")
	}
	if download && len(body) == 0 {
		return nil, port.ErrGarminNoFile
	}
	return body, nil
}

func retryAfter(value string, now time.Time) time.Duration {
	if seconds, err := strconv.ParseUint(value, 10, 32); err == nil {
		return time.Duration(seconds) * time.Second
	}
	if retry, err := http.ParseTime(value); err == nil {
		return max(0, retry.Sub(now))
	}
	return 0
}
