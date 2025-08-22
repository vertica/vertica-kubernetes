package security

import (
	"crypto/ecdsa"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"time"
)

const renewalThreshold = 30 * 24 * time.Hour // 30 days

// Decode a certificate using PEM and then parse using X.509, returning a usable cert
func DecodeCertificate(certPEM []byte) (*x509.Certificate, error) {
	block, _ := pem.Decode(certPEM)
	if block == nil || block.Type != "CERTIFICATE" {
		return nil, errors.New("failed to decode PEM block containing certificate")
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse certificate: %v", err)
	}

	return cert, nil
}

func ValidateTLSSecret(certPEM, keyPEM []byte) error {
	if err := ValidateCertificate(certPEM); err != nil {
		return err
	}
	if err := ValidateCertificateAndKeyMatch(certPEM, keyPEM); err != nil {
		return err
	}
	return nil
}

// Check that a PEM-encoded X.509 certificate has SubjectKeyIdentifier, appropriate Extended Key Usage, and is not expired.
func ValidateCertificate(certPEM []byte) error {
	cert, err := DecodeCertificate(certPEM)

	if err != nil {
		return err
	}

	if cert.IsCA && len(cert.SubjectKeyId) == 0 {
		return errors.New("certificate is missing SubjectKeyIdentifier extension")
	}

	var hasClientAuth, hasServerAuth bool
	for _, usage := range cert.ExtKeyUsage {
		if usage == x509.ExtKeyUsageClientAuth {
			hasClientAuth = true
		}
		if usage == x509.ExtKeyUsageServerAuth {
			hasServerAuth = true
		}
	}
	if !hasClientAuth || !hasServerAuth {
		return errors.New("certificate must include both clientAuth and serverAuth in Extended Key Usage")
	}

	now := time.Now().UTC()
	if now.Before(cert.NotBefore) {
		return fmt.Errorf("certificate is not valid yet: NotBefore = %v", cert.NotBefore)
	}
	if now.After(cert.NotAfter) {
		return fmt.Errorf("certificate has expired: NotAfter = %v", cert.NotAfter)
	}

	return nil
}

// Return whether or not a certificate is expiring soon (30 days or less)
// Also, return when it is expiring, to be used in logging output
func CheckCertificateExpiringSoon(certPEM []byte) (bool, time.Time, error) {
	cert, err := DecodeCertificate(certPEM)

	if err != nil {
		return false, time.Time{}, err
	}

	now := time.Now().UTC()
	return cert.NotAfter.Before(now.Add(renewalThreshold)), cert.NotAfter, nil
}

// Validate common name for a certificate matches expected
func ValidateCertificateCommonName(certPEM []byte, expectedCommonName string) error {
	cert, err := DecodeCertificate(certPEM)

	if err != nil {
		return err
	}

	if cert.Subject.CommonName != expectedCommonName {
		return errors.New("certificate common name does not match expected common name")
	}

	return nil
}

// ValidateCertificateAndKeyMatch ensures the private key matches the public key in the certificate.
func ValidateCertificateAndKeyMatch(certPEM, keyPEM []byte) error {
	// Decode and parse cert
	cert, err := DecodeCertificate(certPEM)
	if err != nil {
		return err
	}

	// Decode key
	keyBlock, _ := pem.Decode(keyPEM)
	if keyBlock == nil {
		return errors.New("failed to decode PEM block containing private key")
	}

	// Try all key formats (most common: PKCS1, PKCS8, EC)
	var privKey interface{}
	if privKey, err = x509.ParsePKCS1PrivateKey(keyBlock.Bytes); err != nil {
		if privKey, err = x509.ParsePKCS8PrivateKey(keyBlock.Bytes); err != nil {
			if privKey, err = x509.ParseECPrivateKey(keyBlock.Bytes); err != nil {
				return errors.New("failed to parse private key in any known format")
			}
		}
	}

	// Check key type match
	switch pub := cert.PublicKey.(type) {
	case *rsa.PublicKey:
		priv, ok := privKey.(*rsa.PrivateKey)
		if !ok || pub.N.Cmp(priv.N) != 0 {
			return errors.New("RSA private key does not match certificate public key")
		}
	case *ecdsa.PublicKey:
		priv, ok := privKey.(*ecdsa.PrivateKey)
		if !ok || pub.X.Cmp(priv.X) != 0 || pub.Y.Cmp(priv.Y) != 0 {
			return errors.New("ECDSA private key does not match certificate public key")
		}
	default:
		return fmt.Errorf("unsupported public key type %T", cert.PublicKey)
	}

	return nil
}
