package besteffort

import "github.com/opencairn-org/cairn-core/internal/domain"

type downhillProfile struct {
	points    []cyclingPoint
	altitude  []float64
	assistedM []float64
}

func verifiedElevation(sample domain.StreamSample) bool {
	return sample.Latitude != nil && sample.Longitude != nil && domain.ValidRecordingAltitude(sample)
}

// Smooth each altitude over a centred 50 m moving window. Mark distance
// ridden on descents of at least 3% over a further 50 m, then check both
// that distance and the effort's net grade. GPS/elevation gaps split
// the input before this profile is created, so a window cannot bridge one.
func newDownhillProfile(samples []domain.StreamSample, points []cyclingPoint) downhillProfile {
	profile := downhillProfile{points, downhillAltitude(samples, points), make([]float64, len(samples))}
	left, right := 0, 0
	for i := 1; i < len(points); i++ {
		centre := (points[i-1].metres + points[i].metres) / 2
		for left < i-1 && points[left+1].metres <= centre-25 {
			left++
		}
		for right < len(points)-1 && points[right].metres < centre+25 {
			right++
		}
		profile.assistedM[i] = profile.assistedM[i-1]
		span := points[right].metres - points[left].metres
		if span >= 35 && profile.altitude[left]-profile.altitude[right] >= .03*span {
			profile.assistedM[i] += points[i].metres - points[i-1].metres
		}
	}
	return profile
}

func downhillAltitude(samples []domain.StreamSample, points []cyclingPoint) []float64 {
	altitude := make([]float64, len(samples))
	if points[len(points)-1].metres <= 50 {
		// A centred mean would flatten an entire short recording.
		for i, sample := range samples {
			altitude[i] = *sample.AltitudeM
		}
		return altitude
	}
	left, right := 0, 0
	sum := 0.0
	for i, point := range points {
		for right < len(points) && points[right].metres <= point.metres+25 {
			sum += *samples[right].AltitudeM
			right++
		}
		for points[left].metres < point.metres-25 {
			sum -= *samples[left].AltitudeM
			left++
		}
		altitude[i] = sum / float64(right-left)
	}
	return altitude
}

type downhillWindow struct {
	offset, elapsed, distance, drop, assisted float64
}

func interpolate(from, to, fraction float64) float64 {
	return from + (to-from)*fraction
}

// An eligible optimum can lie between samples, where a grade or descent
// limit is reached. Sweep the overlapping start and end legs in O(n).
func (p downhillProfile) fastestWindow(width float64, byDistance bool) (offset, elapsed, distance float64) {
	if width <= 0 || len(p.points) < 2 {
		return 0, 0, 0
	}
	coordinate := func(i int) float64 {
		if byDistance {
			return p.points[i].metres
		}
		return p.points[i].seconds
	}
	var best downhillWindow
	for from, to := 0, 0; from+1 < len(p.points) && to+1 < len(p.points); {
		a, b := coordinate(from), coordinate(from+1)
		c, d := coordinate(to)-width, coordinate(to+1)-width
		lo, hi := max(a, c), min(b, d)
		if a < b && c < d && lo <= hi {
			left := p.window(from, to, (lo-a)/(b-a), (lo-c)/(d-c))
			right := p.window(from, to, (hi-a)/(b-a), (hi-c)/(d-c))
			best = chooseDownhillWindow(left, right, best, byDistance)
		}
		if b <= d {
			from++
		} else {
			to++
		}
	}
	return best.offset, best.elapsed, best.distance
}

func (p downhillProfile) window(from, to int, f, t float64) downhillWindow {
	start := interpolate(p.points[from].seconds, p.points[from+1].seconds, f)
	end := interpolate(p.points[to].seconds, p.points[to+1].seconds, t)
	return downhillWindow{
		offset: start, elapsed: end - start,
		distance: interpolate(p.points[to].metres, p.points[to+1].metres, t) -
			interpolate(p.points[from].metres, p.points[from+1].metres, f),
		drop: interpolate(p.altitude[from], p.altitude[from+1], f) -
			interpolate(p.altitude[to], p.altitude[to+1], t),
		assisted: interpolate(p.assistedM[to], p.assistedM[to+1], t) -
			interpolate(p.assistedM[from], p.assistedM[from+1], f),
	}
}

func chooseDownhillWindow(left, right, best downhillWindow, byDistance bool) downhillWindow {
	minimum, maximum := 0.0, 1.0
	clipDownhillLimit(&minimum, &maximum, .20*left.distance-left.assisted, .20*right.distance-right.assisted)
	// Net drop is allowed below 3 m OR below a 1.5% grade.
	for _, limit := range []struct{ drop, grade float64 }{{drop: 3}, {grade: .015}} {
		lower, upper := minimum, maximum
		clipDownhillLimit(&lower, &upper,
			limit.drop+limit.grade*left.distance-left.drop, limit.drop+limit.grade*right.distance-right.drop)
		if lower > upper {
			continue
		}
		// Speed and elapsed time are linear over this interval. Only the
		// endpoint giving the better value needs to be ranked.
		fraction := lower
		if (byDistance && right.elapsed < left.elapsed) || (!byDistance && right.distance > left.distance) {
			fraction = upper
		}
		span := interpolate(left.elapsed, right.elapsed, fraction)
		metres := interpolate(left.distance, right.distance, fraction)
		if span > 0 && ((byDistance && (best.elapsed == 0 || span < best.elapsed)) || (!byDistance && metres > best.distance)) {
			best = downhillWindow{offset: interpolate(left.offset, right.offset, fraction), elapsed: span, distance: metres}
		}
	}
	return best
}

// Intersect the candidate interval with the part whose margin is positive.
func clipDownhillLimit(lo, hi *float64, left, right float64) {
	const margin = 1e-8 // Stay inside the strict cutoff despite roundoff.
	switch {
	case left < margin && right < margin:
		*lo, *hi = 1, 0
	case left < margin:
		*lo = max(*lo, (margin-left)/(right-left))
	case right < margin:
		*hi = min(*hi, (margin-left)/(right-left))
	}
}
