//go:build windows

package storage

import "errors"

func diskStats(_ string) (totalBytes, freeBytes uint64, err error) {
	return 0, 0, errors.New("not implemented on windows")
}
