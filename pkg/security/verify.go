package security

import (
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
