// Copyright Envoy AI Gateway Authors
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package controller

import (
	"testing"

	egv1a1 "github.com/envoyproxy/gateway/api/v1alpha1"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fake2 "k8s.io/client-go/kubernetes/fake"
	gwapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	aigv1b1 "github.com/envoyproxy/ai-gateway/api/v1beta1"
	"github.com/envoyproxy/ai-gateway/internal/filterapi"
)

func TestGatewayController_resolveBackendFQDNHostname(t *testing.T) {
	namespace := "envoy-ai-gateway-system"
	fakeClient := requireNewFakeClientWithIndexes(t)
	c := NewGatewayController(fakeClient, fake2.NewClientset(), ctrl.Log, "envoy-gateway-system",
		"docker.io/envoyproxy/ai-gateway-extproc:latest", "info", false, nil, true)

	require.NoError(t, fakeClient.Create(t.Context(), &egv1a1.Backend{
		ObjectMeta: metav1.ObjectMeta{Name: "bedrock-runtime-us-west-2", Namespace: namespace},
		Spec: egv1a1.BackendSpec{
			Endpoints: []egv1a1.BackendEndpoint{{
				FQDN: &egv1a1.FQDNEndpoint{Hostname: "bedrock-runtime.us-west-2.amazonaws.com", Port: 443},
			}},
		},
	}))
	require.NoError(t, fakeClient.Create(t.Context(), &egv1a1.Backend{
		ObjectMeta: metav1.ObjectMeta{Name: "bedrock-mantle-us-east-1", Namespace: namespace},
		Spec: egv1a1.BackendSpec{
			Endpoints: []egv1a1.BackendEndpoint{{
				FQDN: &egv1a1.FQDNEndpoint{Hostname: "bedrock-mantle.us-east-1.api.aws", Port: 443},
			}},
		},
	}))
	require.NoError(t, fakeClient.Create(t.Context(), &egv1a1.Backend{
		ObjectMeta: metav1.ObjectMeta{Name: "legacy-hostname-backend", Namespace: namespace},
		Spec: egv1a1.BackendSpec{
			Endpoints: []egv1a1.BackendEndpoint{{
				Hostname: ptr.To("bedrock-runtime.eu-central-1.amazonaws.com"),
			}},
		},
	}))

	for _, tc := range []struct {
		name string
		ref  gwapiv1.BackendObjectReference
		want string
	}{
		{
			name: "fqdn endpoint",
			ref: gwapiv1.BackendObjectReference{
				Group:     ptr.To(gwapiv1.Group("gateway.envoyproxy.io")),
				Kind:      ptr.To(gwapiv1.Kind("Backend")),
				Name:      "bedrock-runtime-us-west-2",
				Namespace: ptr.To(gwapiv1.Namespace(namespace)),
			},
			want: "bedrock-runtime.us-west-2.amazonaws.com",
		},
		{
			name: "mantle fqdn endpoint",
			ref: gwapiv1.BackendObjectReference{
				Group:     ptr.To(gwapiv1.Group("gateway.envoyproxy.io")),
				Kind:      ptr.To(gwapiv1.Kind("Backend")),
				Name:      "bedrock-mantle-us-east-1",
				Namespace: ptr.To(gwapiv1.Namespace(namespace)),
			},
			want: "bedrock-mantle.us-east-1.api.aws",
		},
		{
			name: "legacy hostname endpoint",
			ref: gwapiv1.BackendObjectReference{
				Group:     ptr.To(gwapiv1.Group("gateway.envoyproxy.io")),
				Kind:      ptr.To(gwapiv1.Kind("Backend")),
				Name:      "legacy-hostname-backend",
				Namespace: ptr.To(gwapiv1.Namespace(namespace)),
			},
			want: "bedrock-runtime.eu-central-1.amazonaws.com",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := c.resolveBackendFQDNHostname(t.Context(), namespace, tc.ref)
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}

	t.Run("missing backend", func(t *testing.T) {
		_, err := c.resolveBackendFQDNHostname(t.Context(), namespace, gwapiv1.BackendObjectReference{
			Group:     ptr.To(gwapiv1.Group("gateway.envoyproxy.io")),
			Kind:      ptr.To(gwapiv1.Kind("Backend")),
			Name:      "does-not-exist",
			Namespace: ptr.To(gwapiv1.Namespace(namespace)),
		})
		require.ErrorContains(t, err, "failed to get Backend")
	})
}

func TestGatewayController_bspToFilterAPIBackendAuth_AutoSigningHost(t *testing.T) {
	namespace := "envoy-ai-gateway-system"
	fakeClient := requireNewFakeClientWithIndexes(t)
	c := NewGatewayController(fakeClient, fake2.NewClientset(), ctrl.Log, "envoy-gateway-system",
		"docker.io/envoyproxy/ai-gateway-extproc:latest", "info", false, nil, true)

	require.NoError(t, fakeClient.Create(t.Context(), &egv1a1.Backend{
		ObjectMeta: metav1.ObjectMeta{Name: "bedrock-runtime-us-west-2", Namespace: namespace},
		Spec: egv1a1.BackendSpec{
			Endpoints: []egv1a1.BackendEndpoint{{
				FQDN: &egv1a1.FQDNEndpoint{Hostname: "bedrock-runtime.us-west-2.amazonaws.com", Port: 443},
			}},
		},
	}))
	require.NoError(t, fakeClient.Create(t.Context(), &egv1a1.Backend{
		ObjectMeta: metav1.ObjectMeta{Name: "bedrock-mantle-us-east-1", Namespace: namespace},
		Spec: egv1a1.BackendSpec{
			Endpoints: []egv1a1.BackendEndpoint{{
				FQDN: &egv1a1.FQDNEndpoint{Hostname: "bedrock-mantle.us-east-1.api.aws", Port: 443},
			}},
		},
	}))

	runtimeBackend := &aigv1b1.AIServiceBackend{
		ObjectMeta: metav1.ObjectMeta{Name: "bedrock-runtime-backend", Namespace: namespace},
		Spec: aigv1b1.AIServiceBackendSpec{
			BackendRef: gwapiv1.BackendObjectReference{
				Group:     ptr.To(gwapiv1.Group("gateway.envoyproxy.io")),
				Kind:      ptr.To(gwapiv1.Kind("Backend")),
				Name:      "bedrock-runtime-us-west-2",
				Namespace: ptr.To(gwapiv1.Namespace(namespace)),
			},
		},
	}
	mantleBackend := &aigv1b1.AIServiceBackend{
		ObjectMeta: metav1.ObjectMeta{Name: "bedrock-mantle-backend", Namespace: namespace},
		Spec: aigv1b1.AIServiceBackendSpec{
			BackendRef: gwapiv1.BackendObjectReference{
				Group:     ptr.To(gwapiv1.Group("gateway.envoyproxy.io")),
				Kind:      ptr.To(gwapiv1.Kind("Backend")),
				Name:      "bedrock-mantle-us-east-1",
				Namespace: ptr.To(gwapiv1.Namespace(namespace)),
			},
		},
	}

	for _, bsp := range []*aigv1b1.BackendSecurityPolicy{
		{
			ObjectMeta: metav1.ObjectMeta{Name: "aws-runtime-auto-host", Namespace: namespace},
			Spec: aigv1b1.BackendSecurityPolicySpec{
				Type: aigv1b1.BackendSecurityPolicyTypeAWSCredentials,
				AWSCredentials: &aigv1b1.BackendSecurityPolicyAWSCredentials{
					Region: "us-west-2",
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{Name: "aws-mantle-auto-host", Namespace: namespace},
			Spec: aigv1b1.BackendSecurityPolicySpec{
				Type: aigv1b1.BackendSecurityPolicyTypeAWSCredentials,
				AWSCredentials: &aigv1b1.BackendSecurityPolicyAWSCredentials{
					Region: "us-east-1",
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{Name: "aws-mantle-explicit-override", Namespace: namespace},
			Spec: aigv1b1.BackendSecurityPolicySpec{
				Type: aigv1b1.BackendSecurityPolicyTypeAWSCredentials,
				AWSCredentials: &aigv1b1.BackendSecurityPolicyAWSCredentials{
					Region:      "us-east-1",
					SigningHost: ptr.To("custom-signing-host.example.aws"),
				},
			},
		},
	} {
		require.NoError(t, fakeClient.Create(t.Context(), bsp))
	}

	for _, tc := range []struct {
		name            string
		bspName         string
		aiServiceBackend *aigv1b1.AIServiceBackend
		want            *filterapi.BackendAuth
	}{
		{
			name:             "bedrock runtime host from Backend CR",
			bspName:          "aws-runtime-auto-host",
			aiServiceBackend: runtimeBackend,
			want: &filterapi.BackendAuth{
				AWSAuth: &filterapi.AWSAuth{
					Region:      "us-west-2",
					SigningHost: "bedrock-runtime.us-west-2.amazonaws.com",
				},
			},
		},
		{
			name:             "mantle host from Backend CR",
			bspName:          "aws-mantle-auto-host",
			aiServiceBackend: mantleBackend,
			want: &filterapi.BackendAuth{
				AWSAuth: &filterapi.AWSAuth{
					Region:      "us-east-1",
					SigningHost: "bedrock-mantle.us-east-1.api.aws",
				},
			},
		},
		{
			name:             "explicit BSP signingHost overrides Backend CR",
			bspName:          "aws-mantle-explicit-override",
			aiServiceBackend: mantleBackend,
			want: &filterapi.BackendAuth{
				AWSAuth: &filterapi.AWSAuth{
					Region:      "us-east-1",
					SigningHost: "custom-signing-host.example.aws",
				},
			},
		},
		{
			name:    "no AIServiceBackend keeps legacy runtime fallback in extproc",
			bspName: "aws-runtime-auto-host",
			want: &filterapi.BackendAuth{
				AWSAuth: &filterapi.AWSAuth{
					Region: "us-west-2",
				},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bsp := &aigv1b1.BackendSecurityPolicy{}
			require.NoError(t, fakeClient.Get(t.Context(), client.ObjectKey{Name: tc.bspName, Namespace: namespace}, bsp))
			auth, err := c.bspToFilterAPIBackendAuth(t.Context(), bsp, tc.aiServiceBackend)
			require.NoError(t, err)
			require.Equal(t, tc.want, auth)
		})
	}
}
