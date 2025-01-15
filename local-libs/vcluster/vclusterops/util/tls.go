package util

import (
	"crypto/tls"
	"crypto/x509"
	"time"
)

// generateTLSVerifyFunc returns a callback function suitable for use as the VerifyPeerCertificate
// field of a tls.Config struct.  It is a slightly less performant but logically equivalent version of
// the validation logic which gets run when InsecureSkipVerify == false in go v1.20.11.  The difference
// is that hostname validation is elided, which is not possible without custom verification.
//
// See crypto/x509/verify.go for hostname validation behavior and crypto/tls/handshake_client.go for
// the reference implementation of this function.
func GenerateTLSVerifyFunc(rootCAs *x509.CertPool) func([][]byte, [][]*x509.Certificate) error {
	return func(certificates [][]byte, _ [][]*x509.Certificate) error {
		// Reparse certs.  The crypto/tls package version does some extra checks, but they're already
		// done by this point, so no need to repeat them.  It also uses a cache to reduce parsing, which
		// isn't included here, but could be if there is a perf issue.
		certs := make([]*x509.Certificate, len(certificates))
		for i, asn1Data := range certificates {
			cert, err := x509.ParseCertificate(asn1Data)
			if err != nil {
				return err
			}
			certs[i] = cert
		}

		// construct verification options like reference implementation, minus hostname
		opts := x509.VerifyOptions{
			Roots:         rootCAs,
			CurrentTime:   time.Now(),
			DNSName:       "",
			Intermediates: x509.NewCertPool(),
		}

		for _, cert := range certs[1:] {
			opts.Intermediates.AddCert(cert)
		}
		_, err := certs[0].Verify(opts)
		if err != nil {
			return &tls.CertificateVerificationError{UnverifiedCertificates: certs, Err: err}
		}

		return nil
	}
}
