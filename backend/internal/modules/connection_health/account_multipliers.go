package connection_health

import (
	"context"
	"log"
	"math"
	"strings"
)

// accountMultiplierReader is deliberately kept separate from healthRepository.
// A number of integrations provide a small in-memory health repository; making this
// optional lets those integrations keep working while the production repository gains
// the persisted manual multiplier table. Keep read capability independent from the
// mutation methods so a read-only integration is still able to expose overrides.
type accountMultiplierReader interface {
	ListAccountMultiplierOverrides(ctx context.Context, userID string, adminAccountID string) ([]AccountMultiplierOverride, error)
}

// accountMultiplierWriter is the narrow mutation contract used by the PUT endpoint.
// GetAccountMultiplierOverride is intentionally not required here: writes always
// return the value supplied by the caller, and the aggregate read path uses the list
// method above. This keeps older repository adapters source-compatible.
type accountMultiplierWriter interface {
	UpsertAccountMultiplierOverride(ctx context.Context, override AccountMultiplierOverride) error
	DeleteAccountMultiplierOverride(ctx context.Context, userID string, adminAccountID string, targetID string) error
}

// loadAccountMultiplierOverrides loads the current workspace's manual values and
// preserves read errors for callers that must not make a scheduling decision without
// knowing whether an override exists.
func (s *Service) loadAccountMultiplierOverrides(ctx context.Context, userID string, adminAccountID string) (map[string]float64, error) {
	result := make(map[string]float64)
	repository, ok := s.repo.(accountMultiplierReader)
	if !ok {
		return result, nil
	}
	rows, err := repository.ListAccountMultiplierOverrides(ctx, userID, adminAccountID)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		// Production SQL already scopes rows by both keys, but keep the
		// boundary defensive for alternate repository implementations.
		if (row.UserID != "" && row.UserID != userID) ||
			(row.AdminAccountID != "" && row.AdminAccountID != adminAccountID) {
			continue
		}
		targetID := strings.TrimSpace(row.TargetID)
		if targetID == "" || !validAccountMultiplier(row.Multiplier) {
			continue
		}
		result[targetID] = row.Multiplier
	}
	return result, nil
}

// accountMultiplierOverrides is the best-effort variant used by the read-only
// health aggregation. A transient storage error should not take down the entire
// page; the scheduler uses loadAccountMultiplierOverrides directly instead.
func (s *Service) accountMultiplierOverrides(ctx context.Context, userID string, adminAccountID string) map[string]float64 {
	result, err := s.loadAccountMultiplierOverrides(ctx, userID, adminAccountID)
	if err != nil {
		log.Printf("[connection-health] account multiplier overrides read failed user_id=%s admin_account_id=%s err=%v", userID, adminAccountID, err)
		return make(map[string]float64)
	}
	return result
}

// resolveAccountMultiplier defines the single value exposed by the merged UI column.
// The manual override is authoritative. When it is absent, retain the existing
// priority/group value first, then use the actual upstream key group. The legacy
// Sub2API account rate_multiplier is intentionally excluded because it does not
// represent the upstream API-key group multiplier.
func resolveAccountMultiplier(values ...*float64) *float64 {
	for _, value := range values {
		if value == nil || !validAccountMultiplier(*value) {
			continue
		}
		resolved := *value
		return &resolved
	}
	return nil
}

func validAccountMultiplier(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= 0
}

func validAccountMultiplierPointer(value *float64) bool {
	return value != nil && validAccountMultiplier(*value)
}

// groupTargetsHaveMultiplierSources verifies that a multiplier policy can run
// without a group-level value. A target is covered by either a persisted manual
// override or the currently resolved upstream API-key group multiplier. The
// check is deliberately all-targets-at-once so a partially configured group
// cannot receive an implicit/default rank for only some accounts.
func (s *Service) groupTargetsHaveMultiplierSources(
	ctx context.Context,
	userID string,
	adminAccountID string,
	platform string,
	targetIDs map[string]struct{},
) (bool, error) {
	if len(targetIDs) == 0 {
		return true, nil
	}
	manual, err := s.loadAccountMultiplierOverrides(ctx, userID, adminAccountID)
	if err != nil {
		return false, err
	}
	upstreamGroups := s.upstreamKeyGroupsByAdminAccount(ctx, userID, adminAccountID, platform)
	for targetID := range targetIDs {
		if value, ok := manual[targetID]; ok && validAccountMultiplier(value) {
			continue
		}
		parsed, ok := parseTargetID(targetID)
		if !ok {
			return false, nil
		}
		if info, ok := upstreamGroups[parsed.accountID]; ok && validAccountMultiplierPointer(info.multiplier) {
			continue
		}
		return false, nil
	}
	return true, nil
}

// AccountMultiplierResponse is returned after a manual value is saved or cleared.
// The response intentionally contains no account credentials or upstream payload.
type AccountMultiplierResponse struct {
	TargetID                string   `json:"targetId"`
	Multiplier              *float64 `json:"multiplier"`
	ManualAccountMultiplier *float64 `json:"manualAccountMultiplier"`
	AccountMultiplier       *float64 `json:"accountMultiplier"`
}

// SetAccountMultiplier validates the target's workspace ownership before persisting
// the override. A nil multiplier clears the manual value and restores automatic
// fallback to the existing group/upstream sources.
func (s *Service) SetAccountMultiplier(ctx context.Context, userID string, targetID string, multiplier *float64) (AccountMultiplierResponse, error) {
	adminAccountID, err := s.currentAdminAccountID(ctx, userID)
	if err != nil {
		return AccountMultiplierResponse{}, err
	}
	targetID = strings.TrimSpace(targetID)
	parsed, ok := parseTargetID(targetID)
	if !ok || parsed.adminAccountID != adminAccountID {
		return AccountMultiplierResponse{}, requestError(ErrorProbeTargetNotFound)
	}
	if multiplier != nil && !validAccountMultiplier(*multiplier) {
		return AccountMultiplierResponse{}, requestError(ErrorAccountMultiplierInvalid)
	}
	repository, ok := s.repo.(accountMultiplierWriter)
	if !ok {
		return AccountMultiplierResponse{}, requestError(ErrorAccountMultiplierUnsupported)
	}

	// A target id is deterministic, but validating it against the current inventory
	// prevents saving arbitrary IDs from another workspace or a deleted account. Keep
	// this check server-side; the browser never supplies platform credentials. A
	// partially configured service must fail closed: skipping this check would let a
	// caller persist an arbitrary, otherwise well-formed target id.
	if s.platformGroups == nil || s.mySites == nil {
		return AccountMultiplierResponse{}, requestError(ErrorAccountMultiplierUnsupported)
	}
	session, sessionErr := s.mySites.RequireSession(ctx, userID, adminAccountID)
	if sessionErr != nil {
		return AccountMultiplierResponse{}, sessionErr
	}
	// Target IDs are persisted and later looked up byte-for-byte. Do not accept a
	// case-variant platform segment that would never match the canonical ID emitted
	// by buildTargetID.
	if string(session.Platform) != parsed.platform {
		return AccountMultiplierResponse{}, requestError(ErrorProbeTargetNotFound)
	}
	target, _, found, accountsReadError, targetErr := s.findAdminTarget(ctx, session, adminAccountID, parsed.accountID)
	if targetErr != nil {
		return AccountMultiplierResponse{}, targetErr
	}
	if !found {
		if accountsReadError {
			return AccountMultiplierResponse{}, requestError(ErrorAccountsFetch)
		}
		return AccountMultiplierResponse{}, requestError(ErrorProbeTargetNotFound)
	}
	if target.TargetID != targetID {
		return AccountMultiplierResponse{}, requestError(ErrorProbeTargetNotFound)
	}

	if multiplier == nil {
		if err := repository.DeleteAccountMultiplierOverride(ctx, userID, adminAccountID, targetID); err != nil {
			return AccountMultiplierResponse{}, err
		}
	} else {
		value := *multiplier
		if err := repository.UpsertAccountMultiplierOverride(ctx, AccountMultiplierOverride{
			UserID: userID, AdminAccountID: adminAccountID, TargetID: targetID, Multiplier: value,
		}); err != nil {
			return AccountMultiplierResponse{}, err
		}
	}

	response := AccountMultiplierResponse{TargetID: targetID, Multiplier: multiplier, ManualAccountMultiplier: multiplier}
	if multiplier != nil {
		value := *multiplier
		response.AccountMultiplier = &value
	}
	return response, nil
}

// accountMultiplierForTarget is used by priority sync. It intentionally returns nil
// when the value is absent, allowing the existing group multiplier logic to run.
func accountMultiplierForTarget(overrides map[string]float64, targetID string) *float64 {
	if value, ok := overrides[targetID]; ok && validAccountMultiplier(value) {
		resolved := value
		return &resolved
	}
	return nil
}
