package hw

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const GiB = 1 << 30

type Device struct {
	ID    string
	Name  string
	Total int64
	Free  int64
}

func (d Device) Short() string {
	n := regexp.MustCompile(`\s*\([^)]*\)`).ReplaceAllString(d.Name, "")
	for _, p := range []string{"AMD ", "Radeon ", "NVIDIA ", "GeForce ", "Intel(R) ", "Arc(TM) "} {
		n = strings.TrimPrefix(n, p)
	}
	return strings.TrimSpace(n)
}

type Hardware struct {
	Installed bool
	Version   string
	Devices   []Device
	RAM       int64
}

func (h Hardware) Primary() (Device, bool) {
	if len(h.Devices) == 0 {
		return Device{}, false
	}
	return h.Devices[0], true
}

func (h Hardware) VRAM(id string) int64 {
	for _, d := range h.Devices {
		if id == "" || d.ID == id {
			return d.Total
		}
	}
	return 0
}

var (
	deviceRe  = regexp.MustCompile(`^\s*(\S+): (.+) \((\d+) MiB, (\d+) MiB free\)\s*$`)
	versionRe = regexp.MustCompile(`build (\d+)`)
)

func Detect() Hardware {
	h := Hardware{RAM: availableRAM()}
	if _, err := exec.LookPath("llama-server"); err != nil {
		return h
	}
	h.Installed = true
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if out, err := exec.CommandContext(ctx, "llama-server", "--version").CombinedOutput(); err == nil || len(out) > 0 {
		if m := versionRe.FindStringSubmatch(string(out)); m != nil {
			h.Version = "b" + m[1]
		}
	}
	out, _ := exec.CommandContext(ctx, "llama-server", "--list-devices").CombinedOutput()
	sc := bufio.NewScanner(strings.NewReader(string(out)))
	for sc.Scan() {
		if m := deviceRe.FindStringSubmatch(sc.Text()); m != nil {
			total, _ := strconv.ParseInt(m[3], 10, 64)
			free, _ := strconv.ParseInt(m[4], 10, 64)
			h.Devices = append(h.Devices, Device{ID: m[1], Name: m[2], Total: total << 20, Free: free << 20})
		}
	}
	fmt.Fprintf(os.Stderr, "detected %d devices\n", len(h.Devices))
	return h
}

func availableRAM() int64 {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if rest, ok := strings.CutPrefix(sc.Text(), "MemAvailable:"); ok {
			kb, _ := strconv.ParseInt(strings.Fields(rest)[0], 10, 64)
			return kb << 10
		}
	}
	return 0
}
