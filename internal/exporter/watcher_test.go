// SPDX-FileCopyrightText: Contributors to the Gardener project
//
// SPDX-License-Identifier: Apache-2.0

package exporter

import (
	"testing"

	"go.etcd.io/etcd/api/v3/mvccpb"

	. "github.com/onsi/gomega"
)

func TestResourceFromKey(t *testing.T) {
	tests := []struct {
		name string
		key  string
		want string
	}{
		{name: "lease key", key: "/registry/leases/x", want: "leases"},
		{name: "namespaced pod key", key: "/registry/pods/ns/name", want: "pods"},
		{name: "two segments only", key: "/registry", want: otherResourceBucket},
		{name: "single segment no slash", key: "registry", want: otherResourceBucket},
		{name: "empty key", key: "", want: otherResourceBucket},
		{name: "trailing empty resource segment", key: "//", want: otherResourceBucket},
		{name: "deep key still resolves to resource", key: "/registry/configmaps/kube-system/foo/bar", want: "configmaps"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			g.Expect(resourceFromKey(tt.key)).To(Equal(tt.want))
		})
	}
}

func TestEventTypeLabel(t *testing.T) {
	tests := []struct {
		name string
		in   mvccpb.Event_EventType
		want string
	}{
		{name: "put", in: mvccpb.PUT, want: "PUT"},
		{name: "delete", in: mvccpb.DELETE, want: "DELETE"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			g.Expect(eventTypeLabel(tt.in)).To(Equal(tt.want))
		})
	}
}
