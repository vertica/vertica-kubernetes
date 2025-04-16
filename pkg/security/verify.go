package security

import (
	"bytes"
	"crypto/tls"
	"encoding/pem"
	"fmt"

	"github.com/go-logr/logr"
)

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
		log.Info("raw cert from service - " + url + " - " + remoteCert)
		if newCert == remoteCert {
			return 0, nil
		} else if currentCert == remoteCert {
			return 1, nil
		}
	}
	return 2, nil
}
