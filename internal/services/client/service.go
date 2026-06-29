// Package client provides CRUD and lifecycle operations for proxy clients,
// including credential and subscription-token generation and quota/status
// transitions.
package client

import (
	"context"
	"errors"
	"fmt"
	"time"

	"sing-box-web-panel/internal/domain"
	"sing-box-web-panel/internal/lib/keys"
)

// Repo is the persistence contract for clients.
type Repo interface {
	Create(ctx context.Context, c *domain.Client) error
	GetByID(ctx context.Context, id int64) (*domain.Client, error)
	GetBySubToken(ctx context.Context, token string) (*domain.Client, error)
	List(ctx context.Context) ([]domain.Client, error)
	ListByInbound(ctx context.Context, inboundID int64) ([]domain.Client, error)
	Update(ctx context.Context, c *domain.Client) error
	Delete(ctx context.Context, id int64) error
	SetStatus(ctx context.Context, id int64, status domain.ClientStatus, enabled bool) error
	ResetTraffic(ctx context.Context, id int64) error
	DeleteMany(ctx context.Context, ids []int64) error
	SetStatusMany(ctx context.Context, ids []int64, status domain.ClientStatus, enabled bool) error
	ResetTrafficMany(ctx context.Context, ids []int64) error
}

// InboundLookup validates that a referenced inbound exists.
type InboundLookup interface {
	GetByID(ctx context.Context, id int64) (*domain.Inbound, error)
}

// ConfigTrigger requests a (debounced) regenerate-and-apply of the live config.
type ConfigTrigger interface {
	Trigger()
}

var (
	ErrValidation     = errors.New("validation error")
	ErrInboundMissing = errors.New("inbound does not exist")
)

type Service struct {
	repo     Repo
	inbounds InboundLookup
	trigger  ConfigTrigger
}

func NewService(repo Repo, inbounds InboundLookup, trigger ConfigTrigger) *Service {
	return &Service{repo: repo, inbounds: inbounds, trigger: trigger}
}

func (s *Service) notify() {
	if s.trigger != nil {
		s.trigger.Trigger()
	}
}

// CreateInput carries the fields the UI supplies when provisioning a client.
// InboundIDs is the multi-binding set (SIN-11); InboundID is the legacy single
// binding kept for backward compatibility. When InboundIDs is empty, InboundID
// is used as the sole binding.
type CreateInput struct {
	Name               string
	InboundID          int64
	InboundIDs         []int64
	TotalQuota         int64
	Expiry             *time.Time
	StartAfterFirstUse bool
}

// UpdateInput carries optional field updates; nil fields are left unchanged.
// InboundIDs (when non-nil) replaces the client's whole binding set; InboundID
// is the legacy single-binding alias.
type UpdateInput struct {
	Name               *string
	InboundID          *int64
	InboundIDs         *[]int64
	TotalQuota         *int64
	Expiry             *time.Time
	Status             *domain.ClientStatus
	StartAfterFirstUse *bool
}

// BulkResult preserves the requested client ID and reports its independent
// outcome. Err is intentionally kept internal; handlers map it to safe API
// messages before serializing a response.
type BulkResult struct {
	ID  int64
	OK  bool
	Err error
}

func (s *Service) List(ctx context.Context, inboundFilter *int64) ([]domain.Client, error) {
	if inboundFilter != nil {
		return s.repo.ListByInbound(ctx, *inboundFilter)
	}
	return s.repo.List(ctx)
}

func (s *Service) Get(ctx context.Context, id int64) (*domain.Client, error) {
	return s.repo.GetByID(ctx, id)
}

// resolveInboundSet validates that every referenced inbound exists and is local
// (not on a remote node), returning the order-preserving deduped set. The first
// element becomes the client's primary binding.
func (s *Service) resolveInboundSet(ctx context.Context, ids []int64) ([]int64, error) {
	seen := make(map[int64]bool, len(ids))
	out := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id == 0 || seen[id] {
			continue
		}
		ib, err := s.inbounds.GetByID(ctx, id)
		if err != nil {
			return nil, ErrInboundMissing
		}
		if ib.NodeID != nil {
			return nil, fmt.Errorf("%w: inbound belongs to a remote node", ErrValidation)
		}
		seen[id] = true
		out = append(out, id)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%w: at least one inbound is required", ErrValidation)
	}
	return out, nil
}

func (s *Service) Create(ctx context.Context, in CreateInput) (*domain.Client, error) {
	if in.Name == "" {
		return nil, fmt.Errorf("%w: name is required", ErrValidation)
	}
	candidate := in.InboundIDs
	if len(candidate) == 0 {
		candidate = []int64{in.InboundID}
	}
	set, err := s.resolveInboundSet(ctx, candidate)
	if err != nil {
		return nil, err
	}

	uuid, err := keys.GenerateUUID()
	if err != nil {
		return nil, err
	}
	password, err := keys.GeneratePassword()
	if err != nil {
		return nil, err
	}
	token, err := keys.GenerateSubToken()
	if err != nil {
		return nil, err
	}

	c := &domain.Client{
		InboundID:          set[0],
		InboundIDs:         set,
		Name:               in.Name,
		UUID:               uuid,
		Password:           password,
		TotalQuota:         in.TotalQuota,
		Expiry:             in.Expiry,
		Status:             domain.ClientStatusActive,
		SubToken:           token,
		StartAfterFirstUse: in.StartAfterFirstUse,
		Enabled:            true,
	}
	if err := s.repo.Create(ctx, c); err != nil {
		return nil, err
	}
	s.notify()
	return c, nil
}

func (s *Service) Update(ctx context.Context, id int64, in UpdateInput) (*domain.Client, error) {
	c, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if c.NodeID != nil {
		return nil, fmt.Errorf("%w: remote client must be updated through its node", ErrValidation)
	}

	if in.Name != nil {
		if *in.Name == "" {
			return nil, fmt.Errorf("%w: name is required", ErrValidation)
		}
		c.Name = *in.Name
	}
	if in.InboundIDs != nil || in.InboundID != nil {
		var candidate []int64
		if in.InboundIDs != nil {
			candidate = *in.InboundIDs
		} else {
			candidate = []int64{*in.InboundID}
		}
		set, err := s.resolveInboundSet(ctx, candidate)
		if err != nil {
			return nil, err
		}
		c.InboundID = set[0]
		c.InboundIDs = set
	}
	if in.TotalQuota != nil {
		c.TotalQuota = *in.TotalQuota
	}
	if in.Expiry != nil {
		c.Expiry = in.Expiry
	}
	if in.StartAfterFirstUse != nil {
		c.StartAfterFirstUse = *in.StartAfterFirstUse
	}
	if in.Status != nil {
		c.Status = *in.Status
		c.Enabled = *in.Status == domain.ClientStatusActive
	}

	if err := s.repo.Update(ctx, c); err != nil {
		return nil, err
	}
	s.notify()
	return c, nil
}

func (s *Service) Delete(ctx context.Context, id int64) error {
	c, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if c.NodeID != nil {
		return fmt.Errorf("%w: remote client must be deleted through its node", ErrValidation)
	}
	if err := s.repo.Delete(ctx, id); err != nil {
		return err
	}
	s.notify()
	return nil
}

// SetStatus transitions a client and aligns its enabled flag (only active
// clients are emitted into the live config).
func (s *Service) SetStatus(ctx context.Context, id int64, status domain.ClientStatus) (*domain.Client, error) {
	c, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if c.NodeID != nil {
		return nil, fmt.Errorf("%w: remote client status must be set through its node", ErrValidation)
	}
	enabled := status == domain.ClientStatusActive
	if err := s.repo.SetStatus(ctx, id, status, enabled); err != nil {
		return nil, err
	}
	c.Status = status
	c.Enabled = enabled
	s.notify()
	return c, nil
}

func (s *Service) ResetTraffic(ctx context.Context, id int64) (*domain.Client, error) {
	existing, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if existing.NodeID != nil {
		return nil, fmt.Errorf("%w: remote client traffic must be reset through its node", ErrValidation)
	}
	if err := s.repo.ResetTraffic(ctx, id); err != nil {
		return nil, err
	}
	c, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	return c, nil
}

// BulkDelete deletes every valid local client in one repository transaction.
// Missing and remote clients remain independent failures. A successful batch
// schedules exactly one config apply.
func (s *Service) BulkDelete(ctx context.Context, ids []int64) ([]BulkResult, error) {
	return s.bulkMutate(ctx, ids, true, func(valid []int64) error {
		return s.repo.DeleteMany(ctx, valid)
	})
}

// BulkSetStatus updates status and enabled together in one transaction and
// schedules exactly one config apply when at least one client changes.
func (s *Service) BulkSetStatus(ctx context.Context, ids []int64, status domain.ClientStatus) ([]BulkResult, error) {
	if status != domain.ClientStatusActive && status != domain.ClientStatusDisabled {
		err := fmt.Errorf("%w: status must be active or disabled", ErrValidation)
		return failedBulkResults(ids, err), err
	}
	enabled := status == domain.ClientStatusActive
	return s.bulkMutate(ctx, ids, true, func(valid []int64) error {
		return s.repo.SetStatusMany(ctx, valid, status, enabled)
	})
}

// BulkResetTraffic clears counters in one transaction. Traffic counters are
// not part of generated sing-box config, so this operation does not apply it.
func (s *Service) BulkResetTraffic(ctx context.Context, ids []int64) ([]BulkResult, error) {
	return s.bulkMutate(ctx, ids, false, func(valid []int64) error {
		return s.repo.ResetTrafficMany(ctx, valid)
	})
}

func (s *Service) bulkMutate(
	ctx context.Context,
	ids []int64,
	notify bool,
	mutate func([]int64) error,
) ([]BulkResult, error) {
	results := make([]BulkResult, len(ids))
	valid := make([]int64, 0, len(ids))
	validIndexes := make([]int, 0, len(ids))
	for i, id := range ids {
		results[i].ID = id
		c, err := s.repo.GetByID(ctx, id)
		if err != nil {
			results[i].Err = err
			continue
		}
		if c.NodeID != nil {
			results[i].Err = fmt.Errorf("%w: remote client must be changed through its node", ErrValidation)
			continue
		}
		valid = append(valid, id)
		validIndexes = append(validIndexes, i)
	}

	if len(valid) == 0 {
		return results, nil
	}
	if err := mutate(valid); err != nil {
		for _, i := range validIndexes {
			results[i].Err = err
		}
		return results, err
	}
	for _, i := range validIndexes {
		results[i].OK = true
	}
	if notify {
		s.notify()
	}
	return results, nil
}

func failedBulkResults(ids []int64, err error) []BulkResult {
	results := make([]BulkResult, len(ids))
	for i, id := range ids {
		results[i] = BulkResult{ID: id, Err: err}
	}
	return results
}
