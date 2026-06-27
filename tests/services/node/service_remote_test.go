package node_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"sing-box-web-panel/internal/domain"
	"sing-box-web-panel/internal/repo"
	svcclient "sing-box-web-panel/internal/services/client"
	svcinbound "sing-box-web-panel/internal/services/inbound"
	svcnode "sing-box-web-panel/internal/services/node"
)

type remoteNodeRepo struct {
	nodes map[int64]*domain.Node
}

func (r *remoteNodeRepo) Create(context.Context, *domain.Node) error { return nil }
func (r *remoteNodeRepo) GetByID(_ context.Context, id int64) (*domain.Node, error) {
	n, ok := r.nodes[id]
	if !ok {
		return nil, repo.ErrNotFound
	}
	cp := *n
	return &cp, nil
}
func (r *remoteNodeRepo) List(context.Context) ([]domain.Node, error)        { return nil, nil }
func (r *remoteNodeRepo) ListEnabled(context.Context) ([]domain.Node, error) { return nil, nil }
func (r *remoteNodeRepo) Update(context.Context, *domain.Node) error         { return nil }
func (r *remoteNodeRepo) Delete(context.Context, int64) error                { return nil }
func (r *remoteNodeRepo) SetEnabled(context.Context, int64, bool) error      { return nil }
func (r *remoteNodeRepo) SetStatus(context.Context, int64, domain.NodeStatus, *time.Time, int64, string, string, float64, float64, int64, string) error {
	return nil
}

type remoteInboundCache struct {
	items  map[int64]*domain.Inbound
	nextID int64
}

func newRemoteInboundCache() *remoteInboundCache {
	return &remoteInboundCache{items: map[int64]*domain.Inbound{}, nextID: 100}
}

func (c *remoteInboundCache) GetByID(_ context.Context, id int64) (*domain.Inbound, error) {
	ib, ok := c.items[id]
	if !ok {
		return nil, repo.ErrNotFound
	}
	cp := *ib
	return &cp, nil
}

func (c *remoteInboundCache) GetByRemote(_ context.Context, nodeID int64, remoteID string) (*domain.Inbound, error) {
	for _, ib := range c.items {
		if ib.NodeID != nil && *ib.NodeID == nodeID && ib.RemoteID == remoteID {
			cp := *ib
			return &cp, nil
		}
	}
	return nil, repo.ErrNotFound
}

func (c *remoteInboundCache) UpsertRemote(_ context.Context, nodeID int64, remoteID string, ib *domain.Inbound) (int64, error) {
	for id, existing := range c.items {
		if existing.NodeID != nil && *existing.NodeID == nodeID && existing.RemoteID == remoteID {
			cp := *ib
			cp.ID = id
			cp.NodeID = ptrInt64(nodeID)
			cp.RemoteID = remoteID
			c.items[id] = &cp
			return id, nil
		}
	}
	c.nextID++
	cp := *ib
	cp.ID = c.nextID
	cp.NodeID = ptrInt64(nodeID)
	cp.RemoteID = remoteID
	c.items[cp.ID] = &cp
	return cp.ID, nil
}

func (c *remoteInboundCache) Delete(_ context.Context, id int64) error {
	delete(c.items, id)
	return nil
}

type remoteClientCache struct {
	items  map[int64]*domain.Client
	nextID int64
}

func newRemoteClientCache() *remoteClientCache {
	return &remoteClientCache{items: map[int64]*domain.Client{}, nextID: 200}
}

func (c *remoteClientCache) GetByID(_ context.Context, id int64) (*domain.Client, error) {
	client, ok := c.items[id]
	if !ok {
		return nil, repo.ErrNotFound
	}
	cp := *client
	return &cp, nil
}

func (c *remoteClientCache) GetByRemote(_ context.Context, nodeID int64, remoteID string) (*domain.Client, error) {
	for _, client := range c.items {
		if client.NodeID != nil && *client.NodeID == nodeID && client.RemoteID == remoteID {
			cp := *client
			return &cp, nil
		}
	}
	return nil, repo.ErrNotFound
}

func (c *remoteClientCache) UpsertRemote(_ context.Context, nodeID int64, remoteID string, inboundID int64, client *domain.Client) error {
	for id, existing := range c.items {
		if existing.NodeID != nil && *existing.NodeID == nodeID && existing.RemoteID == remoteID {
			cp := *client
			cp.ID = id
			cp.NodeID = ptrInt64(nodeID)
			cp.RemoteID = remoteID
			cp.InboundID = inboundID
			c.items[id] = &cp
			return nil
		}
	}
	c.nextID++
	cp := *client
	cp.ID = c.nextID
	cp.NodeID = ptrInt64(nodeID)
	cp.RemoteID = remoteID
	cp.InboundID = inboundID
	c.items[cp.ID] = &cp
	return nil
}

func (c *remoteClientCache) Delete(_ context.Context, id int64) error {
	delete(c.items, id)
	return nil
}

func (c *remoteClientCache) DeleteMany(_ context.Context, ids []int64) error {
	for _, id := range ids {
		delete(c.items, id)
	}
	return nil
}

func (c *remoteClientCache) SetStatusMany(_ context.Context, ids []int64, status domain.ClientStatus, enabled bool) error {
	for _, id := range ids {
		if client, ok := c.items[id]; ok {
			client.Status = status
			client.Enabled = enabled
		}
	}
	return nil
}

func (c *remoteClientCache) ResetTrafficMany(_ context.Context, ids []int64) error {
	for _, id := range ids {
		if client, ok := c.items[id]; ok {
			client.UsedUp = 0
			client.UsedDown = 0
		}
	}
	return nil
}

type fakeRemoteClienter struct {
	createClientReq  *svcnode.RemoteClientCreateRequest
	updateInboundReq *svcnode.RemoteInboundRequest
	deleteInboundErr error
	bulkDeleteCalls  int
	bulkDeleteIDs    []string
}

func (f *fakeRemoteClienter) Status(context.Context, *domain.Node) (*svcnode.RemoteStatus, time.Duration, error) {
	return nil, 0, nil
}
func (f *fakeRemoteClienter) Snapshot(context.Context, *domain.Node) (*svcnode.RemoteSnapshot, error) {
	return nil, nil
}
func (f *fakeRemoteClienter) CreateInbound(context.Context, *domain.Node, svcnode.RemoteInboundRequest) (*svcnode.RemoteInbound, error) {
	return nil, nil
}
func (f *fakeRemoteClienter) UpdateInbound(_ context.Context, _ *domain.Node, remoteID string, in svcnode.RemoteInboundRequest) (*svcnode.RemoteInbound, error) {
	f.updateInboundReq = &in
	return &svcnode.RemoteInbound{ID: remoteID, Remark: in.Remark, Protocol: domain.Protocol(in.Protocol), Port: in.Port, TLS: domain.TLSMode(in.TLS), Settings: domain.InboundSettings{
		ACMEDomain:    in.ACMEDomain,
		ACMEEmail:     in.ACMEEmail,
		CertPath:      in.CertPath,
		KeyPath:       in.KeyPath,
		AllowInsecure: in.AllowInsecure,
	}}, nil
}
func (f *fakeRemoteClienter) DeleteInbound(context.Context, *domain.Node, string) error {
	return f.deleteInboundErr
}
func (f *fakeRemoteClienter) ToggleInbound(context.Context, *domain.Node, string) (*svcnode.RemoteInbound, error) {
	return nil, nil
}
func (f *fakeRemoteClienter) CreateClient(_ context.Context, _ *domain.Node, in svcnode.RemoteClientCreateRequest) (*svcnode.RemoteClient, error) {
	f.createClientReq = &in
	return &svcnode.RemoteClient{ID: "9", InboundID: in.InboundID, Name: in.Name, UUID: "uuid", Password: "pass", Status: domain.ClientStatusActive, SubToken: "sub", Enabled: true}, nil
}
func (f *fakeRemoteClienter) UpdateClient(context.Context, *domain.Node, string, svcnode.RemoteClientUpdateRequest) (*svcnode.RemoteClient, error) {
	return nil, nil
}
func (f *fakeRemoteClienter) DeleteClient(context.Context, *domain.Node, string) error { return nil }
func (f *fakeRemoteClienter) ResetClientTraffic(context.Context, *domain.Node, string) (*svcnode.RemoteClient, error) {
	return nil, nil
}
func (f *fakeRemoteClienter) SetClientStatus(context.Context, *domain.Node, string, domain.ClientStatus) (*svcnode.RemoteClient, error) {
	return nil, nil
}
func (f *fakeRemoteClienter) BulkDeleteClients(_ context.Context, _ *domain.Node, ids []string) (*svcnode.RemoteClientBulkResponse, error) {
	f.bulkDeleteCalls++
	f.bulkDeleteIDs = append([]string(nil), ids...)
	return successfulRemoteBulk(ids), nil
}
func (f *fakeRemoteClienter) BulkResetClientTraffic(_ context.Context, _ *domain.Node, ids []string) (*svcnode.RemoteClientBulkResponse, error) {
	return successfulRemoteBulk(ids), nil
}
func (f *fakeRemoteClienter) BulkSetClientStatus(_ context.Context, _ *domain.Node, ids []string, _ domain.ClientStatus) (*svcnode.RemoteClientBulkResponse, error) {
	return successfulRemoteBulk(ids), nil
}

func successfulRemoteBulk(ids []string) *svcnode.RemoteClientBulkResponse {
	results := make([]svcnode.RemoteClientBulkResult, len(ids))
	for i, id := range ids {
		results[i] = svcnode.RemoteClientBulkResult{ID: id, OK: true}
	}
	return &svcnode.RemoteClientBulkResponse{Results: results}
}

func TestServiceCreateRemoteClientMapsInboundAndCaches(t *testing.T) {
	inbounds := newRemoteInboundCache()
	clients := newRemoteClientCache()
	nodeID := int64(1)
	inbounds.items[10] = &domain.Inbound{ID: 10, NodeID: ptrInt64(nodeID), RemoteID: "7", Remark: "edge"}
	remote := &fakeRemoteClienter{}
	svc := newRemoteService(inbounds, clients, remote)

	got, err := svc.CreateClient(context.Background(), nodeID, svcclient.CreateInput{
		Name:      "alice",
		InboundID: 10,
	})
	if err != nil {
		t.Fatalf("create client: %v", err)
	}
	if remote.createClientReq == nil || remote.createClientReq.InboundID != "7" {
		t.Fatalf("remote inbound id = %#v, want 7", remote.createClientReq)
	}
	if got.NodeID == nil || *got.NodeID != nodeID || got.RemoteID != "9" || got.InboundID != 10 {
		t.Fatalf("cached client = %+v", got)
	}
}

func TestServiceUpdateRemoteInboundPreservesTLSFields(t *testing.T) {
	inbounds := newRemoteInboundCache()
	clients := newRemoteClientCache()
	nodeID := int64(1)
	inbounds.items[10] = &domain.Inbound{ID: 10, NodeID: ptrInt64(nodeID), RemoteID: "7", Remark: "edge"}
	remote := &fakeRemoteClienter{}
	svc := newRemoteService(inbounds, clients, remote)
	allowInsecure := false

	got, err := svc.UpdateInbound(context.Background(), 10, svcinbound.Input{
		Remark:        "edge-updated",
		Protocol:      domain.ProtocolHysteria2,
		Port:          8443,
		TLS:           domain.TLSModeTLS,
		ACMEDomain:    "vpn.example.com",
		ACMEEmail:     "admin@example.com",
		AllowInsecure: &allowInsecure,
	})
	if err != nil {
		t.Fatalf("update inbound: %v", err)
	}
	if remote.updateInboundReq == nil || remote.updateInboundReq.ACMEDomain != "vpn.example.com" || remote.updateInboundReq.AllowInsecure == nil || *remote.updateInboundReq.AllowInsecure {
		t.Fatalf("remote inbound request = %+v", remote.updateInboundReq)
	}
	if got.Settings.ACMEDomain != "vpn.example.com" || got.Settings.AllowInsecure == nil || *got.Settings.AllowInsecure {
		t.Fatalf("cached settings = %+v", got.Settings)
	}
}

func TestServiceDeleteRemoteInboundDropsCacheOnRemote404(t *testing.T) {
	inbounds := newRemoteInboundCache()
	clients := newRemoteClientCache()
	nodeID := int64(1)
	inbounds.items[10] = &domain.Inbound{ID: 10, NodeID: ptrInt64(nodeID), RemoteID: "7", Remark: "edge"}
	remote := &fakeRemoteClienter{deleteInboundErr: &svcnode.RemoteHTTPError{StatusCode: 404}}
	svc := newRemoteService(inbounds, clients, remote)

	if err := svc.DeleteInbound(context.Background(), 10); err != nil {
		t.Fatalf("delete inbound: %v", err)
	}
	if _, ok := inbounds.items[10]; ok {
		t.Fatal("cached inbound should be deleted")
	}
}

func TestServiceRejectsCrossNodeClientMove(t *testing.T) {
	inbounds := newRemoteInboundCache()
	clients := newRemoteClientCache()
	nodeID := int64(1)
	otherNodeID := int64(2)
	inbounds.items[10] = &domain.Inbound{ID: 10, NodeID: ptrInt64(otherNodeID), RemoteID: "other", Remark: "other"}
	clients.items[20] = &domain.Client{ID: 20, NodeID: ptrInt64(nodeID), RemoteID: "9", InboundID: 11, Name: "alice"}
	svc := newRemoteService(inbounds, clients, &fakeRemoteClienter{})

	_, err := svc.UpdateClient(context.Background(), 20, svcclient.UpdateInput{InboundID: ptrInt64(10)})
	if !errors.Is(err, svcnode.ErrValidation) {
		t.Fatalf("expected ErrValidation, got %v", err)
	}
}

func TestServiceBulkDeleteUsesOneRemoteRequestAndClearsCache(t *testing.T) {
	inbounds := newRemoteInboundCache()
	clients := newRemoteClientCache()
	nodeID := int64(1)
	clients.items[20] = &domain.Client{ID: 20, NodeID: ptrInt64(nodeID), RemoteID: "90", Name: "one"}
	clients.items[21] = &domain.Client{ID: 21, NodeID: ptrInt64(nodeID), RemoteID: "91", Name: "two"}
	remote := &fakeRemoteClienter{}
	service := newRemoteService(inbounds, clients, remote)

	results, err := service.BulkDeleteClients(context.Background(), nodeID, []int64{20, 21})
	if err != nil {
		t.Fatalf("BulkDeleteClients: %v", err)
	}
	if remote.bulkDeleteCalls != 1 {
		t.Fatalf("remote calls = %d, want 1", remote.bulkDeleteCalls)
	}
	if len(remote.bulkDeleteIDs) != 2 || remote.bulkDeleteIDs[0] != "90" || remote.bulkDeleteIDs[1] != "91" {
		t.Fatalf("remote ids = %v", remote.bulkDeleteIDs)
	}
	if !results[0].OK || !results[1].OK {
		t.Fatalf("results = %+v", results)
	}
	if _, exists := clients.items[20]; exists {
		t.Fatal("client 20 remains cached")
	}
	if _, exists := clients.items[21]; exists {
		t.Fatal("client 21 remains cached")
	}
}

func newRemoteService(inbounds *remoteInboundCache, clients *remoteClientCache, remote *fakeRemoteClienter) *svcnode.Service {
	nodes := &remoteNodeRepo{nodes: map[int64]*domain.Node{
		1: {ID: 1, Enabled: true, APITokenSecret: "secret"},
		2: {ID: 2, Enabled: true, APITokenSecret: "secret"},
	}}
	return svcnode.NewService(nodes, inbounds, clients, remote)
}

func ptrInt64(v int64) *int64 {
	return &v
}
