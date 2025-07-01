// Copyright 2024 Microsoft. All rights reserved.
// MIT License

package imds_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/Azure/azure-container-networking/cns/imds"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetVMUniqueID(t *testing.T) {
	computeMetadata, err := os.ReadFile("testdata/computeMetadata.json")
	require.NoError(t, err, "error reading testdata compute metadata file")

	mockIMDSServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// request header "Metadata: true" must be present
		metadataHeader := r.Header.Get("Metadata")
		assert.Equal(t, "true", metadataHeader)

		// query params should include apiversion and json format
		apiVersion := r.URL.Query().Get("api-version")
		assert.Equal(t, "2021-01-01", apiVersion)
		format := r.URL.Query().Get("format")
		assert.Equal(t, "json", format)
		w.WriteHeader(http.StatusOK)
		_, writeErr := w.Write(computeMetadata)
		require.NoError(t, writeErr, "error writing response")
	}))
	defer mockIMDSServer.Close()

	imdsClient := imds.NewClient(imds.Endpoint(mockIMDSServer.URL))
	vmUniqueID, err := imdsClient.GetVMUniqueID(context.Background())
	require.NoError(t, err, "error querying testserver")

	require.Equal(t, "55b8499d-9b42-4f85-843f-24ff69f4a643", vmUniqueID)
}

func TestGetVMUniqueIDInvalidEndpoint(t *testing.T) {
	imdsClient := imds.NewClient(imds.Endpoint(string([]byte{0x7f})), imds.RetryAttempts(1))
	_, err := imdsClient.GetVMUniqueID(context.Background())
	require.Error(t, err, "expected invalid path")
}

func TestIMDSInternalServerError(t *testing.T) {
	mockIMDSServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// request header "Metadata: true" must be present
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer mockIMDSServer.Close()

	imdsClient := imds.NewClient(imds.Endpoint(mockIMDSServer.URL), imds.RetryAttempts(1))

	_, err := imdsClient.GetVMUniqueID(context.Background())
	require.ErrorIs(t, err, imds.ErrUnexpectedStatusCode, "expected internal server error")
}

func TestIMDSInvalidJSON(t *testing.T) {
	mockIMDSServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, err := w.Write([]byte("not json"))
		require.NoError(t, err)
	}))
	defer mockIMDSServer.Close()

	imdsClient := imds.NewClient(imds.Endpoint(mockIMDSServer.URL), imds.RetryAttempts(1))

	_, err := imdsClient.GetVMUniqueID(context.Background())
	require.Error(t, err, "expected json decoding error")
}

func TestInvalidVMUniqueID(t *testing.T) {
	computeMetadata, err := os.ReadFile("testdata/invalidComputeMetadata.json")
	require.NoError(t, err, "error reading testdata compute metadata file")

	mockIMDSServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// request header "Metadata: true" must be present
		metadataHeader := r.Header.Get("Metadata")
		assert.Equal(t, "true", metadataHeader)

		// query params should include apiversion and json format
		apiVersion := r.URL.Query().Get("api-version")
		assert.Equal(t, "2021-01-01", apiVersion)
		format := r.URL.Query().Get("format")
		assert.Equal(t, "json", format)
		w.WriteHeader(http.StatusOK)
		_, writeErr := w.Write(computeMetadata)
		require.NoError(t, writeErr, "error writing response")
	}))
	defer mockIMDSServer.Close()

	imdsClient := imds.NewClient(imds.Endpoint(mockIMDSServer.URL))
	vmUniqueID, err := imdsClient.GetVMUniqueID(context.Background())
	require.Error(t, err, "error querying testserver")
	require.Equal(t, "", vmUniqueID)
}

func TestGetNCVersionsFromIMDS(t *testing.T) {
	networkMetadata := []byte(`{
		"interface": [
			{
				"macAddress": "00:0D:3A:12:34:56",
				"ncVersion": "1",
				"ncId": "nc-12345-67890"
			},
			{
				"macAddress": "00:0D:3A:78:90:AB",
				"ncVersion": "2",
				"ncId": "nc-54321-09876"
			},
			{
				"macAddress": "00:0D:3A:CD:EF:12",
				"ncVersion": "",
				"ncId": "nc-abcdef-123456"
			}
		]
	}`)

	mockIMDSServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// request header "Metadata: true" must be present
		metadataHeader := r.Header.Get("Metadata")
		assert.Equal(t, "true", metadataHeader)

		// verify path is network metadata
		assert.Contains(t, r.URL.Path, "/metadata/instance/network")

		// query params should include apiversion and json format
		apiVersion := r.URL.Query().Get("api-version")
		assert.Equal(t, "2021-01-01", apiVersion)
		format := r.URL.Query().Get("format")
		assert.Equal(t, "json", format)

		w.WriteHeader(http.StatusOK)
		_, writeErr := w.Write(networkMetadata)
		require.NoError(t, writeErr, "error writing response")
	}))
	defer mockIMDSServer.Close()

	imdsClient := imds.NewClient(imds.Endpoint(mockIMDSServer.URL))
	ncVersions, err := imdsClient.GetNCVersionsFromIMDS(context.Background())
	require.NoError(t, err, "error querying testserver")

	expectedNCVersions := map[string]string{
		"nc-12345-67890":   "1",
		"nc-54321-09876":   "2",
		"nc-abcdef-123456": "", // empty version
	}
	require.Equal(t, expectedNCVersions, ncVersions)
}

func TestGetNCVersionsFromIMDSInvalidEndpoint(t *testing.T) {
	imdsClient := imds.NewClient(imds.Endpoint(string([]byte{0x7f})), imds.RetryAttempts(1))
	_, err := imdsClient.GetNCVersionsFromIMDS(context.Background())
	require.Error(t, err, "expected invalid path")
}

func TestGetNCVersionsFromIMDSServerError(t *testing.T) {
	mockIMDSServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer mockIMDSServer.Close()

	imdsClient := imds.NewClient(imds.Endpoint(mockIMDSServer.URL), imds.RetryAttempts(1))
	_, err := imdsClient.GetNCVersionsFromIMDS(context.Background())
	require.ErrorIs(t, err, imds.ErrUnexpectedStatusCode, "expected internal server error")
}

func TestGetNCVersionsFromIMDSInvalidJSON(t *testing.T) {
	mockIMDSServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, err := w.Write([]byte("not json"))
		require.NoError(t, err)
	}))
	defer mockIMDSServer.Close()

	imdsClient := imds.NewClient(imds.Endpoint(mockIMDSServer.URL), imds.RetryAttempts(1))
	_, err := imdsClient.GetNCVersionsFromIMDS(context.Background())
	require.Error(t, err, "expected json decoding error")
}

func TestGetNCVersionsFromIMDSNoInterfaces(t *testing.T) {
	emptyNetworkMetadata := []byte(`{
		"interface": []
	}`)

	mockIMDSServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		metadataHeader := r.Header.Get("Metadata")
		assert.Equal(t, "true", metadataHeader)

		w.WriteHeader(http.StatusOK)
		_, writeErr := w.Write(emptyNetworkMetadata)
		require.NoError(t, writeErr, "error writing response")
	}))
	defer mockIMDSServer.Close()

	imdsClient := imds.NewClient(imds.Endpoint(mockIMDSServer.URL))
	ncVersions, err := imdsClient.GetNCVersionsFromIMDS(context.Background())
	require.NoError(t, err, "error querying testserver")
	require.Empty(t, ncVersions, "expected empty NC versions map")
}

func TestGetNCVersionsFromIMDSNoNCIDs(t *testing.T) {
	networkMetadataNoNC := []byte(`{
		"interface": [
			{
				"macAddress": "00:0D:3A:12:34:56",
				"ipv4": {
					"ipAddress": [
						{
							"privateIpAddress": "10.0.0.4",
							"publicIpAddress": ""
						}
					]
				}
			},
			{
				"macAddress": "00:0D:3A:78:90:AB",
				"ipv4": {
					"ipAddress": [
						{
							"privateIpAddress": "10.0.1.4",
							"publicIpAddress": ""
						}
					]
				}
			}
		]
	}`)

	mockIMDSServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		metadataHeader := r.Header.Get("Metadata")
		assert.Equal(t, "true", metadataHeader)

		w.WriteHeader(http.StatusOK)
		_, writeErr := w.Write(networkMetadataNoNC)
		require.NoError(t, writeErr, "error writing response")
	}))
	defer mockIMDSServer.Close()

	imdsClient := imds.NewClient(imds.Endpoint(mockIMDSServer.URL))
	ncVersions, err := imdsClient.GetNCVersionsFromIMDS(context.Background())
	require.NoError(t, err, "error querying testserver")
	require.Empty(t, ncVersions, "expected empty NC versions map when no NC IDs present")
}

func TestGetNCVersionsFromIMDSWithRetries(t *testing.T) {
	networkMetadata := []byte(`{
		"interface": [
			{
				"macAddress": "00:0D:3A:12:34:56",
				"ncVersion": "1",
				"ncId": "nc-12345-67890"
			},
			{
				"macAddress": "00:0D:3A:78:90:AB",
				"ncVersion": "2",
				"ncId": "nc-54321-09876"
			},
			{
				"macAddress": "00:0D:3A:CD:EF:12",
				"ncVersion": "",
				"ncId": "nc-abcdef-123456"
			}
		]
	}`)

	callCount := 0
	mockIMDSServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount < 3 {
			// Fail the first two requests
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		// Succeed on the third request
		metadataHeader := r.Header.Get("Metadata")
		assert.Equal(t, "true", metadataHeader)

		w.WriteHeader(http.StatusOK)
		_, writeErr := w.Write(networkMetadata)
		require.NoError(t, writeErr, "error writing response")
	}))
	defer mockIMDSServer.Close()

	imdsClient := imds.NewClient(imds.Endpoint(mockIMDSServer.URL), imds.RetryAttempts(3))
	ncVersions, err := imdsClient.GetNCVersionsFromIMDS(context.Background())
	require.NoError(t, err, "error querying testserver after retries")
	require.Len(t, ncVersions, 3, "expected 3 NC versions")
	require.Equal(t, 3, callCount, "expected 3 calls due to retries")
}
