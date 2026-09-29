// SPDX-FileCopyrightText: Contributors to the Gardener project
//
// SPDX-License-Identifier: Apache-2.0

package exporter

import (
	"fmt"
	"os"

	"sigs.k8s.io/yaml"
)

// etcdConfig is a partial representation of the etcd configuration file. Only the fields
// relevant to the exporter are declared; all other fields are ignored during parsing.
type etcdConfig struct {
	// QuotaBackendBytes mirrors the etcd "quota-backend-bytes" configuration option.
	// A pointer is used to distinguish "field absent" from "explicitly set to 0".
	QuotaBackendBytes *int64 `json:"quota-backend-bytes,omitempty"`
}

// parseQuotaBackendBytes reads the etcd configuration file at the given path and returns the configured
// quota-backend-bytes value. The boolean return value reports whether the field was present in the file.
// A missing field is not treated as an error: (0, false, nil) is returned so callers can decide how to react.
func parseQuotaBackendBytes(configFilePath string) (int64, bool, error) {
	data, err := os.ReadFile(configFilePath)
	if err != nil {
		return 0, false, fmt.Errorf("failed to read etcd config file %q: %w", configFilePath, err)
	}
	return parseQuotaBackendBytesFromData(data)
}

// parseQuotaBackendBytesFromData parses raw etcd configuration YAML/JSON bytes and extracts the
// quota-backend-bytes value. It is separated from file I/O to keep the parsing logic unit-testable.
func parseQuotaBackendBytesFromData(data []byte) (int64, bool, error) {
	var cfg etcdConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return 0, false, fmt.Errorf("failed to unmarshal etcd config: %w", err)
	}
	if cfg.QuotaBackendBytes == nil {
		return 0, false, nil
	}
	return *cfg.QuotaBackendBytes, true, nil
}
