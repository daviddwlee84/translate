//go:build !darwin && !linux && !windows

package hostinfo

import "errors"

func totalMemory() (uint64, error) {
	return 0, errors.ErrUnsupported
}
