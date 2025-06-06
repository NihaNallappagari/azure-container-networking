//go:build !windows
// +build !windows

package endpointmanager

import (
	"github.com/Azure/azure-container-networking/cns/logger"
)

func SetTestRegistryKey() {
	logger.Printf("SetTestRegistryKey is a no-op on non-Windows platforms")
}

func GetTestRegistryKey() {
	logger.Printf("GetTestRegistryKey is a no-op on non-Windows platforms")
}
