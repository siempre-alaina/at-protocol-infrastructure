package verify

import (
	"bytes"
	"context"
	"fmt"
	"time"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/bluesky-social/indigo/repo"
	"github.com/rs/zerolog/log"

	"github.com/siempre-alaina/at-protocol-infrastructure/relay/internal/config"
	"github.com/siempre-alaina/at-protocol-infrastructure/relay/internal/ingest"
	"github.com/siempre-alaina/at-protocol-infrastructure/relay/internal/metrics"
)

// VerificationResult contains the result of verifying an event
type VerificationResult struct {
	Valid    bool
	Event    ingest.Event
	Identity string // Resolved handle or DID
	Error    error
	Duration time.Duration
	CacheHit bool
}

// Pipeline handles event verification
type Pipeline struct {
	resolver *IdentityResolver
	metrics  *metrics.Metrics
	cfg      config.IdentityConfig

	// Stats
	verified int64
	failed   int64
}

// NewPipeline creates a new verification pipeline
func NewPipeline(cfg config.IdentityConfig, m *metrics.Metrics) *Pipeline {
	return &Pipeline{
		resolver: NewIdentityResolver(cfg, m),
		metrics:  m,
		cfg:      cfg,
	}
}

// Verify validates an event
func (p *Pipeline) Verify(ctx context.Context, event ingest.Event) VerificationResult {
	start := time.Now()
	result := VerificationResult{
		Event: event,
	}

	// Only verify commit events (they have signatures)
	if event.Type != "commit" {
		// Non-commit events (identity, account, etc.) are trusted from the PDS
		result.Valid = true
		result.Duration = time.Since(start)
		return result
	}

	// Skip if no raw data (blocks)
	if len(event.RawData) == 0 {
		result.Valid = true // Trust events without blocks (tooBig events)
		result.Duration = time.Since(start)
		return result
	}

	// Step 1: Resolve the DID to get the signing key
	ident, err := p.resolver.ResolveIdentity(ctx, event.DID)
	if err != nil {
		result.Error = fmt.Errorf("identity resolution failed: %w", err)
		result.Duration = time.Since(start)
		p.recordFailure("identity_failed")
		return result
	}

	result.Identity = ident.Handle.String()

	// Read the repo from CAR blocks
	r, err := repo.ReadRepoFromCar(ctx, bytes.NewReader(event.RawData))
	if err != nil {
		result.Error = fmt.Errorf("failed to read repo from CAR: %w", err)
		result.Duration = time.Since(start)
		p.recordFailure("car_parse_failed")
		return result
	}

	// Step 3: Get the signed commit and verify it has a signature
	signedCommit := r.SignedCommit()
	if len(signedCommit.Sig) == 0 {
		result.Error = fmt.Errorf("no signature in commit")
		result.Duration = time.Since(start)
		p.recordFailure("no_signature")
		return result
	}

	// Step 4: Get the public key from the identity
	pubKey, err := ident.PublicKey()
	if err != nil {
		result.Error = fmt.Errorf("failed to get public key: %w", err)
		result.Duration = time.Since(start)
		p.recordFailure("no_pubkey")
		return result
	}

	// Step 5: Verify the signature
	if err := verifyCommitSignature(signedCommit, pubKey); err != nil {
		result.Error = fmt.Errorf("signature verification failed: %w", err)
		result.Duration = time.Since(start)
		p.recordFailure("signature_invalid")
		return result
	}

	// Note: CID verification is implicit - the signature is over the commit content,
	// so if signature verification passes, the commit is valid
	// The commit CID can be verified by re-encoding, but that's redundant with signature check

	// All checks passed
	result.Valid = true
	result.Duration = time.Since(start)
	p.recordSuccess()

	if p.metrics != nil {
		p.metrics.VerificationDuration.Observe(result.Duration.Seconds())
	}

	return result
}

// verifyCommitSignature verifies the signature on a signed commit
func verifyCommitSignature(commit repo.SignedCommit, pubKey atcrypto.PublicKey) error {
	// Get the unsigned commit and serialize it for verification
	unsigned := commit.Unsigned()

	// Get bytes for signing (DAG-CBOR encoding of unsigned commit)
	unsignedBytes, err := unsigned.BytesForSigning()
	if err != nil {
		return fmt.Errorf("failed to get bytes for signing: %w", err)
	}

	// Verify the signature using the public key
	if err := pubKey.HashAndVerify(unsignedBytes, commit.Sig); err != nil {
		return fmt.Errorf("signature verification failed: %w", err)
	}

	return nil
}

func (p *Pipeline) recordSuccess() {
	p.verified++
	if p.metrics != nil {
		p.metrics.VerificationTotal.WithLabelValues("success").Inc()
	}
}

func (p *Pipeline) recordFailure(reason string) {
	p.failed++
	if p.metrics != nil {
		p.metrics.VerificationTotal.WithLabelValues(reason).Inc()
	}
	log.Debug().Str("reason", reason).Msg("Verification failed")
}

// Stats returns verification statistics
func (p *Pipeline) Stats() (verified, failed int64) {
	return p.verified, p.failed
}
