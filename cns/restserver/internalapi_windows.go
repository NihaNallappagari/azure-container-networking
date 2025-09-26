package restserver

import (
	"context"
	"fmt"
	"time"

	"github.com/Azure/azure-container-networking/cns"
	"github.com/Azure/azure-container-networking/cns/logger"
	"github.com/Azure/azure-container-networking/cns/types"
	"github.com/Microsoft/hcsshim"
	"github.com/pkg/errors"
	"golang.org/x/sys/windows/registry"
)

const (
	// timeout for powershell command to return the interfaces list
	pwshTimeout             = 120 * time.Second
	hnsRegistryPath         = `SYSTEM\CurrentControlSet\Services\HNS\wcna_state\config`
	prefixOnNicRegistryPath = `SYSTEM\CurrentControlSet\Services\HNS\wcna_state\config\PrefixOnNic`
	infraNicIfName          = "eth0"
	enableSNAT              = false
)

// HNS Registry configuration values
// config := HNSRegistryConfig{
// 	PrefixOnNicEnabled: true,
// 	InfraNicMacAddress: macAddress,
// 	InfraNicIfName:     infraNicIfName,
// 	EnableSNAT:         false,
// }

var errUnsupportedAPI = errors.New("unsupported api")

type IPtablesProvider struct{}

func (*IPtablesProvider) GetIPTables() (iptablesClient, error) {
	return nil, errUnsupportedAPI
}

func (*IPtablesProvider) GetIPTablesLegacy() (iptablesLegacyClient, error) {
	return nil, errUnsupportedAPI
}

// nolint
func (service *HTTPRestService) programSNATRules(req *cns.CreateNetworkContainerRequest) (types.ResponseCode, string) {
	return types.Success, ""
}

// setVFForAccelnetNICs is used in SWIFTV2 mode to set VF on accelnet nics
func (service *HTTPRestService) setVFForAccelnetNICs() error {
	// supply the primary MAC address to HNS api
	macAddress, err := service.getPrimaryNICMACAddress()
	if err != nil {
		return err
	}
	macAddresses := []string{macAddress}
	if _, err := hcsshim.SetNnvManagementMacAddresses(macAddresses); err != nil {
		return errors.Wrap(err, "Failed to set primary NIC MAC address")
	}
	return nil
}

// getPrimaryNICMacAddress fetches the MAC address of the primary NIC on the node.
func (service *HTTPRestService) getPrimaryNICMACAddress() (string, error) {
	// Create a new context and add a timeout to it
	ctx, cancel := context.WithTimeout(context.Background(), pwshTimeout)
	defer cancel() // The cancel should be deferred so resources are cleaned up

	res, err := service.wscli.GetInterfaces(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to find primary interface info: %w", err)
	}
	var macAddress string
	for _, i := range res.Interface {
		// skip if not primary
		if !i.IsPrimary {
			continue
		}
		// skip if no subnets
		if len(i.IPSubnet) == 0 {
			continue
		}
		macAddress = i.MacAddress
	}

	if macAddress == "" {
		return "", errors.New("MAC address not found(empty) from wireserver")
	}
	return macAddress, nil
}

// setRegistryValue sets a registry value at the specified path

func (service *HTTPRestService) setPrefixOnNicEnabled(enabled bool) error {
	return service.setRegistryValue(prefixOnNicRegistryPath, "enabled", enabled)
}

func (service *HTTPRestService) setInfraNicMacAddress(macAddress string) error {
	return service.setRegistryValue(prefixOnNicRegistryPath, "infra_nic_mac_address", macAddress)
}

func (service *HTTPRestService) setInfraNicIfName(ifName string) error {
	return service.setRegistryValue(prefixOnNicRegistryPath, "infra_nic_ifname", ifName)
}

func (service *HTTPRestService) setEnableSNAT(enabled bool) error {
	return service.setRegistryValue(hnsRegistryPath, "EnableSNAT", enabled)
}

func (service *HTTPRestService) setPrefixOnNICRegistry(prefixOnNicEnabled bool, infraNicMacAddress string) error {
	if err := service.setPrefixOnNicEnabled(prefixOnNicEnabled); err != nil {
		return fmt.Errorf("failed to set PrefixOnNic enabled: %w", err)
	}

	if err := service.setInfraNicMacAddress(infraNicMacAddress); err != nil {
		return fmt.Errorf("failed to set InfraNicMacAddress: %w", err)
	}

	if err := service.setInfraNicIfName(infraNicIfName); err != nil {
		return fmt.Errorf("failed to set InfraNicIfName: %w", err)
	}

	if err := service.setEnableSNAT(enableSNAT); err != nil {
		return fmt.Errorf("failed to set EnableSNAT: %w", err)
	}

	return nil
}

func (service *HTTPRestService) setRegistryValue(registryPath, keyName string, value interface{}) error {
	key, _, err := registry.CreateKey(registry.LOCAL_MACHINE, registryPath, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("failed to create/open registry key %s: %w", registryPath, err)
	}
	defer key.Close()

	switch v := value.(type) {
	case string:
		err = key.SetStringValue(keyName, v)
	case bool:
		dwordValue := uint32(0)
		if v {
			dwordValue = 1
		}
		err = key.SetDWordValue(keyName, dwordValue)
	case uint32:
		err = key.SetDWordValue(keyName, v)
	case int:
		err = key.SetDWordValue(keyName, uint32(v))
	default:
		return fmt.Errorf("unsupported value type for registry key %s: %T", keyName, value)
	}

	if err != nil {
		return fmt.Errorf("failed to set registry value '%s': %w", keyName, err)
	}

	logger.Printf("[setRegistryValue] Set %s\\%s = %v", registryPath, keyName, value)
	return nil
}

func (service *HTTPRestService) getPrefixOnNicEnabled() (bool, error) {
	key, err := registry.OpenKey(registry.LOCAL_MACHINE, prefixOnNicRegistryPath, registry.QUERY_VALUE)
	if err != nil {
		return false, nil // Key doesn't exist, default to false
	}
	defer key.Close()

	value, _, err := key.GetIntegerValue("enabled")
	if err != nil {
		return false, nil // Value doesn't exist, default to false
	}

	return value == 1, nil
}

// func (service *HTTPRestService) getCurrentHNSRegistryConfig() (HNSRegistryConfig, error) {
//     config := HNSRegistryConfig{}

//     enabled, err := service.getPrefixOnNicEnabled()
//     if err != nil {
//         return config, fmt.Errorf("failed to read PrefixOnNic enabled: %w", err)
//     }
//     config.PrefixOnNicEnabled = enabled

//     // macAddress, _ := service.getInfraNicMacAddress() // Ignore error for optional value
//     // config.InfraNicMacAddress = macAddress

//     // enableSNAT, err := service.getEnableSNAT()
//     // if err != nil {
//     //     return config, fmt.Errorf("failed to read EnableSNAT: %w", err)
//     // }
//     // config.EnableSNAT = enableSNAT

//     // config.InfraNicIfName = infraNicIfName // This is constant

//     return config, nil
// }
