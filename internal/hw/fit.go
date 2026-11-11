package hw

type Fit int

const (
	Unknown Fit = iota
	Fits
	Tight
	Spills
	TooBig
)

func (f Fit) Label() string {
	switch f {
	case Fits:
		return "fits"
	case Tight:
		return "tight"
	case Spills:
		return "partly on CPU"
	case TooBig:
		return "too big"
	}
	return ""
}

func Assess(size, vram, ram int64) Fit {
	switch {
	case vram == 0:
		return Unknown
	case float64(size) <= 0.7*float64(vram):
		return Fits
	case size <= vram:
		return Tight
	case ram > 0 && float64(size) > float64(vram)+0.9*float64(ram):
		return TooBig
	}
	return Spills
}

func Pick(names []string, sizes []int64, vram int64) int {
	if len(names) == 0 {
		return -1
	}
	k, best := -1, -1
	for i, n := range names {
		if n == "Q4_K_M" {
			k = i
		}
		if vram > 0 && float64(sizes[i]) <= 0.7*float64(vram) {
			best = i
		}
	}
	switch {
	case k >= 0 && (vram == 0 || float64(sizes[k]) <= 0.7*float64(vram)):
		return k
	case best >= 0 && (k < 0 || float64(sizes[best]) >= 0.75*float64(sizes[k])):
		return best
	case k >= 0:
		return k
	}
	return 0
}
