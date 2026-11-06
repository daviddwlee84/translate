// Package hostinfo reads the memory available to a local model and suggests a
// model size. It uses no cgo, so the release targets stay cross-compilable.
package hostinfo

import (
	"context"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// GB is the unit recommendation thresholds are expressed in.
const GB uint64 = 1 << 30

// Host describes the memory a local model can use. A zero value means unknown.
type Host struct {
	RAM     uint64
	VRAM    uint64 // largest NVIDIA GPU, if any
	Unified bool   // RAM doubles as GPU memory (Apple Silicon)
}

// Detect reads the current host. Failures leave fields at zero.
func Detect(ctx context.Context) Host {
	ram, err := totalMemory()
	if err != nil {
		ram = 0
	}
	return Host{
		RAM:     ram,
		VRAM:    nvidiaVRAM(ctx),
		Unified: runtime.GOOS == "darwin" && runtime.GOARCH == "arm64",
	}
}

// nvidiaVRAM asks nvidia-smi for the largest GPU's memory, or 0 if unavailable.
func nvidiaVRAM(ctx context.Context) uint64 {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "nvidia-smi", "--query-gpu=memory.total", "--format=csv,noheader,nounits").Output()
	if err != nil {
		return 0
	}
	return parseNvidiaSMI(string(out))
}

// parseNvidiaSMI reads one MiB value per GPU and returns the largest in bytes.
func parseNvidiaSMI(out string) uint64 {
	var largest uint64
	for _, line := range strings.Split(out, "\n") {
		mib, err := strconv.ParseUint(strings.TrimSpace(line), 10, 64)
		if err == nil && mib<<20 > largest {
			largest = mib << 20
		}
	}
	return largest
}

// ModelSuggestion is a model tag with its download size.
type ModelSuggestion struct {
	Tag        string
	DownloadGB float64
}

// RecommendTranslateGemma picks the largest size that should run comfortably on
// host, or ok=false when memory is unknown. The thresholds are this project's
// conservative heuristic, not a Google specification: GPU memory decides, so a
// host with neither unified memory nor a detected GPU gets the smallest model
// because CPU inference on larger ones is slow regardless of RAM.
func RecommendTranslateGemma(host Host) (suggestion ModelSuggestion, ok bool) {
	small := ModelSuggestion{"translategemma:4b", 3.3}
	medium := ModelSuggestion{"translategemma:12b", 8.1}
	large := ModelSuggestion{"translategemma:27b", 17}

	switch {
	case host.VRAM > 0:
		switch {
		case host.VRAM >= 20*GB:
			return large, true
		case host.VRAM >= 10*GB:
			return medium, true
		}
		return small, true
	case host.RAM == 0:
		return ModelSuggestion{}, false
	case !host.Unified:
		return small, true
	case host.RAM >= 32*GB:
		return large, true
	case host.RAM >= 16*GB:
		return medium, true
	}
	return small, true
}
