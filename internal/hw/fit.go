package hw

import (
	"slices"
	"strings"
)

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
	fits := func(i int) bool { return vram > 0 && float64(sizes[i]) <= 0.7*float64(vram) }
	k := slices.IndexFunc(names, func(n string) bool { return strings.TrimPrefix(n, "UD-") == "Q4_K_M" })
	if k < 0 {
		k = slices.IndexFunc(names, func(n string) bool { return strings.Contains(n, "Q4") })
	}
	best := -1
	for i := range names {
		if fits(i) {
			best = i
		}
	}
	switch {
	case k >= 0 && (vram == 0 || fits(k)):
		return k
	case best >= 0 && (k < 0 || float64(sizes[best]) >= 0.75*float64(sizes[k])):
		return best
	case k >= 0:
		return k
	case vram > 0:
		return 0
	}
	return len(names) / 2
}
