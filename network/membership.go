// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"fmt"
	"slices"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/subnets"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/set"
)

const membershipExpiryWarning = 30 * 24 * time.Hour

var _ subnets.MembershipChecker = (*membership)(nil)

// certifiedSubnets maps each subnet whose membership CA verified a peer's chain
// to when that verification expires. It is never mutated once built.
type certifiedSubnets map[ids.ID]time.Time

func (c certifiedSubnets) validAt(subnetID ids.ID, now time.Time) bool {
	expiresAt, ok := c[subnetID]
	return ok && now.Before(expiresAt)
}

func (c certifiedSubnets) anyValidAt(now time.Time) bool {
	for _, expiresAt := range c {
		if now.Before(expiresAt) {
			return true
		}
	}
	return false
}

// membership knows the non-validator members of the subnets this node tracks:
// peers certified by a subnet's membership CA, and its allowedNodes.
// Certification is decided once per connection, at the handshake, and dropped
// on disconnect: the node ID fixes the leaf but not the intermediates.
type membership struct {
	// Never changes after construction, so it is read without the lock.
	cas map[ids.ID]*subnets.MembershipCA
	// Connection admission only asks whether a peer is listed anywhere.
	allowedNodes set.Set[ids.NodeID]

	lock        sync.RWMutex
	certMembers map[ids.NodeID]certifiedSubnets
}

// newMembership loads the membership CAs and allowedNodes of [subnetConfigs]
// and logs what it loaded.
func newMembership(
	log logging.Logger,
	tlsConfig *tls.Config,
	subnetConfigs map[ids.ID]subnets.Config,
) (*membership, error) {
	m := &membership{
		cas:         make(map[ids.ID]*subnets.MembershipCA),
		certMembers: make(map[ids.NodeID]certifiedSubnets),
	}
	for subnetID, config := range subnetConfigs {
		ca, digests, err := config.LoadMembershipCA()
		if err != nil {
			return nil, fmt.Errorf("loading membership CA of subnet %s: %w", subnetID, err)
		}
		if ca != nil {
			m.cas[subnetID] = ca
			logLoaded(log, subnetID, ca, digests)
		}

		if config.AllowedNodes.Len() > 0 {
			log.Warn("allowedNodes is deprecated, use membershipCAPath or membershipCAContent instead",
				zap.Stringer("subnetID", subnetID),
			)
		}
		m.allowedNodes.Union(config.AllowedNodes)
	}

	m.logOwnCertificate(log, tlsConfig)
	return m, nil
}

// findCertifiedSubnets returns the subnets whose membership CA verifies
// [chain], leaf first, or nil so that stock peers cost no allocation. It is the
// connection path's only signature check, so it runs once per upgrade, before
// peersLock is taken.
func (m *membership) findCertifiedSubnets(chain []*x509.Certificate) certifiedSubnets {
	var certified certifiedSubnets
	for subnetID, ca := range m.cas {
		expiresAt, ok := ca.Certify(chain)
		if !ok {
			continue
		}
		if certified == nil {
			certified = make(certifiedSubnets)
		}
		certified[subnetID] = expiresAt
	}
	return certified
}

// track is called under peersLock after the de-duplication checks, so that a
// Disconnected for an earlier connection to the same node cannot remove this
// one's record.
func (m *membership) track(nodeID ids.NodeID, verified certifiedSubnets) {
	if len(verified) == 0 {
		return
	}

	m.lock.Lock()
	defer m.lock.Unlock()

	m.certMembers[nodeID] = verified
}

func (m *membership) untrack(nodeID ids.NodeID) {
	m.lock.Lock()
	defer m.lock.Unlock()

	delete(m.certMembers, nodeID)
}

func (m *membership) certified(nodeID ids.NodeID) certifiedSubnets {
	m.lock.RLock()
	defer m.lock.RUnlock()

	return m.certMembers[nodeID]
}

// IsCertified reports whether [nodeID]'s chain verified against [subnetID]'s
// membership CA when it connected, and has not expired since.
func (m *membership) IsCertified(subnetID ids.ID, nodeID ids.NodeID) bool {
	if _, ok := m.cas[subnetID]; !ok {
		return false
	}
	return m.certified(nodeID).validAt(subnetID, time.Now())
}

// isMemberOfAny reports whether [nodeID] is a non-validator member of a tracked
// subnet. Validators are kept by the ip tracker, which [network.AllowConnection]
// checks first.
func (m *membership) isMemberOfAny(nodeID ids.NodeID) bool {
	return m.allowedNodes.Contains(nodeID) ||
		m.certified(nodeID).anyValidAt(time.Now())
}

// logLoaded lets an operator confirm the node read the CA and revocation lists
// they installed: the digests match sha256sum of the files.
func logLoaded(log logging.Logger, subnetID ids.ID, ca *subnets.MembershipCA, digests subnets.MembershipDigests) {
	fields := []zap.Field{
		zap.Stringer("subnetID", subnetID),
		zap.String("sha256", hex.EncodeToString(digests.CA[:])),
	}
	if len(ca.CRLs()) > 0 {
		fields = append(fields, zap.String("crlSHA256", hex.EncodeToString(digests.CRL[:])))
	}
	log.Info("membership CA loaded", fields...)
	for _, list := range ca.CRLs() {
		log.Info("membership CRL loaded",
			zap.Stringer("subnetID", subnetID),
			zap.Stringer("issuer", list.Issuer),
			zap.Stringer("number", list.Number),
			zap.Int("revoked", len(list.RevokedCertificateEntries)),
			zap.String("thisUpdate", list.ThisUpdate.UTC().Format(time.RFC3339)),
		)
	}
}

// logOwnCertificate reports whether this node's own chain verifies against a
// membership CA. It is advisory, and silent for the stock self-signed
// certificate.
func (m *membership) logOwnCertificate(log logging.Logger, tlsConfig *tls.Config) {
	if len(m.cas) == 0 {
		return
	}

	// Only the leaf was parsed when the staking certificate was loaded.
	chain, err := x509.ParseCertificates(slices.Concat(tlsConfig.Certificates[0].Certificate...))
	if err != nil {
		log.Warn("staking certificate chain does not parse",
			zap.Error(err),
		)
		return
	}
	if len(chain) == 1 && bytes.Equal(chain[0].RawIssuer, chain[0].RawSubject) {
		return
	}

	serial := serialHex(chain[0])
	certified := m.findCertifiedSubnets(chain)
	if len(certified) == 0 {
		log.Warn("staking certificate does not chain to any tracked subnet's membership CA",
			zap.Int("chainLen", len(chain)),
		)
		return
	}
	for subnetID, expiresAt := range certified {
		fields := []zap.Field{
			zap.Stringer("subnetID", subnetID),
			zap.String("serial", serial),
			// zap.Time's log format drops the year.
			zap.String("expiresAt", expiresAt.UTC().Format(time.RFC3339)),
		}
		if remaining := time.Until(expiresAt); remaining < membershipExpiryWarning {
			log.Warn("staking certificate membership expires soon",
				append(fields, zap.Duration("remaining", remaining))...,
			)
			continue
		}
		log.Info("staking certificate chains to membership CA", fields...)
	}
}

// serialHex is how a revocation list names [cert]: hex of the serial's bytes,
// as OpenSSL prints it.
func serialHex(cert *x509.Certificate) string {
	return fmt.Sprintf("%X", cert.SerialNumber.Bytes())
}

// IsCertified implements [subnets.MembershipChecker].
func (n *network) IsCertified(subnetID ids.ID, nodeID ids.NodeID) bool {
	return n.membership.IsCertified(subnetID, nodeID)
}
