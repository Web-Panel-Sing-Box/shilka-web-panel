package node

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"sing-box-web-panel/internal/domain"
	"sing-box-web-panel/internal/repo"
	svcclient "sing-box-web-panel/internal/services/client"
	svcinbound "sing-box-web-panel/internal/services/inbound"
)

var ErrValidation = errors.New("node validation error")

type Repo interface {
	Create(ctx context.Context, n *domain.Node) error
	GetByID(ctx context.Context, id int64) (*domain.Node, error)
	List(ctx context.Context) ([]domain.Node, error)
	ListEnabled(ctx context.Context) ([]domain.Node, error)
	Update(ctx context.Context, n *domain.Node) error
	Delete(ctx context.Context, id int64) error
	SetEnabled(ctx context.Context, id int64, enabled bool) error
	SetStatus(ctx context.Context, id int64, status domain.NodeStatus, heartbeatAt *time.Time, latencyMS int64, panelVersion, coreVersion string, cpuPct, ramPct float64, uptimeSeconds int64, lastErr string) error
}

type InboundCache interface {
	GetByID(ctx context.Context, id int64) (*domain.Inbound, error)
	GetByRemote(ctx context.Context, nodeID int64, remoteID string) (*domain.Inbound, error)
	UpsertRemote(ctx context.Context, nodeID int64, remoteID string, ib *domain.Inbound) (int64, error)
	Delete(ctx context.Context, id int64) error
}

type ClientCache interface {
	GetByID(ctx context.Context, id int64) (*domain.Client, error)
	GetByRemote(ctx context.Context, nodeID int64, remoteID string) (*domain.Client, error)
	UpsertRemote(ctx context.Context, nodeID int64, remoteID string, inboundID int64, c *domain.Client) error
	Delete(ctx context.Context, id int64) error
	DeleteMany(ctx context.Context, ids []int64) error
	SetStatusMany(ctx context.Context, ids []int64, status domain.ClientStatus, enabled bool) error
	ResetTrafficMany(ctx context.Context, ids []int64) error
}

type Service struct {
	repo     Repo
	inbounds InboundCache
	clients  ClientCache
	remote   RemoteClienter
	now      func() time.Time
}

func NewService(repo Repo, inbounds InboundCache, clients ClientCache, remote RemoteClienter) *Service {
	return &Service{repo: repo, inbounds: inbounds, clients: clients, remote: remote, now: time.Now}
}

func (s *Service) List(ctx context.Context) ([]domain.Node, error) {
	return s.repo.List(ctx)
}

func (s *Service) Get(ctx context.Context, id int64) (*domain.Node, error) {
	return s.repo.GetByID(ctx, id)
}

func (s *Service) Create(ctx context.Context, in Input) (*domain.Node, error) {
	n, err := nodeFromInput(in)
	if err != nil {
		return nil, err
	}
	if err := s.repo.Create(ctx, n); err != nil {
		return nil, err
	}
	return n, nil
}

func (s *Service) Update(ctx context.Context, id int64, in Input) (*domain.Node, error) {
	n, err := nodeFromInput(in)
	if err != nil {
		return nil, err
	}
	n.ID = id
	if err := s.repo.Update(ctx, n); err != nil {
		return nil, err
	}
	return s.repo.GetByID(ctx, id)
}

func (s *Service) Delete(ctx context.Context, id int64) error {
	return s.repo.Delete(ctx, id)
}

func (s *Service) Toggle(ctx context.Context, id int64) (*domain.Node, error) {
	n, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	n.Enabled = !n.Enabled
	if err := s.repo.SetEnabled(ctx, id, n.Enabled); err != nil {
		return nil, err
	}
	return n, nil
}

func (s *Service) Probe(ctx context.Context, id int64) (*domain.Node, error) {
	n, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	status, latency, err := s.remote.Status(ctx, n)
	now := s.now().UTC()
	if err != nil {
		_ = s.repo.SetStatus(ctx, id, domain.NodeStatusOffline, nil, 0, "", "", 0, 0, 0, err.Error())
		n.Status = domain.NodeStatusOffline
		n.LastError = err.Error()
		return n, nil
	}
	n.Status = domain.NodeStatusOnline
	n.LastHeartbeatAt = &now
	n.LatencyMS = latency.Milliseconds()
	n.PanelVersion = status.PanelVersion
	n.CoreVersion = status.CoreVersion
	n.CPUPct = status.CPUPct
	n.RAMPct = status.RAMPct
	n.UptimeSeconds = status.UptimeSeconds
	n.LastError = ""
	if err := s.repo.SetStatus(ctx, id, n.Status, n.LastHeartbeatAt, n.LatencyMS, n.PanelVersion, n.CoreVersion, n.CPUPct, n.RAMPct, n.UptimeSeconds, ""); err != nil {
		return nil, err
	}
	return n, nil
}

func (s *Service) Sync(ctx context.Context, id int64) (*SyncResult, error) {
	n, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	snap, err := s.remote.Snapshot(ctx, n)
	if err != nil {
		now := s.now().UTC()
		_ = s.repo.SetStatus(ctx, id, domain.NodeStatusOffline, &now, 0, "", "", 0, 0, 0, err.Error())
		return nil, err
	}

	inboundIDs := make(map[string]int64, len(snap.Inbounds))
	for _, rib := range snap.Inbounds {
		ib := &domain.Inbound{
			NodeID:       &id,
			RemoteID:     rib.ID,
			Remark:       rib.Remark,
			Protocol:     rib.Protocol,
			Port:         rib.Port,
			Transmission: rib.Transmission,
			TLS:          rib.TLS,
			SNI:          rib.SNI,
			Dest:         rib.Dest,
			Enabled:      rib.Enabled,
			Settings:     rib.Settings,
		}
		localID, err := s.inbounds.UpsertRemote(ctx, id, rib.ID, ib)
		if err != nil {
			return nil, err
		}
		inboundIDs[rib.ID] = localID
	}
	for _, rc := range snap.Clients {
		inboundID, ok := inboundIDs[rc.InboundID]
		if !ok {
			continue
		}
		expiry, err := parseTimePtr(rc.Expiry)
		if err != nil {
			return nil, err
		}
		firstUsedAt, err := parseTimePtr(rc.FirstUsedAt)
		if err != nil {
			return nil, err
		}
		c := &domain.Client{
			NodeID:             &id,
			RemoteID:           rc.ID,
			InboundID:          inboundID,
			Name:               rc.Name,
			UUID:               rc.UUID,
			Password:           rc.Password,
			UsedUp:             rc.UsedUp,
			UsedDown:           rc.UsedDown,
			TotalQuota:         rc.TotalQuota,
			Expiry:             expiry,
			Status:             rc.Status,
			SubToken:           rc.SubToken,
			StartAfterFirstUse: rc.StartAfterFirstUse,
			Enabled:            rc.Enabled,
			FirstUsedAt:        firstUsedAt,
		}
		if err := s.clients.UpsertRemote(ctx, id, rc.ID, inboundID, c); err != nil {
			return nil, err
		}
	}
	now := s.now().UTC()
	_ = s.repo.SetStatus(ctx, id, domain.NodeStatusOnline, &now, 0, snap.Status.PanelVersion, snap.Status.CoreVersion, snap.Status.CPUPct, snap.Status.RAMPct, snap.Status.UptimeSeconds, "")
	return &SyncResult{NodeID: id, InboundCount: len(snap.Inbounds), ClientCount: len(snap.Clients), SyncedAt: now}, nil
}

func (s *Service) CreateInbound(ctx context.Context, nodeID int64, in svcinbound.Input) (*domain.Inbound, error) {
	n, err := s.remoteNode(ctx, nodeID)
	if err != nil {
		return nil, err
	}
	rib, err := s.remote.CreateInbound(ctx, n, remoteInboundRequest(in))
	if err != nil {
		return nil, err
	}
	return s.cacheRemoteInbound(ctx, nodeID, rib)
}

func (s *Service) UpdateInbound(ctx context.Context, id int64, in svcinbound.Input) (*domain.Inbound, error) {
	ib, nodeID, err := s.cachedRemoteInbound(ctx, id)
	if err != nil {
		return nil, err
	}
	n, err := s.remoteNode(ctx, nodeID)
	if err != nil {
		return nil, err
	}
	rib, err := s.remote.UpdateInbound(ctx, n, ib.RemoteID, remoteInboundRequest(in))
	if err != nil {
		return nil, err
	}
	return s.cacheRemoteInbound(ctx, nodeID, rib)
}

func (s *Service) DeleteInbound(ctx context.Context, id int64) error {
	ib, nodeID, err := s.cachedRemoteInbound(ctx, id)
	if err != nil {
		return err
	}
	n, err := s.remoteNode(ctx, nodeID)
	if err != nil {
		return err
	}
	if err := s.remote.DeleteInbound(ctx, n, ib.RemoteID); err != nil && !IsRemoteStatus(err, 404) {
		return err
	}
	return s.inbounds.Delete(ctx, id)
}

func (s *Service) ToggleInbound(ctx context.Context, id int64) (*domain.Inbound, error) {
	ib, nodeID, err := s.cachedRemoteInbound(ctx, id)
	if err != nil {
		return nil, err
	}
	n, err := s.remoteNode(ctx, nodeID)
	if err != nil {
		return nil, err
	}
	rib, err := s.remote.ToggleInbound(ctx, n, ib.RemoteID)
	if err != nil {
		return nil, err
	}
	return s.cacheRemoteInbound(ctx, nodeID, rib)
}

func (s *Service) CreateClient(ctx context.Context, nodeID int64, in svcclient.CreateInput) (*domain.Client, error) {
	ib, err := s.remoteClientInbound(ctx, nodeID, in.InboundID)
	if err != nil {
		return nil, err
	}
	n, err := s.remoteNode(ctx, nodeID)
	if err != nil {
		return nil, err
	}
	req := RemoteClientCreateRequest{
		Name:               in.Name,
		InboundID:          ib.RemoteID,
		TotalQuota:         in.TotalQuota,
		Expiry:             formatTimePtr(in.Expiry),
		StartAfterFirstUse: in.StartAfterFirstUse,
	}
	rc, err := s.remote.CreateClient(ctx, n, req)
	if err != nil {
		return nil, err
	}
	return s.cacheRemoteClient(ctx, nodeID, rc)
}

func (s *Service) UpdateClient(ctx context.Context, id int64, in svcclient.UpdateInput) (*domain.Client, error) {
	c, nodeID, err := s.cachedRemoteClient(ctx, id)
	if err != nil {
		return nil, err
	}
	n, err := s.remoteNode(ctx, nodeID)
	if err != nil {
		return nil, err
	}
	req := RemoteClientUpdateRequest{
		Name:               in.Name,
		TotalQuota:         in.TotalQuota,
		StartAfterFirstUse: in.StartAfterFirstUse,
	}
	if in.InboundID != nil {
		ib, err := s.remoteClientInbound(ctx, nodeID, *in.InboundID)
		if err != nil {
			return nil, err
		}
		req.InboundID = &ib.RemoteID
	}
	if in.Expiry != nil {
		expiry := formatTimePtr(in.Expiry)
		req.Expiry = &expiry
	}
	if in.Status != nil {
		status := string(*in.Status)
		req.Status = &status
	}
	rc, err := s.remote.UpdateClient(ctx, n, c.RemoteID, req)
	if err != nil {
		return nil, err
	}
	return s.cacheRemoteClient(ctx, nodeID, rc)
}

func (s *Service) DeleteClient(ctx context.Context, id int64) error {
	c, nodeID, err := s.cachedRemoteClient(ctx, id)
	if err != nil {
		return err
	}
	n, err := s.remoteNode(ctx, nodeID)
	if err != nil {
		return err
	}
	if err := s.remote.DeleteClient(ctx, n, c.RemoteID); err != nil && !IsRemoteStatus(err, 404) {
		return err
	}
	return s.clients.Delete(ctx, id)
}

func (s *Service) ResetClientTraffic(ctx context.Context, id int64) (*domain.Client, error) {
	c, nodeID, err := s.cachedRemoteClient(ctx, id)
	if err != nil {
		return nil, err
	}
	n, err := s.remoteNode(ctx, nodeID)
	if err != nil {
		return nil, err
	}
	rc, err := s.remote.ResetClientTraffic(ctx, n, c.RemoteID)
	if err != nil {
		return nil, err
	}
	return s.cacheRemoteClient(ctx, nodeID, rc)
}

func (s *Service) SetClientStatus(ctx context.Context, id int64, status domain.ClientStatus) (*domain.Client, error) {
	c, nodeID, err := s.cachedRemoteClient(ctx, id)
	if err != nil {
		return nil, err
	}
	n, err := s.remoteNode(ctx, nodeID)
	if err != nil {
		return nil, err
	}
	rc, err := s.remote.SetClientStatus(ctx, n, c.RemoteID, status)
	if err != nil {
		return nil, err
	}
	return s.cacheRemoteClient(ctx, nodeID, rc)
}

func (s *Service) BulkDeleteClients(ctx context.Context, nodeID int64, ids []int64) ([]svcclient.BulkResult, error) {
	return s.bulkClients(ctx, nodeID, ids,
		func(n *domain.Node, remoteIDs []string) (*RemoteClientBulkResponse, error) {
			return s.remote.BulkDeleteClients(ctx, n, remoteIDs)
		},
		func(successIDs []int64) error { return s.clients.DeleteMany(ctx, successIDs) },
	)
}

func (s *Service) BulkResetClientTraffic(ctx context.Context, nodeID int64, ids []int64) ([]svcclient.BulkResult, error) {
	return s.bulkClients(ctx, nodeID, ids,
		func(n *domain.Node, remoteIDs []string) (*RemoteClientBulkResponse, error) {
			return s.remote.BulkResetClientTraffic(ctx, n, remoteIDs)
		},
		func(successIDs []int64) error { return s.clients.ResetTrafficMany(ctx, successIDs) },
	)
}

func (s *Service) BulkSetClientStatus(ctx context.Context, nodeID int64, ids []int64, status domain.ClientStatus) ([]svcclient.BulkResult, error) {
	if status != domain.ClientStatusActive && status != domain.ClientStatusDisabled {
		err := fmt.Errorf("%w: status must be active or disabled", ErrValidation)
		return bulkFailures(ids, err), err
	}
	return s.bulkClients(ctx, nodeID, ids,
		func(n *domain.Node, remoteIDs []string) (*RemoteClientBulkResponse, error) {
			return s.remote.BulkSetClientStatus(ctx, n, remoteIDs, status)
		},
		func(successIDs []int64) error {
			return s.clients.SetStatusMany(ctx, successIDs, status, status == domain.ClientStatusActive)
		},
	)
}

func (s *Service) bulkClients(
	ctx context.Context,
	nodeID int64,
	ids []int64,
	remoteCall func(*domain.Node, []string) (*RemoteClientBulkResponse, error),
	updateCache func([]int64) error,
) ([]svcclient.BulkResult, error) {
	results := make([]svcclient.BulkResult, len(ids))
	remoteIDs := make([]string, 0, len(ids))
	remoteToIndex := make(map[string]int, len(ids))
	for i, id := range ids {
		results[i].ID = id
		c, actualNodeID, err := s.cachedRemoteClient(ctx, id)
		if err != nil {
			results[i].Err = err
			continue
		}
		if actualNodeID != nodeID {
			results[i].Err = fmt.Errorf("%w: client belongs to a different node", ErrValidation)
			continue
		}
		remoteIDs = append(remoteIDs, c.RemoteID)
		remoteToIndex[c.RemoteID] = i
	}
	if len(remoteIDs) == 0 {
		return results, nil
	}

	n, err := s.remoteNode(ctx, nodeID)
	if err != nil {
		setPendingBulkError(results, remoteToIndex, err)
		return results, err
	}
	response, err := remoteCall(n, remoteIDs)
	if err != nil {
		setPendingBulkError(results, remoteToIndex, err)
		return results, err
	}
	if response == nil {
		err := fmt.Errorf("%w: empty bulk response", ErrRemote)
		setPendingBulkError(results, remoteToIndex, err)
		return results, err
	}

	seen := make(map[string]bool, len(remoteIDs))
	successIDs := make([]int64, 0, len(remoteIDs))
	for _, remoteResult := range response.Results {
		i, ok := remoteToIndex[remoteResult.ID]
		if !ok || seen[remoteResult.ID] {
			continue
		}
		seen[remoteResult.ID] = true
		if remoteResult.OK {
			results[i].OK = true
			successIDs = append(successIDs, results[i].ID)
			continue
		}
		results[i].Err = remoteBulkError(remoteResult.Error)
	}
	for remoteID, i := range remoteToIndex {
		if !seen[remoteID] {
			results[i].Err = fmt.Errorf("%w: missing bulk result", ErrRemote)
		}
	}

	if len(successIDs) > 0 {
		if cacheErr := updateCache(successIDs); cacheErr != nil {
			for i := range results {
				if results[i].OK {
					results[i].OK = false
					results[i].Err = cacheErr
				}
			}
			return results, cacheErr
		}
	}
	return results, nil
}

func setPendingBulkError(results []svcclient.BulkResult, indexes map[string]int, err error) {
	for _, i := range indexes {
		results[i].Err = err
	}
}

func remoteBulkError(message string) error {
	switch strings.TrimSpace(message) {
	case "not found":
		return repo.ErrNotFound
	case "node unreachable":
		return &UnreachableError{Detail: "unreachable"}
	default:
		return fmt.Errorf("%w: remote bulk item failed", ErrRemote)
	}
}

func bulkFailures(ids []int64, err error) []svcclient.BulkResult {
	results := make([]svcclient.BulkResult, len(ids))
	for i, id := range ids {
		results[i] = svcclient.BulkResult{ID: id, Err: err}
	}
	return results
}

func (s *Service) remoteNode(ctx context.Context, nodeID int64) (*domain.Node, error) {
	n, err := s.repo.GetByID(ctx, nodeID)
	if err != nil {
		return nil, err
	}
	if !n.Enabled {
		return nil, fmt.Errorf("%w: node is disabled", ErrValidation)
	}
	if strings.TrimSpace(n.APITokenSecret) == "" {
		return nil, fmt.Errorf("%w: node api token is required", ErrValidation)
	}
	return n, nil
}

func (s *Service) cachedRemoteInbound(ctx context.Context, id int64) (*domain.Inbound, int64, error) {
	ib, err := s.inbounds.GetByID(ctx, id)
	if err != nil {
		return nil, 0, err
	}
	if ib.NodeID == nil || ib.RemoteID == "" {
		return nil, 0, fmt.Errorf("%w: inbound is local", ErrValidation)
	}
	return ib, *ib.NodeID, nil
}

func (s *Service) cachedRemoteClient(ctx context.Context, id int64) (*domain.Client, int64, error) {
	c, err := s.clients.GetByID(ctx, id)
	if err != nil {
		return nil, 0, err
	}
	if c.NodeID == nil || c.RemoteID == "" {
		return nil, 0, fmt.Errorf("%w: client is local", ErrValidation)
	}
	return c, *c.NodeID, nil
}

func (s *Service) remoteClientInbound(ctx context.Context, nodeID int64, inboundID int64) (*domain.Inbound, error) {
	ib, err := s.inbounds.GetByID(ctx, inboundID)
	if err != nil {
		return nil, err
	}
	if ib.NodeID == nil || *ib.NodeID != nodeID || ib.RemoteID == "" {
		return nil, fmt.Errorf("%w: inbound belongs to a different node", ErrValidation)
	}
	return ib, nil
}

func (s *Service) cacheRemoteInbound(ctx context.Context, nodeID int64, rib *RemoteInbound) (*domain.Inbound, error) {
	if rib == nil || rib.ID == "" {
		return nil, fmt.Errorf("%w: remote inbound id is missing", ErrRemote)
	}
	ib := remoteInboundToDomain(nodeID, rib)
	localID, err := s.inbounds.UpsertRemote(ctx, nodeID, rib.ID, ib)
	if err != nil {
		return nil, err
	}
	return s.inbounds.GetByID(ctx, localID)
}

func (s *Service) cacheRemoteClient(ctx context.Context, nodeID int64, rc *RemoteClient) (*domain.Client, error) {
	if rc == nil || rc.ID == "" {
		return nil, fmt.Errorf("%w: remote client id is missing", ErrRemote)
	}
	ib, err := s.inbounds.GetByRemote(ctx, nodeID, rc.InboundID)
	if err != nil {
		return nil, err
	}
	c, err := remoteClientToDomain(nodeID, ib.ID, rc)
	if err != nil {
		return nil, err
	}
	if err := s.clients.UpsertRemote(ctx, nodeID, rc.ID, ib.ID, c); err != nil {
		return nil, err
	}
	return s.clients.GetByRemote(ctx, nodeID, rc.ID)
}

func (s *Service) Run(ctx context.Context, heartbeatInterval, syncInterval time.Duration) {
	if heartbeatInterval <= 0 {
		heartbeatInterval = 10 * time.Second
	}
	if syncInterval <= 0 {
		syncInterval = 15 * time.Second
	}
	heartbeat := time.NewTicker(heartbeatInterval)
	syncTick := time.NewTicker(syncInterval)
	defer heartbeat.Stop()
	defer syncTick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-heartbeat.C:
			s.probeEnabled(ctx)
		case <-syncTick.C:
			s.syncEnabled(ctx)
		}
	}
}

func (s *Service) probeEnabled(ctx context.Context) {
	nodes, err := s.repo.ListEnabled(ctx)
	if err != nil {
		return
	}
	var wg sync.WaitGroup
	sem := make(chan struct{}, 16)
	for i := range nodes {
		id := nodes[i].ID
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				return
			}
			_, _ = s.Probe(ctx, id)
		}()
	}
	wg.Wait()
}

func (s *Service) syncEnabled(ctx context.Context) {
	nodes, err := s.repo.ListEnabled(ctx)
	if err != nil {
		return
	}
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	for i := range nodes {
		id := nodes[i].ID
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				return
			}
			_, _ = s.Sync(ctx, id)
		}()
	}
	wg.Wait()
}

func nodeFromInput(in Input) (*domain.Node, error) {
	if strings.TrimSpace(in.Name) == "" {
		return nil, fmt.Errorf("name is required")
	}
	scheme := strings.ToLower(strings.TrimSpace(in.Scheme))
	if scheme == "" {
		scheme = "https"
	}
	if scheme != "http" && scheme != "https" {
		return nil, fmt.Errorf("scheme must be http or https")
	}
	if strings.TrimSpace(in.Address) == "" {
		return nil, fmt.Errorf("address is required")
	}
	if in.Port < 1 || in.Port > 65535 {
		return nil, fmt.Errorf("port must be between 1 and 65535")
	}
	return &domain.Node{
		Name:                strings.TrimSpace(in.Name),
		Remark:              strings.TrimSpace(in.Remark),
		Scheme:              scheme,
		Address:             strings.TrimSpace(in.Address),
		Port:                in.Port,
		BasePath:            strings.TrimSpace(in.BasePath),
		APITokenSecret:      strings.TrimSpace(in.APITokenSecret),
		Enabled:             in.Enabled,
		AllowPrivateAddress: in.AllowPrivateAddress,
		SkipTLSVerify:       in.SkipTLSVerify,
		Status:              domain.NodeStatusUnknown,
	}, nil
}

func parseTimePtr(s string) (*time.Time, error) {
	if s == "" {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func formatTimePtr(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func remoteInboundRequest(in svcinbound.Input) RemoteInboundRequest {
	return RemoteInboundRequest{
		Remark:                   in.Remark,
		Protocol:                 string(in.Protocol),
		Port:                     in.Port,
		Transmission:             string(in.Transmission),
		TLS:                      string(in.TLS),
		SNI:                      in.SNI,
		Dest:                     in.Dest,
		ACMEDomain:               in.ACMEDomain,
		ACMEEmail:                in.ACMEEmail,
		CertPath:                 in.CertPath,
		KeyPath:                  in.KeyPath,
		AllowInsecure:            in.AllowInsecure,
		WSPath:                   in.WSPath,
		GRPCServiceName:          in.GRPCServiceName,
		HTTPUpgradePath:          in.HTTPUpgradePath,
		HTTPUpgradeHost:          in.HTTPUpgradeHost,
		MultiplexEnabled:         in.MultiplexEnabled,
		Hy2UpMbps:                in.Hy2UpMbps,
		Hy2DownMbps:              in.Hy2DownMbps,
		Hy2IgnoreClientBandwidth: in.Hy2IgnoreClientBandwidth,
		Hy2ObfsPassword:          in.Hy2ObfsPassword,
		Hy2ObfsMinPacketSize:     in.Hy2ObfsMinPacketSize,
		Hy2ObfsMaxPacketSize:     in.Hy2ObfsMaxPacketSize,
		Hy2Masquerade:            in.Hy2Masquerade,
		Hy2Network:               in.Hy2Network,
		Hy2BrutalDebug:           in.Hy2BrutalDebug,
		Hy2BbrProfile:            in.Hy2BbrProfile,
		NaiveNetwork:             in.NaiveNetwork,
		NaiveQuicCongestionCtrl:  in.NaiveQuicCongestionCtrl,
	}
}

func remoteInboundToDomain(nodeID int64, rib *RemoteInbound) *domain.Inbound {
	return &domain.Inbound{
		NodeID:       &nodeID,
		RemoteID:     rib.ID,
		Remark:       rib.Remark,
		Protocol:     rib.Protocol,
		Port:         rib.Port,
		Transmission: rib.Transmission,
		TLS:          rib.TLS,
		SNI:          rib.SNI,
		Dest:         rib.Dest,
		Enabled:      rib.Enabled,
		Settings:     rib.Settings,
	}
}

func remoteClientToDomain(nodeID int64, inboundID int64, rc *RemoteClient) (*domain.Client, error) {
	expiry, err := parseTimePtr(rc.Expiry)
	if err != nil {
		return nil, err
	}
	firstUsedAt, err := parseTimePtr(rc.FirstUsedAt)
	if err != nil {
		return nil, err
	}
	return &domain.Client{
		NodeID:             &nodeID,
		RemoteID:           rc.ID,
		InboundID:          inboundID,
		Name:               rc.Name,
		UUID:               rc.UUID,
		Password:           rc.Password,
		UsedUp:             rc.UsedUp,
		UsedDown:           rc.UsedDown,
		TotalQuota:         rc.TotalQuota,
		Expiry:             expiry,
		Status:             rc.Status,
		SubToken:           rc.SubToken,
		StartAfterFirstUse: rc.StartAfterFirstUse,
		Enabled:            rc.Enabled,
		FirstUsedAt:        firstUsedAt,
	}, nil
}
