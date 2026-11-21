package garmin

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/opencairn-org/cairn-core/internal/port"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func response(code int, body string) *http.Response {
	return &http.Response{StatusCode: code, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}

func TestPersonalAPIContract(t *testing.T) {
	expiry := time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)
	token := "header." + base64.RawURLEncoding.EncodeToString(fmt.Appendf(nil, `{"exp":%d}`, expiry.Unix())) + ".signature"
	ctx := context.Background()
	fitBytes := "\x0e\x20\x00\x00\x00\x00\x00\x00.FITexercise"
	var archive bytes.Buffer
	z := zip.NewWriter(&archive)
	for _, item := range []struct{ name, body string }{
		{"older.gpx", "<gpx/>"}, {"folder/ride.FIT", fitBytes},
	} {
		w, err := z.Create(item.name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(w, item.body); err != nil {
			t.Fatal(err)
		}
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}

	client := New()
	client.http.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Scheme != "https" || r.URL.Host != "personal.connectapi.garmin.com" {
			t.Fatalf("unexpected API URL %s", r.URL)
		}
		wantAuth := "Bearer " + token
		if r.URL.Path == tokenPath {
			wantAuth = "ApiKey fixture-key"
		}
		if r.Header.Get("Authorization") != wantAuth {
			t.Fatal("request used the wrong authentication scheme or credential")
		}
		switch r.URL.Path {
		case tokenPath:
			if r.Method != "POST" || r.Header.Get("Accept") != "*/*" {
				t.Fatalf("invalid token request %s", r.Method)
			}
			return response(http.StatusOK, token+"\n"), nil
		case profilePath:
			return response(http.StatusOK, `{"displayName":"fixture-owner","measurementSystem":"metric"}`), nil
		case searchPath:
			q := r.URL.Query()
			if q.Get("start") != "10" || q.Get("limit") != "10" || q.Get("activityType") != "cycling" || q.Get("excludeChildren") != "false" ||
				q.Get("startDate") != "2026-11-08" || q.Get("endDate") != "2026-11-22" {
				t.Fatalf("invalid cycling search %v", q)
			}
			return response(http.StatusOK, `[{"activityId":90300000001,"activityName":"Gravel ride","startTimeGMT":"2026-11-20 12:34:56.125",
				"activityType":{"typeKey":"gravel_cycling"},"duration":1800,"elapsedDuration":1900,"movingDuration":1750,
				"distance":8200,"elevationGain":120,"averageHR":143.0,"avgPower":225.0}]`), nil
		case filePath + "90300000001":
			if !strings.Contains(r.Header.Get("Accept"), "application/zip") {
				t.Fatal("binary request did not advertise ZIP")
			}
			return response(http.StatusOK, archive.String()), nil
		case filePath + "2":
			return response(http.StatusNotFound, "no original"), nil
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
			return nil, nil
		}
	})
	session, err := client.ExchangeKey(ctx, "fixture-key")
	if err != nil || session.Token != token || !session.ExpiresAt.Equal(expiry) {
		t.Fatalf("exchange failed: %v", err)
	}
	profile, err := client.ProfileHandle(ctx, session.Token)
	if err != nil || profile != "fixture-owner" {
		t.Fatalf("profile = %q, %v", profile, err)
	}
	rides, err := client.Rides(ctx, session.Token, expiry.AddDate(0, 0, -14), expiry, 10, 10)
	if err != nil || len(rides) != 1 {
		t.Fatalf("rides = %#v, %v", rides, err)
	}
	r := rides[0]
	if r.ID != "90300000001" || r.Type != "gravel_cycling" || r.StartTime.Format(time.RFC3339Nano) != "2026-11-20T12:34:56.125Z" ||
		r.Moving != 1750*time.Second || *r.DistanceM != 8200 || *r.ElevationM != 120 || *r.AvgHeartRate != 143 || *r.AvgPower != 225 {
		t.Fatalf("unexpected summary: %#v", r)
	}
	original, err := client.Original(ctx, token, r.ID)
	if err != nil || original.Filename != r.ID+".fit" || string(original.Raw) != fitBytes {
		t.Fatalf("unexpected original: %q, %v", original.Filename, err)
	}
	if _, err := client.Original(ctx, token, "2"); !errors.Is(err, port.ErrGarminNoFile) {
		t.Fatalf("missing file error = %v", err)
	}
}

func TestPersonalAPIDoesNotLeakErrorBody(t *testing.T) {
	client := New()
	for _, status := range []int{http.StatusUnauthorized, http.StatusTooManyRequests, http.StatusServiceUnavailable} {
		client.http.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
			r := response(status, "sensitive-content")
			r.Header.Set("Retry-After", "120")
			return r, nil
		})
		_, err := client.ExchangeKey(t.Context(), "fixture-key")
		if err == nil || strings.Contains(err.Error(), "sensitive-content") || strings.Contains(err.Error(), "fixture-key") {
			t.Fatalf("unsafe or absent error for HTTP %d: %v", status, err)
		}
		if status == http.StatusUnauthorized && !errors.Is(err, port.ErrGarminUnauthorized) {
			t.Fatalf("authorization error = %v", err)
		}
		if status == http.StatusTooManyRequests {
			var rate *port.GarminRateLimit
			if !errors.As(err, &rate) || rate.RetryAfter != 2*time.Minute {
				t.Fatalf("rate limit error = %v", err)
			}
		}
	}
}
