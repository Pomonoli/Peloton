package domain

import (
	"math"
	"time"
)

// Category is an independently-gateable slice of an activity's data.
type Category string

const (
	CategorySummary     Category = "summary"      // type, title, date, distance, time, elevation, calories
	CategoryMap         Category = "map"          // GPS track (privacy-zone masked for non-owners)
	CategoryPhotos      Category = "photos"       // activity photos / attachments
	CategoryLocation    Category = "location"     // start place + start/end coords
	CategoryHR          Category = "hr"           // heart-rate stream + avg/max
	CategoryPower       Category = "power"        // power stream + avg/max/NP
	CategoryCadence     Category = "cadence"      // cadence stream
	CategoryPaceSpeed   Category = "pace_speed"   // speed/pace stream + avg/max
	CategorySegments    Category = "segments"     // segment efforts + placement
	CategoryBestEfforts Category = "best_efforts" // best-effort curves
	CategoryZones       Category = "zones"        // time-in-zone
	CategorySocial      Category = "social"       // may kudos/comment (a gate, not data)
)

// AllCategories is the canonical iteration order.
var AllCategories = []Category{
	CategorySummary, CategoryMap, CategoryPhotos, CategoryLocation, CategoryHR, CategoryPower,
	CategoryCadence, CategoryPaceSpeed, CategorySegments, CategoryBestEfforts,
	CategoryZones, CategorySocial,
}

// CategorySet is a set of allowed categories.
type CategorySet map[Category]bool

// Has reports whether the category is allowed.
func (s CategorySet) Has(c Category) bool { return s[c] }

// Slice returns the allowed categories in canonical order (for serialisation).
func (s CategorySet) Slice() []Category {
	out := make([]Category, 0, len(s))
	for _, c := range AllCategories {
		if s[c] {
			out = append(out, c)
		}
	}
	return out
}

// ActivityCategories returns the cycling data available to an authorized member.
func ActivityCategories() CategorySet {
	categories := make(CategorySet, len(AllCategories))
	for _, category := range AllCategories {
		categories[category] = true
	}
	return categories
}

// ---------------------------------------------------------------------------
// Privacy zones
// ---------------------------------------------------------------------------

// PrivacyZone is a circular area (home, work) within which an owner's GPS is
// masked from non-owner viewers.
type PrivacyZone struct {
	ID        PrivacyZoneID
	UserID    UserID
	Label     string
	Lat       float64
	Lng       float64
	RadiusM   float64
	CreatedAt time.Time
}

// Contains reports whether (lat, lng) lies within the zone's radius, using the
// haversine great-circle distance.
func (z PrivacyZone) Contains(lat, lng float64) bool {
	return haversineMeters(z.Lat, z.Lng, lat, lng) <= z.RadiusM
}

// PointInAnyZone reports whether a point falls inside any of the zones.
func PointInAnyZone(zones []PrivacyZone, lat, lng float64) bool {
	for _, z := range zones {
		if z.Contains(lat, lng) {
			return true
		}
	}
	return false
}

func haversineMeters(lat1, lng1, lat2, lng2 float64) float64 {
	const earthR = 6371000.0
	rad := func(d float64) float64 { return d * math.Pi / 180 }
	dLat := rad(lat2 - lat1)
	dLng := rad(lng2 - lng1)
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(rad(lat1))*math.Cos(rad(lat2))*math.Sin(dLng/2)*math.Sin(dLng/2)
	return earthR * 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
}
