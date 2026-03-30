//go:build windows

package doctor

import "errors"

func freeSpaceMB(_ string) (uint64, error) {
	return 0, errors.New("not implemented on windows")
}
