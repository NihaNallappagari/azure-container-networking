package endpointmanager

import (
	"context"

	"github.com/Azure/azure-container-networking/cns"
	"github.com/Azure/azure-container-networking/cns/hnsclient"
	"github.com/Azure/azure-container-networking/cns/logger"
	"github.com/pkg/errors"

	"golang.org/x/sys/windows/registry"
)

// ReleaseIPs implements an Interface in fsnotify for async delete of the HNS endpoint and IP addresses
func (em *EndpointManager) ReleaseIPs(ctx context.Context, ipconfigreq cns.IPConfigsRequest) error {
	logger.Printf("deleting HNS Endpoint asynchronously")
	// remove HNS endpoint
	if err := em.deleteEndpoint(ctx, ipconfigreq.InfraContainerID); err != nil {
		logger.Errorf("failed to remove HNS endpoint %s", err.Error())
	}
	return errors.Wrap(em.cli.ReleaseIPs(ctx, ipconfigreq), "failed to release IP from CNS")
}

func SetTestRegistryKey() {
	logger.Printf("Setting test registry key for infra container ID")
	key, _, err := registry.CreateKey(registry.LOCAL_MACHINE, `SOFTWARE\test`, registry.SET_VALUE)
	if err != nil {
		logger.Printf("Failed to open registry key: %v", err)
		return
	}
	defer key.Close()
	err = key.SetStringValue("infraid", "test infra nc id")
	if err != nil {
		logger.Printf("Failed to set registry value: %v", err)
	}
}

func GetTestRegistryKey() {
	logger.Printf("Reading test registry key for infra container ID")
	key, err := registry.OpenKey(registry.LOCAL_MACHINE, `SOFTWARE\test`, registry.QUERY_VALUE)
	if err != nil {
		logger.Printf("Failed to open registry key: %v", err)
		return
	}
	defer key.Close()

	val, _, err := key.GetStringValue("infraid")
	if err != nil {
		logger.Printf("Failed to read registry value: %v", err)
		return
	}

	logger.Printf("Registry value read: infraid = %s", val)
}

// deleteEndpoint API to get the state and then remove assiciated HNS
func (em *EndpointManager) deleteEndpoint(ctx context.Context, containerid string) error {
	endpointResponse, err := em.cli.GetEndpoint(ctx, containerid)
	if err != nil {
		return errors.Wrap(err, "failed to read the endpoint from CNS state")
	}
	for _, ipInfo := range endpointResponse.EndpointInfo.IfnameToIPMap {
		hnsEndpointID := ipInfo.HnsEndpointID
		// we need to get the HNSENdpoint via the IP address if the HNSEndpointID is not present in the statefile
		if ipInfo.HnsEndpointID == "" {
			if hnsEndpointID, err = hnsclient.GetHNSEndpointbyIP(ipInfo.IPv4, ipInfo.IPv6); err != nil {
				return errors.Wrap(err, "failed to find HNS endpoint with id")
			}
		}
		logger.Printf("deleting HNS Endpoint with id %v", hnsEndpointID)
		if err := hnsclient.DeleteHNSEndpointbyID(hnsEndpointID); err != nil {
			return errors.Wrap(err, "failed to delete HNS endpoint with id "+ipInfo.HnsEndpointID)
		}
	}
	return nil
}
