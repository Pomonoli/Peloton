package besteffort

import (
	"math"

	"github.com/opencairn-org/cairn-core/internal/domain"
)

type downhillProfile struct {
	points    []cyclingPoint
	altitude  []float64
	assistedM []float64
}

func verifiedElevation(sample domain.StreamSample) bool {
	return sample.Latitude != nil && sample.Longitude != nil && sample.AltitudeM != nil &&
		*sample.AltitudeM >= -450 && *sample.AltitudeM <= 9000
}

// Smooth each altitude over a centred 50 m moving window. Mark distance
// ridden on descents of at least 3% over a further 50 m, then check both
// that distance and the effort's net grade. GPS/elevation gaps split
// the input before this profile is created, so a window cannot bridge one.
func newDownhillProfile(samples []domain.StreamSample, points []cyclingPoint) downhillProfile {
	profile := downhillProfile{points, make([]float64, len(samples)), make([]float64, len(samples))}
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
		profile.altitude[i] = sum / float64(right-left)
	}
	left, right = 0, 0
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

func (p downhillProfile) allows(fixed, crossing, previous int, fraction float64) bool {
	at := func(values []float64) float64 {
		return values[previous] + (values[crossing]-values[previous])*fraction
	}
	from, to := p.points[fixed], p.points[previous]
	cross := p.points[crossing]
	distance := math.Abs(to.metres + (cross.metres-to.metres)*fraction - from.metres)
	sampleSpan := math.Abs(float64(previous-fixed) + float64(crossing-previous)*fraction)
	if distance < 35 || sampleSpan < 3 {
		return false
	}
	drop := p.altitude[fixed] - at(p.altitude)
	if crossing < fixed {
		drop = -drop
	}
	assisted := math.Abs(p.assistedM[fixed] - at(p.assistedM))
	return drop < max(3, distance*.015) && assisted < distance*.20
}
