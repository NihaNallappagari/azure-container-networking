// Copyright 2024 Microsoft. All rights reserved.
// MIT License

package imds

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"

	"github.com/Azure/azure-container-networking/cns/logger"
	"github.com/avast/retry-go/v4"
	"github.com/pkg/errors"
)

// see docs for IMDS here: https://learn.microsoft.com/en-us/azure/virtual-machines/instance-metadata-service

// Client returns metadata about the VM by querying IMDS
type Client struct {
	cli    *http.Client
	config clientConfig
}

// clientConfig holds config options for a Client
type clientConfig struct {
	endpoint      string
	retryAttempts uint
}

type ClientOption func(*clientConfig)

// Endpoint overrides the default endpoint for a Client
func Endpoint(endpoint string) ClientOption {
	return func(c *clientConfig) {
		c.endpoint = endpoint
	}
}

// RetryAttempts overrides the default retry attempts for the client
func RetryAttempts(attempts uint) ClientOption {
	return func(c *clientConfig) {
		c.retryAttempts = attempts
	}
}

const (
	vmUniqueIDProperty    = "vmId"
	imdsComputePath       = "/metadata/instance/compute"
	imdsNetworkPath       = "/metadata/instance/network"
	imdsComputeAPIVersion = "api-version=2021-01-01"
	imdsFormatJSON        = "format=json"
	metadataHeaderKey     = "Metadata"
	metadataHeaderValue   = "true"
	defaultRetryAttempts  = 3
	defaultIMDSEndpoint   = "http://169.254.169.254"
	ncVersion             = "ncVersion"
)

var (
	ErrVMUniqueIDNotFound   = errors.New("vm unique ID not found")
	ErrUnexpectedStatusCode = errors.New("imds returned an unexpected status code")
)

// NewClient creates a new imds client
func NewClient(opts ...ClientOption) *Client {
	config := clientConfig{
		endpoint:      defaultIMDSEndpoint,
		retryAttempts: defaultRetryAttempts,
	}

	for _, o := range opts {
		o(&config)
	}

	return &Client{
		cli:    &http.Client{},
		config: config,
	}
}

func (c *Client) GetVMUniqueID(ctx context.Context) (string, error) {
	var vmUniqueID string
	err := retry.Do(func() error {
		computeDoc, err := c.getInstanceMetadata(ctx, imdsComputePath)
		if err != nil {
			return errors.Wrap(err, "error getting IMDS compute metadata")
		}
		vmUniqueIDUntyped := computeDoc[vmUniqueIDProperty]
		var ok bool
		vmUniqueID, ok = vmUniqueIDUntyped.(string)
		if !ok {
			return errors.New("unable to parse IMDS compute metadata, vmId property is not a string")
		}
		return nil
	}, retry.Context(ctx), retry.Attempts(c.config.retryAttempts), retry.DelayType(retry.BackOffDelay))
	if err != nil {
		return "", errors.Wrap(err, "exhausted retries querying IMDS compute metadata")
	}

	if vmUniqueID == "" {
		return "", ErrVMUniqueIDNotFound
	}

	return vmUniqueID, nil
}

func (c *Client) GetNCVersionsFromIMDS(ctx context.Context) (map[string]string, error) {
	networkMetadata, err := c.getInstanceMetadata(ctx, imdsNetworkPath)
	if err != nil {
		return nil, errors.Wrap(err, "error getting IMDS network metadata")
	}

	// Try to parse the network metadata as the expected structure
	var networkData NetworkMetadata

	// Convert the map to JSON and back to properly unmarshal into struct
	jsonData, err := json.Marshal(networkMetadata)
	if err != nil {
		return nil, errors.Wrap(err, "error marshaling network metadata")
	}
	logger.Printf("IMDS network metadata JSON: %s", string(jsonData))

	if err := json.Unmarshal(jsonData, &networkData); err != nil {
		return nil, errors.Wrap(err, "error unmarshaling network metadata")
	}
	logger.Printf("Parsed IMDS network metadata: %+v", networkData)
	// Collect all NC IDs and their versions (empty string if version doesn't exist)
	ncVersions := make(map[string]string)
	for _, iface := range networkData.Interface {
		if iface.NcId != "" {
			// Include NC ID even if NCVersion is empty
			ncVersions[iface.NcId] = iface.NCVersion
		}
	}

	return ncVersions, nil
}

// GetNCVersionByID returns the NC version for a specific NC ID, or empty string if not found
func (c *Client) GetNCVersionByID(ctx context.Context, ncID string) (string, error) {
	ncVersions, err := c.GetNCVersionsFromIMDS(ctx)
	if err != nil {
		return "", err
	}

	// Return the version for the specific NC ID, or empty string if not found
	version, exists := ncVersions[ncID]
	if !exists {
		return "", nil // NC ID not found, return empty string
	}

	return version, nil
}

func (c *Client) getInstanceMetadata(ctx context.Context, imdsComputePath string) (map[string]any, error) {
	imdsComputeURL, err := url.JoinPath(c.config.endpoint, imdsComputePath)
	if err != nil {
		return nil, errors.Wrap(err, "unable to build path to IMDS metadata for path "+imdsComputePath)
	}
	imdsComputeURL = imdsComputeURL + "?" + imdsComputeAPIVersion + "&" + imdsFormatJSON

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, imdsComputeURL, http.NoBody)
	if err != nil {
		return nil, errors.Wrap(err, "error building IMDS http request")
	}

	// IMDS requires the "Metadata: true" header
	req.Header.Add(metadataHeaderKey, metadataHeaderValue)
	resp, err := c.cli.Do(req)
	if err != nil {
		return nil, errors.Wrap(err, "error querying IMDS")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, errors.Wrapf(ErrUnexpectedStatusCode, "unexpected status code %d", resp.StatusCode)
	}

	var m map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		return nil, errors.Wrap(err, "error decoding IMDS response as json")
	}

	return m, nil
}

// NetworkInterface represents a network interface from IMDS
type NetworkInterface struct {
	MacAddress string `json:"macAddress"`
	NCVersion  string `json:"ncVersion"`
	NcId       string `json:"ncId,omitempty"`
}

// type NetworkInterface struct {
// 	IPv4 struct {
// 		IPAddress []struct {
// 			PrivateIPAddress string `json:"privateIpAddress"`
// 			PublicIPAddress  string `json:"publicIpAddress"`
// 		} `json:"ipAddress"`
// 		Subnet []struct {
// 			Address string `json:"address"`
// 			Prefix  string `json:"prefix"`
// 		} `json:"subnet"`
// 	} `json:"ipv4"`
// 	IPv6 struct {
// 		IPAddress []interface{} `json:"ipAddress"`
// 	} `json:"ipv6"`
// 	MacAddress string `json:"macAddress"`
// 	NCVersion  string `json:"ncVersion"`
// 	NcId       string `json:"ncId,omitempty"`
// }

// NetworkMetadata represents the network metadata from IMDS
type NetworkMetadata struct {
	Interface []NetworkInterface `json:"interface"`
}
