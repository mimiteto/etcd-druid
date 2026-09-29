// SPDX-FileCopyrightText: Contributors to the Gardener project
//
// SPDX-License-Identifier: Apache-2.0

package exporter

import (
	"os"
	"path/filepath"
	"testing"

	. "github.com/onsi/gomega"
)

func TestParseQuotaBackendBytesFromData(t *testing.T) {
	tests := []struct {
		name        string
		data        string
		wantValue   int64
		wantPresent bool
		wantErr     bool
	}{
		{
			name:        "field present",
			data:        "name: etcd-main\nquota-backend-bytes: 8589934592\n",
			wantValue:   8589934592,
			wantPresent: true,
		},
		{
			name:        "field present zero",
			data:        "quota-backend-bytes: 0\n",
			wantValue:   0,
			wantPresent: true,
		},
		{
			name:        "field absent",
			data:        "name: etcd-main\ndata-dir: /var/etcd/data\n",
			wantPresent: false,
		},
		{
			name:        "empty document",
			data:        "",
			wantPresent: false,
		},
		{
			name:    "invalid yaml",
			data:    "quota-backend-bytes: : :\n\tbad",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			value, present, err := parseQuotaBackendBytesFromData([]byte(tt.data))
			if tt.wantErr {
				g.Expect(err).To(HaveOccurred())
				return
			}
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(present).To(Equal(tt.wantPresent))
			g.Expect(value).To(Equal(tt.wantValue))
		})
	}
}

func TestParseQuotaBackendBytesFromFile(t *testing.T) {
	g := NewWithT(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "etcd.conf.yaml")
	content := "name: etcd-main\nquota-backend-bytes: 2147483648\nauto-compaction-mode: periodic\n"
	g.Expect(os.WriteFile(path, []byte(content), 0o600)).To(Succeed())

	value, present, err := parseQuotaBackendBytes(path)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(present).To(BeTrue())
	g.Expect(value).To(Equal(int64(2147483648)))
}

func TestParseQuotaBackendBytesFileMissing(t *testing.T) {
	g := NewWithT(t)
	_, _, err := parseQuotaBackendBytes(filepath.Join(t.TempDir(), "does-not-exist.yaml"))
	g.Expect(err).To(HaveOccurred())
}
