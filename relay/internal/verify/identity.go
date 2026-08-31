package verify

import (
	"context"
	"fmt"
	"time"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/rs/zerolog/log"

	"github.com/siempre-alaina/at-protocol/relay/internal/config"
	"github.com/siempre-alaina/at-protocol/relay/internal/metrics"
)

// IdentityResolver handles DID resolution with caching
type IdentityResolver struct {
	directory identity.Directory
	metrics   *metrics.Metrics
}

// NewIdentityResolver creates a new identity resolver with caching
func NewIdentityResolver(cfg config.IdentityConfig, m *metrics.Metrics) *IdentityResolver {
	// Create base directory (handles PLC and did:web resolution)
	baseDir := identity.BaseDirectory{
		PLCURL: cfg.PLCURL,
	}

	// Wrap with caching directory
	cachedDir := identity.NewCacheDirectory(
		&baseDir,
		cfg.CacheSize,  // capacity
		cfg.CacheTTL,   // hit TTL
		time.Minute*5,  // error TTL (cache failures briefly)
		time.Minute*30, // invalid handle TTL
	)

	log.Info().
		Str("plc_url", cfg.PLCURL).
		Int("cache_size", cfg.CacheSize).
		Dur("cache_ttl", cfg.CacheTTL).
		Msg("Identity resolver initialized")

	return &IdentityResolver{
		directory: cachedDir,
		metrics:   m,
	}
}

// ResolveIdentity looks up a DID and returns the identity
func (r *IdentityResolver) ResolveIdentity(ctx context.Context, did string) (*identity.Identity, error) {
	start := time.Now()

	// Parse the DID string into syntax.DID
	parsedDID, err := syntax.ParseDID(did)
	if err != nil {
		return nil, fmt.Errorf("invalid DID format: %w", err)
	}

	ident, err := r.directory.LookupDID(ctx, parsedDID)
	if err != nil {
		if r.metrics != nil {
			r.metrics.DIDCacheMisses.Inc()
			r.metrics.VerificationTotal.WithLabelValues("resolve_failed").Inc()
		}
		return nil, fmt.Errorf("failed to resolve DID %s: %w", did, err)
	}

	duration := time.Since(start)

	// Log slow resolutions
	if duration > time.Second {
		log.Warn().
			Str("did", did).
			Dur("duration", duration).
			Msg("Slow DID resolution")
	}

	if r.metrics != nil {
		// Note: indigo's CacheDirectory doesn't expose hit/miss stats directly
		// We approximate by checking resolution time (cache hits are fast)
		if duration < time.Millisecond*10 {
			r.metrics.DIDCacheHits.Inc()
		} else {
			r.metrics.DIDCacheMisses.Inc()
		}
	}

	return ident, nil
}
