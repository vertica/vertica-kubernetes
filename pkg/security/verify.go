package security

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"time"

	"github.com/go-logr/logr"
)

const renewalThreshold = 30 * 24 * time.Hour // 30 days

// verifyCert returns 0 when newCert is in use, 1 when currentCert is in use.
// 2 when neither of them is in use
func VerifyCert(ip string, port int, newCert, currentCert string, log logr.Logger) (int, error) {
	conf := &tls.Config{
		InsecureSkipVerify: true, // #nosec G402
	}
	url := fmt.Sprintf("%s:%d", ip, port)
	conn, err := tls.Dial("tcp", url, conf)
	if err != nil {
		log.Error(err, "dial error from verify https cert for "+url)
		return -1, err
	}
	defer conn.Close()
	certs := conn.ConnectionState().PeerCertificates
	for _, cert := range certs {
		var b bytes.Buffer
		err := pem.Encode(&b, &pem.Block{
			Type:  "CERTIFICATE",
			Bytes: cert.Raw,
		})
		if err != nil {
			log.Error(err, "failed to convert cert to PEM for verification")
			return -1, err
		}
		remoteCert := b.String()
		if newCert == remoteCert {
			return 0, nil
		} else if currentCert == remoteCert {
			return 1, nil
		}
	}
	return 2, nil
}

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

	now := time.Now()
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

	now := time.Now()
	return cert.NotAfter.Before(now.Add(renewalThreshold)), cert.NotAfter, nil
}
