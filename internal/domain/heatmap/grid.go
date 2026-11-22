package heatmap

import "math"

// rasterize intersects the leg with each grid line, distributing distance
// and time by the fraction of the original leg in each cell.
func rasterize(start Point, dx, dy float64, axis Point, id uint8, level Level, add func(Key, float64, float64, float64, float64, float64)) {
	size := grid[level]
	for startFraction := 0.0; startFraction < 1; {
		endFraction := 1.0
		endX := start.X + dx
		if endX > WorldHalf {
			endFraction = (WorldHalf - start.X) / dx
		}
		if endX < -WorldHalf {
			endFraction = (-WorldHalf - start.X) / dx
		}
		walkGrid(start, dx, dy, axis, id, size.along, size.across, startFraction, endFraction, add)
		if endFraction >= 1 {
			break
		}
		start.X -= math.Copysign(2*WorldHalf, dx)
		startFraction = endFraction
	}
}

func walkGrid(start Point, dx, dy float64, axis Point, id uint8, along, across, from, to float64,
	add func(Key, float64, float64, float64, float64, float64)) {
	u, v := start.X*axis.X+start.Y*axis.Y, -start.X*axis.Y+start.Y*axis.X
	du, dv := dx*axis.X+dy*axis.Y, -dx*axis.Y+dy*axis.X
	for from < to-1e-10 {
		probe := math.Min(from+1e-9, (from+to)/2)
		i, j := math.Floor((u+du*probe)/along), math.Floor((v+dv*probe)/across)
		next := min(to, nextGridCross(u, du, along, i), nextGridCross(v, dv, across, j))
		if next <= from {
			next = probe
		}
		add(Key{int32(i), int32(j), id}, u+du*from, u+du*next, v+dv*(from+next)/2, from, next)
		from = next
	}
}

func nextGridCross(start, delta, size, index float64) float64 {
	if delta > 0 {
		return ((index+1)*size - start) / delta
	}
	if delta < 0 {
		return (index*size - start) / delta
	}
	return math.Inf(1)
}
