// Copyright (c) 2020-2021 Doc.ai and/or its affiliates.
//
// SPDX-License-Identifier: Apache-2.0
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at:
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package nsmgrproxy_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/golang/protobuf/ptypes/empty"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
	"google.golang.org/grpc"

	"github.com/networkservicemesh/api/pkg/api/networkservice"
	"github.com/networkservicemesh/api/pkg/api/networkservice/mechanisms/cls"
	"github.com/networkservicemesh/api/pkg/api/networkservice/mechanisms/kernel"
	"github.com/networkservicemesh/api/pkg/api/registry"

	"github.com/networkservicemesh/sdk/pkg/networkservice/chains/client"
	"github.com/networkservicemesh/sdk/pkg/networkservice/common/clienturl"
	"github.com/networkservicemesh/sdk/pkg/networkservice/common/connect"
	kernelmech "github.com/networkservicemesh/sdk/pkg/networkservice/common/mechanisms/kernel"
	"github.com/networkservicemesh/sdk/pkg/networkservice/core/chain"
	"github.com/networkservicemesh/sdk/pkg/networkservice/core/next"
	"github.com/networkservicemesh/sdk/pkg/tools/sandbox"
)

// TestNSMGR_InterdomainUseCase covers simple interdomain scenario:
//
//  nsc -> nsmgr1 ->  forwarder1 -> nsmgr1 -> nsmgr-proxy1 -> nsmg-proxy2 -> nsmgr2 ->forwarder2 -> nsmgr2 -> nse3
func TestNSMGR_InterdomainUseCase(t *testing.T) {
	t.Cleanup(func() { goleak.VerifyNone(t) })

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()

	var dnsServer = new(sandbox.FakeDNSResolver)

	cluster1 := sandbox.NewBuilder(ctx, t).
		SetNodesCount(1).
		SetDNSResolver(dnsServer).
		SetDNSDomainName("cluster1").
		Build()

	cluster2 := sandbox.NewBuilder(ctx, t).
		SetNodesCount(1).
		SetDNSDomainName("cluster2").
		SetDNSResolver(dnsServer).
		Build()

	nsRegistryClient := cluster2.NewNSRegistryClient(ctx, sandbox.DefaultTokenTimeout)

	nsReg := &registry.NetworkService{
		Name: "my-service-interdomain",
	}

	_, err := nsRegistryClient.Register(ctx, nsReg)
	require.NoError(t, err)

	nseReg := &registry.NetworkServiceEndpoint{
		Name:                "final-endpoint",
		NetworkServiceNames: []string{nsReg.Name},
	}

	cluster2.Nodes[0].NewEndpoint(ctx, nseReg, sandbox.DefaultTokenTimeout)

	nsc := cluster1.Nodes[0].NewClient(ctx, sandbox.DefaultTokenTimeout)

	request := &networkservice.NetworkServiceRequest{
		MechanismPreferences: []*networkservice.Mechanism{
			{Cls: cls.LOCAL, Type: kernel.MECHANISM},
		},
		Connection: &networkservice.Connection{
			Id:             "1",
			NetworkService: fmt.Sprint(nsReg.Name, "@", cluster2.Name),
			Context:        &networkservice.ConnectionContext{},
		},
	}

	conn, err := nsc.Request(ctx, request)
	require.NoError(t, err)
	require.NotNil(t, conn)

	require.Equal(t, 10, len(conn.Path.PathSegments))

	// Simulate refresh from client.

	refreshRequest := request.Clone()
	refreshRequest.Connection = conn.Clone()

	conn, err = nsc.Request(ctx, refreshRequest)
	require.NoError(t, err)
	require.NotNil(t, conn)
	require.Equal(t, 10, len(conn.Path.PathSegments))

	// Close
	_, err = nsc.Close(ctx, conn)
	require.NoError(t, err)
}

// TestNSMGR_Interdomain_TwoNodesNSEs covers scenarion with connection from the one client to two endpoints from diffrenret clusters.
//
//  nsc -> nsmgr1 ->  forwarder1 -> nsmgr1 -> nsmgr-proxy1 -> nsmg-proxy2 -> nsmgr2 ->forwarder2 -> nsmgr2 -> nse2
//  nsc -> nsmgr1 ->  forwarder1 -> nsmgr1 -> nsmgr-proxy1 -> nsmg-proxy3 -> nsmgr3 ->forwarder3 -> nsmgr3 -> nse3
func TestNSMGR_Interdomain_TwoNodesNSEs(t *testing.T) {
	t.Cleanup(func() { goleak.VerifyNone(t) })

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()

	var dnsServer = new(sandbox.FakeDNSResolver)

	cluster1 := sandbox.NewBuilder(ctx, t).
		SetNodesCount(1).
		SetDNSResolver(dnsServer).
		SetDNSDomainName("cluster1").
		Build()

	cluster2 := sandbox.NewBuilder(ctx, t).
		SetNodesCount(2).
		SetDNSDomainName("cluster2").
		SetDNSResolver(dnsServer).
		Build()

	nsRegistryClient := cluster2.NewNSRegistryClient(ctx, sandbox.DefaultTokenTimeout)

	_, err := nsRegistryClient.Register(ctx, &registry.NetworkService{
		Name: "my-service-interdomain-1",
	})
	require.NoError(t, err)

	_, err = nsRegistryClient.Register(ctx, &registry.NetworkService{
		Name: "my-service-interdomain-2",
	})
	require.NoError(t, err)

	nseReg1 := &registry.NetworkServiceEndpoint{
		Name:                "final-endpoint-1",
		NetworkServiceNames: []string{"my-service-interdomain-1"},
	}
	cluster2.Nodes[0].NewEndpoint(ctx, nseReg1, sandbox.DefaultTokenTimeout)

	nseReg2 := &registry.NetworkServiceEndpoint{
		Name:                "final-endpoint-2",
		NetworkServiceNames: []string{"my-service-interdomain-2"},
	}
	cluster2.Nodes[0].NewEndpoint(ctx, nseReg2, sandbox.DefaultTokenTimeout)

	nsc := cluster1.Nodes[0].NewClient(ctx, sandbox.DefaultTokenTimeout)

	request := &networkservice.NetworkServiceRequest{
		MechanismPreferences: []*networkservice.Mechanism{
			{Cls: cls.LOCAL, Type: kernel.MECHANISM},
		},
		Connection: &networkservice.Connection{
			Id:             "1",
			NetworkService: fmt.Sprint("my-service-interdomain-1", "@", cluster2.Name),
			Context:        &networkservice.ConnectionContext{},
		},
	}

	conn, err := nsc.Request(ctx, request)
	require.NoError(t, err)
	require.NotNil(t, conn)

	require.Equal(t, 10, len(conn.Path.PathSegments))

	// Simulate refresh from client.

	refreshRequest := request.Clone()
	refreshRequest.Connection = conn.Clone()

	conn, err = nsc.Request(ctx, refreshRequest)
	require.NoError(t, err)
	require.NotNil(t, conn)
	require.Equal(t, 10, len(conn.Path.PathSegments))

	request = &networkservice.NetworkServiceRequest{
		MechanismPreferences: []*networkservice.Mechanism{
			{Cls: cls.LOCAL, Type: kernel.MECHANISM},
		},
		Connection: &networkservice.Connection{
			Id:             "2",
			NetworkService: fmt.Sprint("my-service-interdomain-2", "@", cluster2.Name),
			Context:        &networkservice.ConnectionContext{},
		},
	}

	conn, err = nsc.Request(ctx, request)
	require.NoError(t, err)
	require.NotNil(t, conn)

	require.Equal(t, 10, len(conn.Path.PathSegments))

	// Simulate refresh from client.

	refreshRequest = request.Clone()
	refreshRequest.Connection = conn.Clone()

	conn, err = nsc.Request(ctx, refreshRequest)
	require.NoError(t, err)
	require.NotNil(t, conn)
	require.Equal(t, 10, len(conn.Path.PathSegments))
}

// TestNSMGR_FloatingInterdomainUseCase covers simple interdomain scenario with resolving endpoint from floating registry:
//
//  nsc -> nsmgr1 ->  forwarder1 -> nsmgr1 -> nsmgr-proxy1 -> nsmg-proxy2 -> nsmgr2 ->forwarder2 -> nsmgr2 -> nse3
func TestNSMGR_FloatingInterdomainUseCase(t *testing.T) {
	t.Cleanup(func() { goleak.VerifyNone(t) })

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()

	var dnsServer = new(sandbox.FakeDNSResolver)

	cluster1 := sandbox.NewBuilder(ctx, t).
		SetNodesCount(1).
		SetDNSResolver(dnsServer).
		SetDNSDomainName("cluster1").
		Build()

	cluster2 := sandbox.NewBuilder(ctx, t).
		SetNodesCount(1).
		SetDNSDomainName("cluster2").
		SetDNSResolver(dnsServer).
		Build()

	floating := sandbox.NewBuilder(ctx, t).
		SetNodesCount(0).
		SetDNSDomainName("floating.domain").
		SetDNSResolver(dnsServer).
		SetNSMgrProxySupplier(nil).
		SetRegistryProxySupplier(nil).
		Build()

	nsRegistryClient := cluster2.NewNSRegistryClient(ctx, sandbox.DefaultTokenTimeout)

	nsReg := &registry.NetworkService{
		Name: "my-service-interdomain@" + floating.Name,
	}

	_, err := nsRegistryClient.Register(ctx, nsReg)
	require.NoError(t, err)

	nseReg := &registry.NetworkServiceEndpoint{
		Name:                "final-endpoint@" + floating.Name,
		NetworkServiceNames: []string{"my-service-interdomain"},
	}

	cluster2.Nodes[0].NewEndpoint(ctx, nseReg, sandbox.DefaultTokenTimeout)

	nsc := cluster1.Nodes[0].NewClient(ctx, sandbox.DefaultTokenTimeout)

	request := &networkservice.NetworkServiceRequest{
		MechanismPreferences: []*networkservice.Mechanism{
			{Cls: cls.LOCAL, Type: kernel.MECHANISM},
		},
		Connection: &networkservice.Connection{
			Id:             "1",
			NetworkService: fmt.Sprint(nsReg.Name),
			Context:        &networkservice.ConnectionContext{},
		},
	}

	conn, err := nsc.Request(ctx, request)
	require.NoError(t, err)
	require.NotNil(t, conn)

	require.Equal(t, 10, len(conn.Path.PathSegments))

	// Simulate refresh from client.

	refreshRequest := request.Clone()
	refreshRequest.Connection = conn.Clone()

	conn, err = nsc.Request(ctx, refreshRequest)
	require.NoError(t, err)
	require.NotNil(t, conn)
	require.Equal(t, 10, len(conn.Path.PathSegments))

	// Close
	_, err = nsc.Close(ctx, conn)
	require.NoError(t, err)
}

// TestNSMGR_FloatingInterdomain_FourClusters covers scenarion with connection from the one client to two endpoints
// from diffrenret clusters using floating registry for resolving endpoints.
//
//  nsc -> nsmgr1 ->  forwarder1 -> nsmgr1 -> nsmgr-proxy1 -> nsmg-proxy2 -> nsmgr2 ->forwarder2 -> nsmgr2 -> nse2
//  nsc -> nsmgr1 ->  forwarder1 -> nsmgr1 -> nsmgr-proxy1 -> nsmg-proxy3 -> nsmgr3 ->forwarder3 -> nsmgr3 -> nse3
func TestNSMGR_FloatingInterdomain_FourClusters(t *testing.T) {
	t.Cleanup(func() { goleak.VerifyNone(t) })

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()

	var dnsServer = new(sandbox.FakeDNSResolver)

	// setup clusters

	cluster1 := sandbox.NewBuilder(ctx, t).
		SetNodesCount(1).
		SetDNSResolver(dnsServer).
		SetDNSDomainName("cluster1").
		Build()

	cluster2 := sandbox.NewBuilder(ctx, t).
		SetNodesCount(1).
		SetDNSDomainName("cluster2").
		SetDNSResolver(dnsServer).
		Build()

	cluster3 := sandbox.NewBuilder(ctx, t).
		SetNodesCount(1).
		SetDNSDomainName("cluster3").
		SetDNSResolver(dnsServer).
		Build()

	floating := sandbox.NewBuilder(ctx, t).
		SetNodesCount(0).
		SetDNSDomainName("floating.domain").
		SetDNSResolver(dnsServer).
		SetNSMgrProxySupplier(nil).
		SetRegistryProxySupplier(nil).
		Build()

	// register first ednpoint

	nsRegistryClient := cluster2.NewNSRegistryClient(ctx, sandbox.DefaultTokenTimeout)

	nsReg1 := &registry.NetworkService{
		Name: "my-service-interdomain-1@" + floating.Name,
	}

	_, err := nsRegistryClient.Register(ctx, nsReg1)
	require.NoError(t, err)

	nseReg1 := &registry.NetworkServiceEndpoint{
		Name:                "final-endpoint-1@" + floating.Name,
		NetworkServiceNames: []string{"my-service-interdomain-1"},
	}

	cluster2.Nodes[0].NewEndpoint(ctx, nseReg1, sandbox.DefaultTokenTimeout)

	nsReg2 := &registry.NetworkService{
		Name: "my-service-interdomain-1@" + floating.Name,
	}

	// register second ednpoint

	nsRegistryClient = cluster3.NewNSRegistryClient(ctx, sandbox.DefaultTokenTimeout)

	_, err = nsRegistryClient.Register(ctx, nsReg2)
	require.NoError(t, err)

	nseReg2 := &registry.NetworkServiceEndpoint{
		Name:                "final-endpoint-2@" + floating.Name,
		NetworkServiceNames: []string{"my-service-interdomain-2"},
	}

	cluster3.Nodes[0].NewEndpoint(ctx, nseReg2, sandbox.DefaultTokenTimeout)

	// connect to first endpoint from cluster2

	nsc := cluster1.Nodes[0].NewClient(ctx, sandbox.DefaultTokenTimeout)

	request := &networkservice.NetworkServiceRequest{
		MechanismPreferences: []*networkservice.Mechanism{
			{Cls: cls.LOCAL, Type: kernel.MECHANISM},
		},
		Connection: &networkservice.Connection{
			Id:             "1",
			NetworkService: fmt.Sprint(nsReg1.Name),
			Context:        &networkservice.ConnectionContext{},
		},
	}

	conn, err := nsc.Request(ctx, request)
	require.NoError(t, err)
	require.NotNil(t, conn)

	require.Equal(t, 10, len(conn.Path.PathSegments))

	// Simulate refresh from client.

	refreshRequest := request.Clone()
	refreshRequest.Connection = conn.Clone()

	conn, err = nsc.Request(ctx, refreshRequest)
	require.NoError(t, err)
	require.NotNil(t, conn)
	require.Equal(t, 10, len(conn.Path.PathSegments))

	// connect to second endpoint from cluster3
	request = &networkservice.NetworkServiceRequest{
		MechanismPreferences: []*networkservice.Mechanism{
			{Cls: cls.LOCAL, Type: kernel.MECHANISM},
		},
		Connection: &networkservice.Connection{
			Id:             "2",
			NetworkService: fmt.Sprint(nsReg2.Name),
			Context:        &networkservice.ConnectionContext{},
		},
	}

	conn, err = nsc.Request(ctx, request)
	require.NoError(t, err)
	require.NotNil(t, conn)

	require.Equal(t, 10, len(conn.Path.PathSegments))

	// Simulate refresh from client.

	refreshRequest = request.Clone()
	refreshRequest.Connection = conn.Clone()

	conn, err = nsc.Request(ctx, refreshRequest)
	require.NoError(t, err)
	require.NotNil(t, conn)
	require.Equal(t, 10, len(conn.Path.PathSegments))
}

type passThroughClient struct {
	networkService string
}

func newPassTroughClient(networkService string) *passThroughClient {
	return &passThroughClient{
		networkService: networkService,
	}
}

func (c *passThroughClient) Request(ctx context.Context, request *networkservice.NetworkServiceRequest, opts ...grpc.CallOption) (*networkservice.Connection, error) {
	request.Connection.NetworkService = c.networkService
	request.Connection.NetworkServiceEndpointName = ""
	return next.Client(ctx).Request(ctx, request, opts...)
}

func (c *passThroughClient) Close(ctx context.Context, conn *networkservice.Connection, opts ...grpc.CallOption) (*empty.Empty, error) {
	conn.NetworkService = c.networkService
	return next.Client(ctx).Close(ctx, conn, opts...)
}

// Test_Interdomain_PassThroughUsecase covers scenario when we have 5 clusters.
// Each cluster contains NSE with name endpoint-${cluster-num}.
// Each endpoint request endpoint from the previous cluster (exclude 1st).
//
//
// nsc -> nsmgr4 ->  forwarder4 -> nsmgr4 -> nsmgr-proxy4 -> nsmgr-proxy3 -> nsmgr3 ->forwarder3 -> nsmgr3 -> nse3 ->
// nse3 -> nsmgr3 ->  forwarder3 -> nsmgr3 -> nsmgr-proxy3 -> nsmgr-proxy2 -> nsmgr2 ->forwarder2-> nsmgr2 -> nse2 ->
// nse2 -> nsmgr2 ->  forwarder2 -> nsmgr2 -> nsmgr-proxy2 -> nsmgr-proxy1 -> nsmgr1 -> forwarder1 -> nsmgr1 -> nse1 ->
// nse1 -> nsmgr1 ->  forwarder1 -> nsmg1 -> nsmgr-proxy1 -> nsmgr-proxy0 -> nsmgr0 -> forwarder0 -> nsmgr0 -> nse0

func Test_Interdomain_PassThroughUsecase(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()

	const clusterCount = 5

	var dnsServer = new(sandbox.FakeDNSResolver)
	var clusters = make([]*sandbox.Domain, clusterCount)

	for i := 0; i < clusterCount; i++ {
		clusters[i] = sandbox.NewBuilder(ctx, t).
			SetNodesCount(1).
			SetDNSResolver(dnsServer).
			SetDNSDomainName("cluster" + fmt.Sprint(i)).
			Build()
		var additionalFunctionality []networkservice.NetworkServiceServer
		if i != 0 {
			// Passtrough to the node i-1
			additionalFunctionality = []networkservice.NetworkServiceServer{
				chain.NewNetworkServiceServer(
					clienturl.NewServer(clusters[i].Nodes[0].URL()),
					connect.NewServer(ctx,
						client.NewClientFactory(client.WithAdditionalFunctionality(
							newPassTroughClient(fmt.Sprintf("my-service-remote-%v@cluster%v", i-1, i-1)),
							kernelmech.NewClient(),
						)),
						connect.WithDialTimeout(sandbox.DialTimeout),
						connect.WithDialOptions(clusters[i].DefaultDialOptions(sandbox.DefaultTokenTimeout)...),
					),
				),
			}
		}

		nsRegistryClient := clusters[i].NewNSRegistryClient(ctx, sandbox.DefaultTokenTimeout)

		nsReg, err := nsRegistryClient.Register(ctx, &registry.NetworkService{
			Name: fmt.Sprintf("my-service-remote-%v", i),
		})
		require.NoError(t, err)

		nsesReg := &registry.NetworkServiceEndpoint{
			Name:                fmt.Sprintf("endpoint-%v", i),
			NetworkServiceNames: []string{nsReg.Name},
		}
		clusters[i].Nodes[0].NewEndpoint(ctx, nsesReg, sandbox.DefaultTokenTimeout, additionalFunctionality...)
	}

	nsc := clusters[clusterCount-1].Nodes[0].NewClient(ctx, sandbox.DefaultTokenTimeout)

	request := &networkservice.NetworkServiceRequest{
		MechanismPreferences: []*networkservice.Mechanism{
			{Cls: cls.LOCAL, Type: kernel.MECHANISM},
		},
		Connection: &networkservice.Connection{
			Id:             "1",
			NetworkService: fmt.Sprintf("my-service-remote-%v", clusterCount-1),
			Context:        &networkservice.ConnectionContext{},
		},
	}

	conn, err := nsc.Request(ctx, request)
	require.NoError(t, err)
	require.NotNil(t, conn)

	// Path length to first endpoint is 5
	// Path length from NSE client to other remote endpoint is 10
	require.Equal(t, 10*(clusterCount-1)+5, len(conn.Path.PathSegments))

	// Close
	_, err = nsc.Close(ctx, conn)
	require.NoError(t, err)
}
