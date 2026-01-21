//! TLS configuration and certificate management.

use std::fs;
use std::io::BufReader;
use std::path::Path;
use std::sync::Arc;

use rustls::pki_types::{CertificateDer, PrivateKeyDer};
use rustls::{ClientConfig, ServerConfig};
use tokio_rustls::{TlsAcceptor, TlsConnector};
use tracing::{debug, info};

use avalanche_ids::NodeId;

use crate::{NetworkError, Result};

/// TLS configuration for the network.
#[derive(Clone)]
pub struct TlsConfig {
    /// TLS acceptor for incoming connections.
    pub acceptor: TlsAcceptor,
    /// TLS connector for outgoing connections.
    pub connector: TlsConnector,
    /// This node's certificate.
    pub certificate: Vec<CertificateDer<'static>>,
    /// Node ID derived from certificate.
    pub node_id: NodeId,
}

impl TlsConfig {
    /// Creates a new TLS configuration from certificate and key files.
    pub fn from_files(cert_path: &Path, key_path: &Path) -> Result<Self> {
        info!(
            "Loading TLS certificates from {} and {}",
            cert_path.display(),
            key_path.display()
        );

        // Load certificate
        let cert_file = fs::File::open(cert_path)
            .map_err(|e| NetworkError::Tls(format!("failed to open cert: {}", e)))?;
        let mut cert_reader = BufReader::new(cert_file);
        let certs: Vec<CertificateDer<'static>> = rustls_pemfile::certs(&mut cert_reader)
            .filter_map(|r| r.ok())
            .collect();

        if certs.is_empty() {
            return Err(NetworkError::Tls("no certificates found".to_string()));
        }

        // Load private key
        let key_file = fs::File::open(key_path)
            .map_err(|e| NetworkError::Tls(format!("failed to open key: {}", e)))?;
        let mut key_reader = BufReader::new(key_file);
        let private_key = rustls_pemfile::private_key(&mut key_reader)
            .map_err(|e| NetworkError::Tls(format!("failed to read key: {}", e)))?
            .ok_or_else(|| NetworkError::Tls("no private key found".to_string()))?;

        Self::from_der(certs, private_key)
    }

    /// Creates a new TLS configuration from DER-encoded certificate and key.
    pub fn from_der(
        certs: Vec<CertificateDer<'static>>,
        private_key: PrivateKeyDer<'static>,
    ) -> Result<Self> {
        // Derive node ID from first certificate
        let node_id = node_id_from_cert(&certs[0])?;
        debug!("Node ID from certificate: {}", node_id);

        // Server config
        let server_config = ServerConfig::builder()
            .with_no_client_auth()
            .with_single_cert(certs.clone(), private_key.clone_key())
            .map_err(|e| NetworkError::Tls(format!("server config error: {}", e)))?;

        // Client config - accept any certificate (we verify NodeId separately)
        // In Avalanche, we don't use CA-based trust. We verify by NodeId.
        // For now, use a permissive verifier.
        let client_config = ClientConfig::builder()
            .dangerous()
            .with_custom_certificate_verifier(Arc::new(PermissiveVerifier))
            .with_client_auth_cert(certs.clone(), private_key)
            .map_err(|e| NetworkError::Tls(format!("client config error: {}", e)))?;

        Ok(Self {
            acceptor: TlsAcceptor::from(Arc::new(server_config)),
            connector: TlsConnector::from(Arc::new(client_config)),
            certificate: certs,
            node_id,
        })
    }

}

/// Derives a NodeId from a certificate.
pub fn node_id_from_cert(cert: &CertificateDer<'_>) -> Result<NodeId> {
    use sha2::{Digest, Sha256};

    // Parse the certificate
    let (_, parsed) = x509_parser::parse_x509_certificate(cert.as_ref())
        .map_err(|e| NetworkError::Tls(format!("failed to parse certificate: {:?}", e)))?;

    // Get the public key
    let pubkey = parsed.public_key().raw;

    // SHA256 hash of the public key
    let hash = Sha256::digest(pubkey);

    // Take first 20 bytes for NodeId
    let mut bytes = [0u8; 20];
    bytes.copy_from_slice(&hash[..20]);

    NodeId::from_slice(&bytes).map_err(|e| NetworkError::Tls(format!("invalid node id: {}", e)))
}

/// A permissive TLS certificate verifier.
///
/// In Avalanche, nodes verify each other by NodeId, not CA chain.
/// This verifier accepts all certificates, and NodeId verification
/// happens at the application layer.
#[derive(Debug)]
struct PermissiveVerifier;

impl rustls::client::danger::ServerCertVerifier for PermissiveVerifier {
    fn verify_server_cert(
        &self,
        _end_entity: &CertificateDer<'_>,
        _intermediates: &[CertificateDer<'_>],
        _server_name: &rustls::pki_types::ServerName<'_>,
        _ocsp_response: &[u8],
        _now: rustls::pki_types::UnixTime,
    ) -> std::result::Result<rustls::client::danger::ServerCertVerified, rustls::Error> {
        Ok(rustls::client::danger::ServerCertVerified::assertion())
    }

    fn verify_tls12_signature(
        &self,
        _message: &[u8],
        _cert: &CertificateDer<'_>,
        _dss: &rustls::DigitallySignedStruct,
    ) -> std::result::Result<rustls::client::danger::HandshakeSignatureValid, rustls::Error> {
        Ok(rustls::client::danger::HandshakeSignatureValid::assertion())
    }

    fn verify_tls13_signature(
        &self,
        _message: &[u8],
        _cert: &CertificateDer<'_>,
        _dss: &rustls::DigitallySignedStruct,
    ) -> std::result::Result<rustls::client::danger::HandshakeSignatureValid, rustls::Error> {
        Ok(rustls::client::danger::HandshakeSignatureValid::assertion())
    }

    fn supported_verify_schemes(&self) -> Vec<rustls::SignatureScheme> {
        vec![
            rustls::SignatureScheme::RSA_PKCS1_SHA256,
            rustls::SignatureScheme::RSA_PKCS1_SHA384,
            rustls::SignatureScheme::RSA_PKCS1_SHA512,
            rustls::SignatureScheme::ECDSA_NISTP256_SHA256,
            rustls::SignatureScheme::ECDSA_NISTP384_SHA384,
            rustls::SignatureScheme::RSA_PSS_SHA256,
            rustls::SignatureScheme::RSA_PSS_SHA384,
            rustls::SignatureScheme::RSA_PSS_SHA512,
            rustls::SignatureScheme::ED25519,
        ]
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_node_id_derivation() {
        // This would need actual certificate data for a real test
        // For now, just verify the function exists and compiles
    }
}
